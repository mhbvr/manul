package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	workerClosed = errors.New("Worker closed")
	tracer       = otel.Tracer("worker")
)

// WorkerConfig defines the configuration for a Worker instance that is adjustable in runtime
type WorkerConfig struct {
	InFlight          int                         // Limit number of in-flight requests allowed
	IntervalGenerator func(float64) time.Duration // Function that generates intervals between requests (nil for ASAP mode)
	Qps               float64                     // Target queries per second
	Timeout           time.Duration               // Timeout for individual job executions
}

func (cfg WorkerConfig) IsValid() error {
	if cfg.InFlight < 0 {
		return fmt.Errorf("InFlight < 0")
	}

	if cfg.Qps < 0 {
		return fmt.Errorf("Qps < 0")
	}

	if cfg.Timeout < 0 {
		return fmt.Errorf("Timeout < 0")
	}
	return nil
}

// StableIntervalGenerator produces (fixed) intervals.
func StableIntervalGenerator(qps float64) time.Duration {
	if qps == 0 {
		return 0
	}
	return time.Duration(float64(time.Second) / qps)
}

// ExponentialIntervalGenerator produces exponentially distributed intervals.
func ExponentialIntervalGenerator(qps float64) time.Duration {
	if qps == 0 {
		return 0
	}
	return time.Duration(rand.ExpFloat64() / qps * float64(time.Second))
}

type Option func(*Worker)

// Worker manages concurrent execution of jobs with configurable rate limiting and timing modes.
type Worker struct {
	ctx         context.Context
	cancelCause context.CancelCauseFunc

	maxInFlight int          // Maximum allowed in-flight limit
	cfg         WorkerConfig // Current configuration

	tokens      chan struct{}          // Token bucket for in-flight limiting
	cfgChan     chan WorkerConfig      // Channel for configuration updates
	readCfgChan chan chan WorkerConfig // Channel for reading current configuration

	job func(context.Context) error // Job function to execute

	logger *log.Logger
}

// NewWorker starts a new Worker with the given configuration, job function, and maximum in-flight limit.
// Returns an error if any parameters are invalid.
func NewWorker(ctx context.Context, job func(context.Context) error, opts ...Option) (*Worker, error) {
	if job == nil {
		return nil, fmt.Errorf("job function should be be defined")
	}

	res := &Worker{
		maxInFlight: 1,
		cfg: WorkerConfig{ // Default config, ASAP with 1 in flight
			InFlight: 1,
			Timeout:  time.Second,
		},
		cfgChan:     make(chan WorkerConfig),
		readCfgChan: make(chan chan WorkerConfig),
		job:         job,
		logger:      log.New(io.Discard, "", 0),
	}

	for _, opt := range opts {
		opt(res)
	}

	if res.maxInFlight < 0 {
		return nil, fmt.Errorf("maxInFlight < 0")
	}

	if res.maxInFlight < res.cfg.InFlight {
		return nil, fmt.Errorf("cfg.InFlight > maxInFlight limit")
	}

	res.tokens = make(chan struct{}, res.maxInFlight)
	for i := 0; i < res.cfg.InFlight; i++ {
		res.tokens <- struct{}{}
	}

	res.ctx, res.cancelCause = context.WithCancelCause(ctx)

	res.logger.Printf("Starting worker: maxInflight: %d, inFlight: %d, Qps: %f, Timeout: %fs",
		res.maxInFlight, res.cfg.InFlight, res.cfg.Qps, res.cfg.Timeout.Seconds())

	go func() {
		err := res.loop()
		res.logger.Printf("Worker terminated: %v", err)
	}()

	return res, nil
}

func WithConfig(cfg WorkerConfig) func(w *Worker) {
	return func(w *Worker) {
		w.cfg = cfg
	}
}

func WithLogger(logger *log.Logger) func(w *Worker) {
	return func(w *Worker) {
		w.logger = logger
	}
}

func WithMaxInFlight(maxInFlight int) func(w *Worker) {
	return func(w *Worker) {
		w.maxInFlight = maxInFlight
	}
}

// GetConfig returns a copy of the current configuration.
func (w *Worker) GetConfig() (*WorkerConfig, error) {
	respChan := make(chan WorkerConfig, 1)

	select {
	case <-w.ctx.Done():
		return nil, context.Cause(w.ctx)
	case w.readCfgChan <- respChan:
	}

	select {
	case <-w.ctx.Done():
		return nil, context.Cause(w.ctx)
	case resp := <-respChan:
		return &resp, nil
	}
}

// SetConfig updates the runner configuration asynchronously.
// The update will be applied during the next iteration of the Run loop.
func (w *Worker) SetConfig(cfg *WorkerConfig) error {
	if err := cfg.IsValid(); err != nil {
		return err
	}

	if cfg.InFlight > w.maxInFlight {
		return fmt.Errorf("InFlight < maxInFlight")
	}

	select {
	case <-w.ctx.Done():
		return context.Cause(w.ctx)
	case w.cfgChan <- *cfg:
	}
	return nil
}

func (w *Worker) Close() {
	w.cancelCause(workerClosed)
}

// setTimer creates a timer channel based on the IntervalGenerator.
// Returns nil for ASAP mode (when IntervalGenerator is nil or returns ≤0).
func (w *Worker) setTimer() <-chan time.Time {
	if w.cfg.IntervalGenerator == nil {
		return nil
	}

	interval := w.cfg.IntervalGenerator(w.cfg.Qps)
	if interval <= 0 {
		return nil
	}

	return time.NewTimer(interval).C
}

// do executes the job function with the given timeout and returns a token when done.
// This method handles the actual job execution and token management.
func (w *Worker) do(ctx context.Context, timeout time.Duration) error {
	defer func() {
		w.tokens <- struct{}{}
	}()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return w.job(ctx)
}

// loop handles job scheduling, rate limiting, and configuration updates.
// This method blocks until the context is cancelled.
func (w *Worker) loop() error {
	_, span := tracer.Start(w.ctx, "worker_loop")

	timer := w.setTimer()
	var trigger chan struct{}
	currentInFlight := w.cfg.InFlight

	if timer == nil {
		// ASAP mode
		trigger = w.tokens
	}

	span.AddEvent("starting loop", trace.WithAttributes(
		attribute.Float64("qps", w.cfg.Qps),
		attribute.Bool("asap", w.cfg.IntervalGenerator == nil),
		attribute.Float64("timeout_sec", w.cfg.Timeout.Seconds()),
		attribute.Int("inflight", w.cfg.InFlight),
	))

	for {
		select {
		case <-w.ctx.Done():
			span.SetStatus(codes.Ok, "")
			span.End()
			return context.Cause(w.ctx)
		case <-timer:
			// Timer was set and expired
			// We can aquire token now when available on the next loop
			trigger = w.tokens
		case <-trigger:
			if currentInFlight > w.cfg.InFlight {
				// Need to decrease in flight because of config change
				// Skiping do() execution
				currentInFlight--
				continue
			}

			go w.do(w.ctx, w.cfg.Timeout)

			if timer != nil {
				// As we using timer we need to wait for the it
				// before sending request. Disabling trigger
				trigger = nil
				timer = w.setTimer()
			}

		case cfg := <-w.cfgChan:
			// Configuration request, assume that validity is checked
			w.cfg = cfg
			span.AddEvent("configuration changed", trace.WithAttributes(
				attribute.Float64("qps", w.cfg.Qps),
				attribute.Bool("asap", w.cfg.IntervalGenerator == nil),
				attribute.Float64("timeout_sec", w.cfg.Timeout.Seconds()),
				attribute.Int("inflight", w.cfg.InFlight),
			))

			// Increase in flight token limiter
			// It is supposed to be fast operation, so doing it immedately
			for cfg.InFlight > currentInFlight {
				w.tokens <- struct{}{}
				currentInFlight++
			}

			// Reset timers as interval generator or qps can changed
			timer = w.setTimer()
			if timer == nil {
				// Need to wait for the timer first
				trigger = w.tokens
			}
		case respChan := <-w.readCfgChan:
			respChan <- w.cfg
		}
	}
}

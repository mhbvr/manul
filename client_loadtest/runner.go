package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// RunnerConfig defines the configuration for a Runner instance.
type RunnerConfig struct {
	Inflight int           // Current number of in-flight requests allowed
	Mode     string        // Request mode: "asap", "stable", or "exponential"
	Rps      float64       // Target requests per second (0 means unlimited)
	Timeout  time.Duration // Timeout for individual job executions
}

// Runner manages concurrent execution of jobs with configurable rate limiting and timing modes.
type Runner struct {
	maxInflight int                         // Maximum allowed in-flight requests
	cfg         *RunnerConfig               // Current configuration
	tokens      chan struct{}               // Token bucket for in-flight limiting
	cfgChan     chan *RunnerConfig          // Channel for configuration updates
	readCfgChan chan chan *RunnerConfig     // Channel for reading current configuration
	job         func(context.Context) error // Job function to execute
}

// NewRunner creates a new Runner with the given configuration, job function, and maximum in-flight limit.
// Returns an error if any parameters are invalid.
func NewRunner(cfg *RunnerConfig, job func(context.Context) error, maxInFlight int) (*Runner, error) {
	if cfg == nil || job == nil || maxInFlight <= 0 {
		return nil, fmt.Errorf("invalid Runner parameters")
	}

	res := &Runner{
		maxInflight: maxInFlight,
		cfg:         cfg,
		tokens:      make(chan struct{}, maxInFlight),
		cfgChan:     make(chan *RunnerConfig),
		readCfgChan: make(chan chan *RunnerConfig),
		job:         job,
	}

	for i := 0; i < cfg.Inflight; i++ {
		res.tokens <- struct{}{}
	}
	return res, nil
}

// GetConfig returns a copy of the current configuration.
func (r *Runner) GetConfig(ctx context.Context) (*RunnerConfig, error) {
	respChan := make(chan *RunnerConfig, 1)

	r.readCfgChan <- respChan
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case resp := <-respChan:
		return resp, nil
	}
}

// SetConfig updates the runner configuration asynchronously.
// The update will be applied during the next iteration of the Run loop.
func (r *Runner) SetConfig(ctx context.Context, cfg *RunnerConfig) error {
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case r.cfgChan <- cfg:
	}
	return nil
}

// setTimer creates a timer channel based on the current mode and RPS settings.
// Returns nil for "asap" mode or when RPS is 0, indicating no timing restrictions.
func (r *Runner) setTimer() <-chan time.Time {
	if r.cfg.Rps == 0 {
		return nil
	}

	switch r.cfg.Mode {
	case "stable":
		return time.NewTimer(time.Duration(float64(time.Second) / r.cfg.Rps)).C
	case "exponential":
		return time.NewTimer(time.Duration(rand.ExpFloat64() / r.cfg.Rps * float64(time.Second))).C
	default:
		return nil
	}
}

// do executes the job function with the given timeout and returns a token when done.
// This method handles the actual job execution and token management.
func (r *Runner) do(ctx context.Context, timeout time.Duration) error {
	defer func() {
		r.tokens <- struct{}{}
	}()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return r.job(ctx)
}

// Run starts the main execution loop that handles job scheduling, rate limiting, and configuration updates.
// This method blocks until the context is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	timer := r.setTimer()
	var trigger chan struct{}
	targetInFlight := r.cfg.Inflight

	if timer == nil {
		trigger = r.tokens
	}

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-timer:
			// Timer expired, we need to run do() func on the next loop
			trigger = r.tokens
		case <-trigger:
			if targetInFlight < r.cfg.Inflight {
				// Need to decrease in flight because of config change
				r.cfg.Inflight--
				continue
			}

			go r.do(ctx, r.cfg.Timeout)

			// Reset timer if nessesary
			if timer != nil {
				trigger = nil
				timer = r.setTimer()
			}

		case cfg := <-r.cfgChan:
			// Increase in flight token limiter
			// It is supposed to be fast operation, so doing it immedately
			for cfg.Inflight > r.cfg.Inflight && r.cfg.Inflight <= r.maxInflight {
				r.tokens <- struct{}{}
				r.cfg.Inflight++
				targetInFlight = r.cfg.Inflight
			}

			// It may take some time as requests may take all tokens
			if cfg.Inflight < r.cfg.Inflight {
				targetInFlight = max(0, cfg.Inflight)
			}

			r.cfg.Mode = cfg.Mode
			r.cfg.Rps = cfg.Rps
			if cfg.Rps == 0 {
				r.cfg.Mode = "asap"
			}
			r.cfg.Timeout = cfg.Timeout

			// Reset timers as rps and mode can changed
			timer = r.setTimer()
			if timer == nil {
				trigger = r.tokens
			}
		case respChan := <-r.readCfgChan:
			cfg := *(r.cfg)
			respChan <- &cfg
		}
	}
}

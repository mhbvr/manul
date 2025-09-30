package loadrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/mhbvr/manul/client_loadtest/worker"
)

var (
	lrClosed = errors.New("LoadRunner closed")
)

type LoadRunner struct {
	worker      *worker.Worker
	maxInFlight int

	ctx    context.Context
	cancel context.CancelCauseFunc

	load     Load
	recorder func(float64, bool)

	startTime time.Time
	logger    *log.Logger
}

type LoadRunnerInfo struct {
	StartTime   time.Time
	MaxInFlight int
	WorkerCfg   *worker.WorkerConfig
}

type Option func(*LoadRunner)

func NewLoadRunner(ctx context.Context,
	maxInFlight int,
	cfg *worker.WorkerConfig,
	load Load,
	opts ...Option) (*LoadRunner, error) {

	ctx, cancel := context.WithCancelCause(context.Background())
	res := &LoadRunner{
		ctx:         ctx,
		cancel:      cancel,
		maxInFlight: maxInFlight,
		startTime:   time.Now(),
		load:        load,
		logger:      log.New(io.Discard, "", 0),
	}

	for _, opt := range opts {
		opt(res)
	}

	// Initialize load
	if err := load.Init(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize load: %v", err)
	}

	// Create worker options
	workerOpts := []worker.Option{
		worker.WithMaxInFlight(maxInFlight),
		worker.WithConfig(*cfg),
		worker.WithLogger(res.logger),
	}

	// Add recorder if provided
	if res.recorder != nil {
		workerOpts = append(workerOpts, worker.WithRecorder(res.recorder))
	}

	// Create worker
	var err error
	res.worker, err = worker.NewWorker(ctx, load.Job, workerOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create worker: %v", err)
	}
	return res, nil
}

func WithLogger(logger *log.Logger) func(lr *LoadRunner) {
	return func(lr *LoadRunner) {
		lr.logger = logger
	}
}

func WithRecorder(recorder func(float64, bool)) func(*LoadRunner) {
	return func(lr *LoadRunner) {
		lr.recorder = recorder
	}
}

func (lr *LoadRunner) SetConfig(cfg *worker.WorkerConfig) error {
	return lr.worker.SetConfig(cfg)
}

func (lr *LoadRunner) GetInfo() (*LoadRunnerInfo, error) {
	var err error
	res := &LoadRunnerInfo{
		StartTime:   lr.startTime,
		MaxInFlight: lr.maxInFlight,
	}

	res.WorkerCfg, err = lr.worker.GetConfig()
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (lr *LoadRunner) Close() {
	lr.cancel(lrClosed)
	lr.load.Close()
}


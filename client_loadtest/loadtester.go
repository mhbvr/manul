package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/mhbvr/manul/client_loadtest/loadrunner"
	"github.com/mhbvr/manul/client_loadtest/worker"
)

func generator(mode string) (func(float64) time.Duration, error) {
	switch mode {
	case "asap":
		return nil, nil
	case "exponential":
		return worker.ExponentialIntervalGenerator, nil
	case "static":
		return worker.StableIntervalGenerator, nil
	}
	return nil, fmt.Errorf("unknown mode: %v", mode)
}

type LoadTester struct {
	mu sync.RWMutex

	maxInflight int
	defaultCfg  *worker.WorkerConfig

	// Multiple runner instances
	runners      map[string]*loadrunner.LoadRunner
	runnersMode  map[string]string
	nextRunnerID int

	metrics *Metrics
}

func NewLoadTester(maxInflight int) (*LoadTester, error) {
	// Default runner configuration

	lt := &LoadTester{
		maxInflight:  maxInflight,
		runners:      make(map[string]*loadrunner.LoadRunner),
		runnersMode:  make(map[string]string),
		nextRunnerID: 0,
		metrics:      NewMetrics(),
	}
	return lt, nil
}

func (lt *LoadTester) AddRunner(serverAddr string, inFlight int, qps float64, timeout time.Duration, mode string) error {
	generator, err := generator(mode)
	if err != nil {
		return err
	}

	lt.mu.Lock()
	defer lt.mu.Unlock()

	runnerID := fmt.Sprintf("runner-%d", lt.nextRunnerID)
	lt.nextRunnerID++

	// Create logger for this runner
	logger := log.New(log.Writer(), fmt.Sprintf("[%s] ", runnerID), log.LstdFlags)

	runner, err := loadrunner.NewLoadRunner(
		context.Background(),
		runnerID,
		serverAddr,
		lt.maxInflight,
		&worker.WorkerConfig{
			InFlight:          inFlight,
			IntervalGenerator: generator,
			Qps:               qps,
			Timeout:           timeout,
		},
		loadrunner.WithRecorger(func(runnerID string, durationSeconds float64, success bool) {
			lt.metrics.RecordRequest(runnerID, durationSeconds, success)
		}),
		loadrunner.WithLogger(logger),
	)
	if err != nil {
		return err
	}

	lt.runners[runnerID] = runner
	lt.runnersMode[runnerID] = mode
	return nil
}

func (lt *LoadTester) RemoveRunner(runnerID string) error {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	runner, exists := lt.runners[runnerID]
	if !exists {
		return fmt.Errorf("runner %s not found", runnerID)
	}

	runner.Close()
	delete(lt.runners, runnerID)
	delete(lt.runnersMode, runnerID)
	return nil
}

func (lt *LoadTester) UpdateRunner(runnerID string,
	inFlight int,
	qps float64,
	timeout time.Duration,
	mode string) error {

	generator, err := generator(mode)
	if err != nil {
		return err
	}

	lt.mu.Lock()
	defer lt.mu.Unlock()
	runner, exists := lt.runners[runnerID]

	if !exists {
		return fmt.Errorf("runner %s not found", runnerID)
	}

	err = runner.SetConfig(&worker.WorkerConfig{
		InFlight:          inFlight,
		IntervalGenerator: generator,
		Qps:               qps,
		Timeout:           timeout,
	})

	if err == nil {
		lt.runnersMode[runnerID] = mode
	}
	return err
}

type Status struct {
	LoadRunnerInfo *loadrunner.LoadRunnerInfo
	OkRequests     int
	ErrRequests    int
	Mode           string
}

func (lt *LoadTester) GetRunnersInfo(ctx context.Context) ([]*Status, error) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	res := make([]*Status, 0)

	for runnerID, runner := range lt.runners {
		// Extract metrics from Prometheus counters
		var successCount, errorCount int

		successMetric, err := lt.metrics.ResponseCounter.GetMetricWithLabelValues("ok", runnerID)
		if err == nil && successMetric != nil {
			pb := &dto.Metric{}
			successMetric.Write(pb)
			successCount += int(pb.GetCounter().GetValue())
		}

		errorMetric, err := lt.metrics.ResponseCounter.GetMetricWithLabelValues("error", runnerID)
		if err == nil && errorMetric != nil {
			pb := &dto.Metric{}
			errorMetric.Write(pb)
			errorCount += int(pb.GetCounter().GetValue())
		}

		info, err := runner.GetInfo()
		if err != nil {
			return nil, err
		}

		status := &Status{
			LoadRunnerInfo: info,
			OkRequests:     successCount,
			ErrRequests:    errorCount,
			Mode:           lt.runnersMode[runnerID],
		}
		res = append(res, status)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].LoadRunnerInfo.Id < res[j].LoadRunnerInfo.Id
	})

	return res, nil
}

func (lt *LoadTester) Close() error {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	for _, runner := range lt.runners {
		runner.Close()
	}
	return nil
}

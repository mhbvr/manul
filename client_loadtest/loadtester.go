package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	_ "github.com/mhbvr/manul/k8s_grpc_resolver"
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

type runnerInfo struct {
	runner *loadrunner.LoadRunner
	id     string
	server string
	mode   string
}

type LoadTester struct {
	mu sync.RWMutex

	maxInflight int
	defaultCfg  *worker.WorkerConfig

	// Multiple runner instances
	runners      map[string]*runnerInfo
	nextRunnerID int

	metrics *Metrics
}

func NewLoadTester(maxInflight int) (*LoadTester, error) {
	// Default runner configuration

	lt := &LoadTester{
		maxInflight:  maxInflight,
		runners:      make(map[string]*runnerInfo),
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

	// Create gRPC dial options
	grpcOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	}

	// Create load implementation
	load := loadrunner.NewCatPhotoLoad(serverAddr, grpcOpts)

	runner, err := loadrunner.NewLoadRunner(
		context.Background(),
		lt.maxInflight,
		&worker.WorkerConfig{
			InFlight:          inFlight,
			IntervalGenerator: generator,
			Qps:               qps,
			Timeout:           timeout,
		},
		load,
		loadrunner.WithRecorder(func(durationSeconds float64, success bool) {
			lt.metrics.RecordRequest(runnerID, durationSeconds, success)
		}),
		loadrunner.WithLogger(logger),
	)
	if err != nil {
		return err
	}

	lt.runners[runnerID] = &runnerInfo{
		runner: runner,
		id:     runnerID,
		server: serverAddr,
		mode:   mode,
	}
	return nil
}

func (lt *LoadTester) RemoveRunner(runnerID string) error {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	info, exists := lt.runners[runnerID]
	if !exists {
		return fmt.Errorf("runner %s not found", runnerID)
	}

	info.runner.Close()
	delete(lt.runners, runnerID)
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
	info, exists := lt.runners[runnerID]

	if !exists {
		return fmt.Errorf("runner %s not found", runnerID)
	}

	err = info.runner.SetConfig(&worker.WorkerConfig{
		InFlight:          inFlight,
		IntervalGenerator: generator,
		Qps:               qps,
		Timeout:           timeout,
	})

	if err == nil {
		info.mode = mode
	}
	return err
}

type Status struct {
	Id             string
	Server         string
	LoadRunnerInfo *loadrunner.LoadRunnerInfo
	OkRequests     int
	ErrRequests    int
	Mode           string
}

func (lt *LoadTester) GetRunnersInfo(ctx context.Context) ([]*Status, error) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	res := make([]*Status, 0)

	for _, info := range lt.runners {
		// Extract metrics from Prometheus counters
		var successCount, errorCount int

		successMetric, err := lt.metrics.ResponseCounter.GetMetricWithLabelValues("ok", info.id)
		if err == nil && successMetric != nil {
			pb := &dto.Metric{}
			successMetric.Write(pb)
			successCount += int(pb.GetCounter().GetValue())
		}

		errorMetric, err := lt.metrics.ResponseCounter.GetMetricWithLabelValues("error", info.id)
		if err == nil && errorMetric != nil {
			pb := &dto.Metric{}
			errorMetric.Write(pb)
			errorCount += int(pb.GetCounter().GetValue())
		}

		lrInfo, err := info.runner.GetInfo()
		if err != nil {
			return nil, err
		}

		status := &Status{
			Id:             info.id,
			Server:         info.server,
			LoadRunnerInfo: lrInfo,
			OkRequests:     successCount,
			ErrRequests:    errorCount,
			Mode:           info.mode,
		}
		res = append(res, status)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].Id < res[j].Id
	})

	return res, nil
}

func (lt *LoadTester) Close() error {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	for _, info := range lt.runners {
		info.runner.Close()
	}
	return nil
}

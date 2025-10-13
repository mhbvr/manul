package main

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"sort"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"

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
	runner      *loadrunner.LoadRunner
	id          string
	loadType    string
	loadOptions map[string]string
	mode        string
}

// LoadConstructor is a function that creates a new Load instance
type LoadConstructor func() loadrunner.Load

type LoadTester struct {
	mu sync.RWMutex

	// Load registry: map from load type name to constructor
	loadRegistry map[string]LoadConstructor

	// Global max in flight for all runners
	maxInFlight int

	// Multiple runner instances
	runners      map[string]*runnerInfo
	nextRunnerID int

	metrics *Metrics
}

func NewLoadTester(maxInFlight int) (*LoadTester, error) {
	lt := &LoadTester{
		loadRegistry: make(map[string]LoadConstructor),
		maxInFlight:  maxInFlight,
		runners:      make(map[string]*runnerInfo),
		nextRunnerID: 0,
		metrics:      NewMetrics(),
	}

	// Register available load types
	lt.RegisterLoad(loadrunner.NewCatPhotoLoad)
	lt.RegisterLoad(loadrunner.NewCatPhotoStreamLoad)

	return lt, nil
}

// RegisterLoad registers a new load type in the registry
func (lt *LoadTester) RegisterLoad(constructor LoadConstructor) {
	load := constructor()
	// Get the concrete type name
	t := reflect.TypeOf(load)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	loadType := t.Name()
	lt.loadRegistry[loadType] = constructor
}

// GetAvailableLoadTypes returns a list of registered load types
func (lt *LoadTester) GetAvailableLoadTypes() []string {
	types := make([]string, 0, len(lt.loadRegistry))
	for loadType := range lt.loadRegistry {
		types = append(types, loadType)
	}
	sort.Strings(types)
	return types
}

// GetLoadOptions returns the available options for a specific load type
func (lt *LoadTester) GetLoadOptions(loadType string) (map[string]string, error) {
	constructor, exists := lt.loadRegistry[loadType]
	if !exists {
		return nil, fmt.Errorf("unknown load type: %s", loadType)
	}

	load := constructor()
	return load.Options(), nil
}

// GetMaxInFlight returns the global max in flight value
func (lt *LoadTester) GetMaxInFlight() int {
	return lt.maxInFlight
}

func (lt *LoadTester) AddRunner(
	loadType string,
	loadOptions map[string]string,
	inFlight int,
	qps float64,
	timeout time.Duration,
	mode string) error {

	// Validate load type
	constructor, exists := lt.loadRegistry[loadType]
	if !exists {
		return fmt.Errorf("unknown load type: %s", loadType)
	}

	generator, err := generator(mode)
	if err != nil {
		return err
	}

	lt.mu.Lock()
	defer lt.mu.Unlock()

	runnerID := fmt.Sprintf("%s-%d", loadType, lt.nextRunnerID)
	lt.nextRunnerID++

	// Create logger for this runner
	logger := log.New(log.Writer(), fmt.Sprintf("[%s] ", runnerID), log.LstdFlags)

	// Create load implementation
	load := constructor()

	runner, err := loadrunner.NewLoadRunner(
		context.Background(),
		lt.maxInFlight,
		&worker.WorkerConfig{
			InFlight:          inFlight,
			IntervalGenerator: generator,
			Qps:               qps,
			Timeout:           timeout,
		},
		load,
		loadrunner.WithLoadOptions(loadOptions),
		loadrunner.WithRecorder(func(durationSeconds float64, success bool) {
			lt.metrics.RecordRequest(runnerID, durationSeconds, success)
		}),
		loadrunner.WithLogger(logger),
	)
	if err != nil {
		return err
	}

	lt.runners[runnerID] = &runnerInfo{
		runner:      runner,
		id:          runnerID,
		loadType:    loadType,
		loadOptions: loadOptions,
		mode:        mode,
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
	LoadType       string
	LoadOptions    map[string]string
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
			LoadType:       info.loadType,
			LoadOptions:    info.loadOptions,
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

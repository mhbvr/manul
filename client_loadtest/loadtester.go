package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	pb "github.com/mhbvr/manul/proto"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type RunnerInstance struct {
	id        int
	idStr     string // For Prometheus metrics
	runner    *Runner
	startTime time.Time
	metrics   *Metrics
	cancel    context.CancelFunc

	client pb.CatPhotosServiceClient
	conn   *grpc.ClientConn

	cats   []uint64
	photos map[uint64][]uint64
}

func NewRunnerInstance(id int,
	serverAddr string,
	maxInFlight int,
	cfg *RunnerConfig,
	metrics *Metrics,
	photos map[uint64][]uint64) (*RunnerInstance, error) {

	// Create new gRPC connection
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}

	client := pb.NewCatPhotosServiceClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	res := &RunnerInstance{
		id:        id,
		idStr:     fmt.Sprintf("%d", id),
		client:    client,
		conn:      conn,
		cancel:    cancel,
		metrics:   metrics,
		startTime: time.Now(),
		photos:    photos,
		cats:      make([]uint64, 0),
	}

	// Populate cats list
	for k, _ := range photos {
		res.cats = append(res.cats, k)
	}

	// Create runner
	res.runner, err = NewRunner(cfg, res.job, maxInFlight)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create runner: %v", err)
	}

	go func() {
		err := res.runner.Run(ctx)
		log.Printf("Runner %d returns: %v", id, err)
		res.conn.Close()
	}()
	return res, nil
}

func (ri *RunnerInstance) job(ctx context.Context) error {
	// Pick random cat ID
	if len(ri.cats) == 0 {
		return errors.New("no cats available")
	}
	catID := ri.cats[rand.Intn(len(ri.cats))]

	// Pick random photo ID for that cat ID
	photos := ri.photos[catID]
	if len(photos) == 0 {
		return errors.New("no photos available for selected cat")
	}
	photoID := photos[rand.Intn(len(photos))]

	start := time.Now()
	_, err := ri.client.GetPhoto(ctx, &pb.GetPhotoRequest{
		CatId:   catID,
		PhotoId: photoID,
	})

	ri.metrics.RecordRequest(time.Since(start).Seconds(), err == nil, ri.idStr)
	return err
}

type LoadTester struct {
	mu          sync.RWMutex
	serverAddr  string
	maxInflight int
	runnerCfg   *RunnerConfig

	// Multiple runner instances
	runners      map[int]*RunnerInstance
	nextRunnerID int
	numRunners   int

	// Available cat/photo IDs fetched from server
	catIDs      []uint64
	photosByCat map[uint64][]uint64

	metrics   *Metrics
	startTime time.Time
}

func NewLoadTester(serverAddr string, maxInflight int, cfg *RunnerConfig, numRunners int) (*LoadTester, error) {
	// Default runner configuration
	if cfg == nil {
		return nil, fmt.Errorf("incorrect configuration: %v", *cfg)
	}

	lt := &LoadTester{
		serverAddr:   serverAddr,
		maxInflight:  maxInflight,
		runnerCfg:    cfg,
		runners:      make(map[int]*RunnerInstance),
		numRunners:   numRunners,
		nextRunnerID: 0,
		photosByCat:  make(map[uint64][]uint64),
		startTime:    time.Now(),
		metrics:      NewMetrics(),
	}

	// Fetch available IDs using temporary connection
	if err := lt.fetchAvailableIDs(); err != nil {
		return nil, fmt.Errorf("failed to fetch available cats and photos IDs: %v", err)
	}

	// Create initial runners
	for i := 0; i < numRunners; i++ {
		if err := lt.addRunner(); err != nil {
			lt.Close()
			return nil, fmt.Errorf("failed to create runner %d: %v", i, err)
		}
	}

	return lt, nil
}

func (lt *LoadTester) addRunner() error {
	runnerID := lt.nextRunnerID
	lt.nextRunnerID++

	runner, err := NewRunnerInstance(runnerID,
		lt.serverAddr,
		lt.maxInflight,
		lt.runnerCfg,
		lt.metrics,
		lt.photosByCat)
	if err != nil {
		return err
	}

	lt.runners[runnerID] = runner
	return nil
}

func (lt *LoadTester) removeRunner() {
	if len(lt.runners) == 0 {
		return
	}

	// Pick the highest ID runner
	var maxID int = -1
	for id := range lt.runners {
		if id > maxID {
			maxID = id
		}
	}

	runner := lt.runners[maxID]
	runner.cancel()
	delete(lt.runners, maxID)
}

func (lt *LoadTester) SetRunnerCount(count int) error {
	if count <= 0 {
		return fmt.Errorf("runner count must be non negative")
	}

	lt.mu.Lock()
	defer lt.mu.Unlock()
	currentCount := len(lt.runners)

	for i := currentCount; i < count; i++ {
		if err := lt.addRunner(); err != nil {
			return fmt.Errorf("failed to add runner: %v", err)
		}
	}

	for i := currentCount; i > count; i-- {
		lt.removeRunner()
	}

	return nil
}

func (lt *LoadTester) fetchAvailableIDs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create temporary connection for fetching IDs
	conn, err := grpc.NewClient(lt.serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewCatPhotosServiceClient(conn)

	// Get all cat IDs
	catsResp, err := client.ListCats(ctx, &pb.ListCatsRequest{})
	if err != nil {
		return err
	}

	log.Printf("Found %d cats.", len(catsResp.CatIds))

	// Get photo IDs for each cat
	for _, catID := range catsResp.CatIds {
		photosResp, err := client.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID})
		if err != nil {
			log.Printf("Failed to get photos for cat %d: %v", catID, err)
			continue
		}
		lt.photosByCat[catID] = photosResp.PhotoIds
		log.Printf("Cat %d has %d photos.", catID, len(photosResp.PhotoIds))
	}

	return nil
}

func (lt *LoadTester) SetConfig(ctx context.Context, cfg *RunnerConfig) error {
	lt.mu.Lock()
	lt.runnerCfg = cfg
	runners := make([]*Runner, 0, len(lt.runners))
	for _, instance := range lt.runners {
		runners = append(runners, instance.runner)
	}
	lt.mu.Unlock()

	// Update all runners with new config
	for _, runner := range runners {
		if err := runner.SetConfig(ctx, *cfg); err != nil {
			return err
		}
	}
	return nil
}

type RunnerInfo struct {
	RunnerCfg   *RunnerConfig
	StartTime   time.Time
	OkRequests  int
	ErrRequests int
	RunnerID    int
}

func (lt *LoadTester) GetRunnersInfo(ctx context.Context) ([]*RunnerInfo, error) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	res := make([]*RunnerInfo, 0)

	for id, r := range lt.runners {
		// Extract metrics from Prometheus counters (sum across all runners)
		var successCount, errorCount int
		runnerIDStr := fmt.Sprintf("%d", id)

		successMetric, err := lt.metrics.ResponseCounter.GetMetricWithLabelValues("ok", runnerIDStr)
		if err == nil && successMetric != nil {
			pb := &dto.Metric{}
			successMetric.Write(pb)
			successCount += int(pb.GetCounter().GetValue())
		}

		errorMetric, err := lt.metrics.ResponseCounter.GetMetricWithLabelValues("error", runnerIDStr)
		if err == nil && errorMetric != nil {
			pb := &dto.Metric{}
			errorMetric.Write(pb)
			errorCount += int(pb.GetCounter().GetValue())
		}

		cfg, err := r.runner.GetConfig(ctx)
		if err != nil {
			return nil, err
		}

		info := &RunnerInfo{
			RunnerCfg:   cfg,
			StartTime:   r.startTime,
			OkRequests:  successCount,
			ErrRequests: errorCount,
			RunnerID:    r.id,
		}
		res = append(res, info)
	}

	return res, nil
}

func (lt *LoadTester) GetConfig(ctx context.Context) (*RunnerConfig, error) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	if len(lt.runners) == 0 {
		return nil, errors.New("no runners initialized")
	}

	return lt.runnerCfg, nil
}

func (lt *LoadTester) GetRunnerCount() int {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	return len(lt.runners)
}

func (lt *LoadTester) Close() error {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	for _, instance := range lt.runners {
		instance.cancel()
	}
	return nil
}

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

type LoadTester struct {
	mu          sync.RWMutex
	serverAddr  string
	maxInflight int
	runnerCfg   *RunnerConfig

	client pb.CatPhotosServiceClient
	conn   *grpc.ClientConn
	runner *Runner
	fg     *RunnerConfig

	// Available cat/photo IDs fetched from server
	catIDs      []uint64
	photosByCat map[uint64][]uint64

	metrics   *Metrics
	startTime time.Time
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewLoadTester(serverAddr string, maxInflight int, cfg *RunnerConfig) (*LoadTester, error) {
	// Default runner configuration
	if cfg == nil {
		return nil, fmt.Errorf("incorrect configuration: %v", *cfg)
	}

	lt := &LoadTester{
		serverAddr:  serverAddr,
		maxInflight: maxInflight,
		runnerCfg:   cfg,
		photosByCat: make(map[uint64][]uint64),
		startTime:   time.Now(),
		metrics:     NewMetrics(),
	}

	if err := lt.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to server %v: %v", serverAddr, err)
	}

	if err := lt.fetchAvailableIDs(); err != nil {
		return nil, fmt.Errorf("failed to fetch available cats and photos IDs: %v", err)
	}

	// Create Runner with job function
	var err error
	lt.runner, err = NewRunner(cfg, lt.createJobFunc(), lt.maxInflight)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %v", err)
	}

	// Start the runner
	lt.ctx, lt.cancel = context.WithCancel(context.Background())
	go func() {
		if err := lt.runner.Run(lt.ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Runner stopped with error: %v", err)
		}
	}()

	return lt, nil
}

func (lt *LoadTester) createJobFunc() func(context.Context) error {
	return func(ctx context.Context) error {
		start := time.Now()

		catID, photoID, err := lt.getRandomCatPhoto()
		if err != nil {
			duration := time.Since(start).Seconds()
			lt.metrics.RecordRequest(duration, false)
			return err
		}

		_, err = lt.client.GetPhoto(ctx, &pb.GetPhotoRequest{
			CatId:   catID,
			PhotoId: photoID,
		})

		duration := time.Since(start).Seconds()
		success := err == nil
		lt.metrics.RecordRequest(duration, success)

		return err
	}
}

func (lt *LoadTester) connect() error {
	var err error
	lt.conn, err = grpc.NewClient(lt.serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	lt.client = pb.NewCatPhotosServiceClient(lt.conn)
	return nil
}

func (lt *LoadTester) fetchAvailableIDs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get all cat IDs
	catsResp, err := lt.client.ListCats(ctx, &pb.ListCatsRequest{})
	if err != nil {
		return err
	}
	lt.catIDs = catsResp.CatIds
	log.Printf("Found %d cats.", len(lt.catIDs))

	// Get photo IDs for each cat
	for _, catID := range lt.catIDs {
		photosResp, err := lt.client.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID})
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
	return lt.runner.SetConfig(ctx, cfg)
}

type RunnerInfo struct {
	RunnerCfg   *RunnerConfig
	StartTime   time.Time
	OkRequests  int
	ErrRequests int
}

func (lt *LoadTester) GetInfo(ctx context.Context) (*RunnerInfo, error) {
	if lt.runner == nil {
		return nil, errors.New("runner not initialized")
	}

	cfg, err := lt.runner.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Extract metrics from Prometheus counters
	var successCount, errorCount int
	successMetric, err := lt.metrics.ResponseCounter.GetMetricWithLabelValues("ok")
	if err != nil {
		return nil, err
	}
	errorMetric, _ := lt.metrics.ResponseCounter.GetMetricWithLabelValues("error")
	if err != nil {
		return nil, err
	}

	// Get current values from Prometheus metrics
	if successMetric != nil {
		pb := &dto.Metric{}
		successMetric.Write(pb)
		successCount = int(pb.GetCounter().GetValue())
	}

	if errorMetric != nil {
		pb := &dto.Metric{}
		errorMetric.Write(pb)
		errorCount = int(pb.GetCounter().GetValue())
	}

	return &RunnerInfo{
		RunnerCfg:   cfg,
		StartTime:   lt.startTime,
		OkRequests:  successCount,
		ErrRequests: errorCount,
	}, nil
}

func (lt *LoadTester) GetConfig(ctx context.Context) (*RunnerConfig, error) {
	if lt.runner == nil {
		return nil, errors.New("runner not initialized")
	}

	cfg, err := lt.runner.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func (lt *LoadTester) getRandomCatPhoto() (uint64, uint64, error) {
	if len(lt.catIDs) == 0 {
		return 0, 0, errors.New("no cats available")
	}

	// Pick random cat
	catID := lt.catIDs[rand.Intn(len(lt.catIDs))]

	// Pick random photo for that cat
	photos := lt.photosByCat[catID]
	if len(photos) == 0 {
		return 0, 0, errors.New("no photos available for selected cat")
	}

	photoID := photos[rand.Intn(len(photos))]
	return catID, photoID, nil
}

func (lt *LoadTester) Close() error {
	if lt.cancel != nil {
		lt.cancel()
	}
	if lt.conn != nil {
		return lt.conn.Close()
	}
	return nil
}

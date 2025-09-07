package main

import (
	"context"
	"errors"
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
	
	client      pb.CatPhotosServiceClient
	conn        *grpc.ClientConn
	runner      *Runner
	
	// Available cat/photo IDs fetched from server
	catIDs       []uint64
	photosByCat  map[uint64][]uint64
	
	metrics     *Metrics
	startTime   time.Time
	ctx         context.Context
	cancel      context.CancelFunc
}

type Option func(*LoadTester)

func WithServerAddr(addr string) Option {
	return func(lt *LoadTester) {
		lt.serverAddr = addr
	}
}

func WithMaxInflight(maxInflight int) Option {
	return func(lt *LoadTester) {
		lt.maxInflight = maxInflight
	}
}

func WithInflight(inflight int) Option {
	return func(lt *LoadTester) {
		// This will be passed to Runner config
	}
}

func WithMode(mode string) Option {
	return func(lt *LoadTester) {
		// This will be passed to Runner config
	}
}

func WithRPS(rps float64) Option {
	return func(lt *LoadTester) {
		// This will be passed to Runner config
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(lt *LoadTester) {
		// This will be passed to Runner config
	}
}

func NewLoadTester(opts ...Option) *LoadTester {
	lt := &LoadTester{
		serverAddr:  "localhost:8081",
		maxInflight: 10,
		photosByCat: make(map[uint64][]uint64),
		startTime:   time.Now(),
		metrics:     NewMetrics(),
	}
	
	for _, opt := range opts {
		opt(lt)
	}
	
	if err := lt.connect(); err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	
	if err := lt.fetchAvailableIDs(); err != nil {
		log.Fatalf("Failed to fetch available IDs: %v", err)
	}
	
	// Extract parameters from command line flags for initial config
	var inflight int = 5
	var mode string = "asap"
	var rps float64 = 1.0
	var timeout time.Duration = 10 * time.Second
	
	// Create initial Runner config
	runnerConfig := &RunnerConfig{
		Inflight: inflight,
		Mode:     mode,
		Rps:      rps,
		Timeout:  timeout,
	}
	
	// Create Runner with job function
	var err error
	lt.runner, err = NewRunner(runnerConfig, lt.createJobFunc(), lt.maxInflight)
	if err != nil {
		log.Fatalf("Failed to create runner: %v", err)
	}
	
	// Start the runner
	lt.ctx, lt.cancel = context.WithCancel(context.Background())
	go func() {
		if err := lt.runner.Run(lt.ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Runner stopped with error: %v", err)
		}
	}()
	
	return lt
}

func NewLoadTesterWithConfig(serverAddr string, maxInflight, inflight int, mode string, rps float64, timeout time.Duration) *LoadTester {
	lt := &LoadTester{
		serverAddr:  serverAddr,
		maxInflight: maxInflight,
		photosByCat: make(map[uint64][]uint64),
		startTime:   time.Now(),
		metrics:     NewMetrics(),
	}
	
	if err := lt.connect(); err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	
	if err := lt.fetchAvailableIDs(); err != nil {
		log.Fatalf("Failed to fetch available IDs: %v", err)
	}
	
	// Create initial Runner config
	runnerConfig := &RunnerConfig{
		Inflight: inflight,
		Mode:     mode,
		Rps:      rps,
		Timeout:  timeout,
	}
	
	// Create Runner with job function
	var err error
	lt.runner, err = NewRunner(runnerConfig, lt.createJobFunc(), lt.maxInflight)
	if err != nil {
		log.Fatalf("Failed to create runner: %v", err)
	}
	
	// Start the runner
	lt.ctx, lt.cancel = context.WithCancel(context.Background())
	go func() {
		if err := lt.runner.Run(lt.ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Runner stopped with error: %v", err)
		}
	}()
	
	return lt
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
	log.Printf("Found %d cats: %v", len(lt.catIDs), lt.catIDs)
	
	// Get photo IDs for each cat
	for _, catID := range lt.catIDs {
		photosResp, err := lt.client.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID})
		if err != nil {
			log.Printf("Failed to get photos for cat %d: %v", catID, err)
			continue
		}
		lt.photosByCat[catID] = photosResp.PhotoIds
		log.Printf("Cat %d has %d photos: %v", catID, len(photosResp.PhotoIds), photosResp.PhotoIds)
	}
	
	return nil
}

func (lt *LoadTester) SetInflight(inflight int) error {
	if lt.runner == nil {
		return errors.New("runner not initialized")
	}
	
	// Get current config
	currentCfg, err := lt.runner.GetConfig(context.Background())
	if err != nil {
		return err
	}
	
	// Update only the inflight value
	newCfg := &RunnerConfig{
		Inflight: inflight,
		Mode:     currentCfg.Mode,
		Rps:      currentCfg.Rps,
		Timeout:  currentCfg.Timeout,
	}
	
	return lt.runner.SetConfig(context.Background(), newCfg)
}

func (lt *LoadTester) SetMode(mode string) error {
	if lt.runner == nil {
		return errors.New("runner not initialized")
	}
	
	// Get current config
	currentCfg, err := lt.runner.GetConfig(context.Background())
	if err != nil {
		return err
	}
	
	// Update only the mode value
	newCfg := &RunnerConfig{
		Inflight: currentCfg.Inflight,
		Mode:     mode,
		Rps:      currentCfg.Rps,
		Timeout:  currentCfg.Timeout,
	}
	
	return lt.runner.SetConfig(context.Background(), newCfg)
}

func (lt *LoadTester) SetRPS(rps float64) error {
	if lt.runner == nil {
		return errors.New("runner not initialized")
	}
	
	// Get current config
	currentCfg, err := lt.runner.GetConfig(context.Background())
	if err != nil {
		return err
	}
	
	// Update only the RPS value
	newCfg := &RunnerConfig{
		Inflight: currentCfg.Inflight,
		Mode:     currentCfg.Mode,
		Rps:      rps,
		Timeout:  currentCfg.Timeout,
	}
	
	return lt.runner.SetConfig(context.Background(), newCfg)
}

func (lt *LoadTester) SetTimeout(timeout time.Duration) error {
	if lt.runner == nil {
		return errors.New("runner not initialized")
	}
	
	// Get current config
	currentCfg, err := lt.runner.GetConfig(context.Background())
	if err != nil {
		return err
	}
	
	// Update only the timeout value
	newCfg := &RunnerConfig{
		Inflight: currentCfg.Inflight,
		Mode:     currentCfg.Mode,
		Rps:      currentCfg.Rps,
		Timeout:  timeout,
	}
	
	return lt.runner.SetConfig(context.Background(), newCfg)
}

func (lt *LoadTester) GetStats() (int64, int64, int64, float64) {
	// Extract metrics from Prometheus counters
	successMetric, _ := lt.metrics.ResponseCounter.GetMetricWithLabelValues("success")
	errorMetric, _ := lt.metrics.ResponseCounter.GetMetricWithLabelValues("error")
	
	var successCount, errorCount int64
	
	// Get current values from Prometheus metrics
	if successMetric != nil {
		pb := &dto.Metric{}
		successMetric.Write(pb)
		successCount = int64(pb.GetCounter().GetValue())
	}
	
	if errorMetric != nil {
		pb := &dto.Metric{}
		errorMetric.Write(pb)
		errorCount = int64(pb.GetCounter().GetValue())
	}
	
	totalRequests := successCount + errorCount
	
	duration := time.Since(lt.startTime).Seconds()
	var currentRPS float64
	if duration > 0 {
		currentRPS = float64(totalRequests) / duration
	}
	
	return totalRequests, successCount, errorCount, currentRPS
}

func (lt *LoadTester) GetConfig() (string, int, string, float64, time.Duration, error) {
	if lt.runner == nil {
		return lt.serverAddr, 0, "", 0, 0, errors.New("runner not initialized")
	}
	
	cfg, err := lt.runner.GetConfig(context.Background())
	if err != nil {
		return lt.serverAddr, 0, "", 0, 0, err
	}
	
	return lt.serverAddr, cfg.Inflight, cfg.Mode, cfg.Rps, cfg.Timeout, nil
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
package loadrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"time"

	_ "github.com/mhbvr/manul/k8s_grpc_resolver"
	pb "github.com/mhbvr/manul/proto"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mhbvr/manul/client_loadtest/worker"
)

var (
	lrClosed = errors.New("LoadRunner closed")
	tracer   = otel.Tracer("load_runner")
)

type LoadRunner struct {
	id          string
	serverAddr  string
	worker      *worker.Worker
	maxInFlight int

	recorder func(string, float64, bool)

	ctx    context.Context
	cancel context.CancelCauseFunc

	client pb.CatPhotosServiceClient
	conn   *grpc.ClientConn

	cats   []uint64
	photos map[uint64][]uint64

	startTime time.Time
	logger    *log.Logger
}

type LoadRunnerInfo struct {
	Id          string
	Server      string
	StartTime   time.Time
	MaxInFlight int
	WorkerCfg   *worker.WorkerConfig
}

type Option func(*LoadRunner)

func NewLoadRunner(ctx context.Context,
	id string,
	serverAddr string,
	maxInFlight int,
	cfg *worker.WorkerConfig,
	opts ...Option) (*LoadRunner, error) {

	// Create new gRPC connection with OpenTelemetry instrumentation and round robin load balancing
	conn, err := grpc.NewClient(serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}

	client := pb.NewCatPhotosServiceClient(conn)

	ctx, cancel := context.WithCancelCause(context.Background())
	res := &LoadRunner{
		id:          id,
		serverAddr:  serverAddr,
		client:      client,
		conn:        conn,
		ctx:         ctx,
		cancel:      cancel,
		maxInFlight: maxInFlight,
		startTime:   time.Now(),
		photos:      make(map[uint64][]uint64),
		cats:        make([]uint64, 0),
		logger:      log.New(io.Discard, "", 0),
	}

	for _, opt := range opts {
		opt(res)
	}

	// Fetch available IDs
	if err := res.fetchAvailableIDs(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to fetch available cats and photos: %v", err)
	}

	// Create runner
	res.worker, err = worker.NewWorker(ctx, res.job,
		worker.WithMaxInFlight(maxInFlight),
		worker.WithConfig(*cfg),
		worker.WithLogger(res.logger))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create runner: %v", err)
	}
	return res, nil
}

func WithLogger(logger *log.Logger) func(lr *LoadRunner) {
	return func(lr *LoadRunner) {
		lr.logger = logger
	}
}

func WithRecorger(recorder func(string, float64, bool)) func(*LoadRunner) {
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
		Id:          lr.id,
		Server:      lr.serverAddr,
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
	lr.conn.Close()
}

func (lr *LoadRunner) job(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "get_cat_photo_job", trace.WithNewRoot())

	// Pick random cat ID
	if len(lr.cats) == 0 {
		return errors.New("no cats available")
	}
	catID := lr.cats[rand.Intn(len(lr.cats))]

	// Pick random photo ID for that cat ID
	photos := lr.photos[catID]
	if len(photos) == 0 {
		return errors.New("no photos available for selected cat")
	}
	photoID := photos[rand.Intn(len(photos))]

	span.AddEvent("looking for photo", trace.WithAttributes(
		attribute.Int("cat_id", int(catID)),
		attribute.Int("photo_id", int(photoID)),
	))
	start := time.Now()
	_, err := lr.client.GetPhoto(ctx, &pb.GetPhotoRequest{
		CatId:   catID,
		PhotoId: photoID,
	})

	if lr.recorder != nil {
		lr.recorder(lr.id, time.Since(start).Seconds(), err == nil)
	}

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
	return err
}

func (lr *LoadRunner) fetchAvailableIDs() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get all cat IDs
	catsResp, err := lr.client.ListCats(ctx, &pb.ListCatsRequest{})
	if err != nil {
		return err
	}

	lr.cats = catsResp.CatIds
	lr.logger.Printf("Found %d cats.", len(catsResp.CatIds))

	// Get photo IDs for each cat
	var total int
	for _, catID := range catsResp.CatIds {
		photosResp, err := lr.client.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID})
		if err != nil {
			lr.logger.Printf("Failed to get photos for cat %d: %v", catID, err)
			continue
		}
		lr.photos[catID] = photosResp.PhotoIds
		total += len(photosResp.PhotoIds)

	}
	lr.logger.Printf("Found %d photos.", total)

	return nil
}

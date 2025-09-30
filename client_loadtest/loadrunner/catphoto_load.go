package loadrunner

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	pb "github.com/mhbvr/manul/proto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

var (
	tracer = otel.Tracer("load_runner")
)

// CatPhotoLoad implements the Load interface for cat photo load testing.
type CatPhotoLoad struct {
	serverAddr string
	grpcOpts   []grpc.DialOption
	client     pb.CatPhotosServiceClient
	conn       *grpc.ClientConn

	cats   []uint64
	photos map[uint64][]uint64
}

// NewCatPhotoLoad creates a new CatPhotoLoad instance.
func NewCatPhotoLoad(serverAddr string, grpcOpts []grpc.DialOption) *CatPhotoLoad {
	return &CatPhotoLoad{
		serverAddr: serverAddr,
		grpcOpts:   grpcOpts,
		photos:     make(map[uint64][]uint64),
		cats:       make([]uint64, 0),
	}
}

// Init creates the gRPC connection and fetches available cat and photo IDs from the server.
func (l *CatPhotoLoad) Init(ctx context.Context) error {
	// Create new gRPC connection
	conn, err := grpc.NewClient(l.serverAddr, l.grpcOpts...)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}

	l.conn = conn
	l.client = pb.NewCatPhotosServiceClient(conn)

	// Fetch available IDs
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Get all cat IDs
	catsResp, err := l.client.ListCats(ctx, &pb.ListCatsRequest{})
	if err != nil {
		l.conn.Close()
		return err
	}

	l.cats = catsResp.CatIds

	// Get photo IDs for each cat
	for _, catID := range catsResp.CatIds {
		photosResp, err := l.client.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID})
		if err != nil {
			continue
		}
		l.photos[catID] = photosResp.PhotoIds
	}

	return nil
}

// Job executes a single cat photo retrieval operation.
// Returns the duration of the operation and any error that occurred.
func (l *CatPhotoLoad) Job(ctx context.Context) (time.Duration, error) {
	ctx, span := tracer.Start(ctx, "get_cat_photo_job", trace.WithNewRoot())
	defer span.End()

	// Pick random cat ID
	if len(l.cats) == 0 {
		span.SetStatus(codes.Error, "no cats available")
		return 0, errors.New("no cats available")
	}
	catID := l.cats[rand.Intn(len(l.cats))]

	// Pick random photo ID for that cat ID
	photos := l.photos[catID]
	if len(photos) == 0 {
		span.SetStatus(codes.Error, "no photos available for selected cat")
		return 0, errors.New("no photos available for selected cat")
	}
	photoID := photos[rand.Intn(len(photos))]

	span.AddEvent("looking for photo", trace.WithAttributes(
		attribute.Int("cat_id", int(catID)),
		attribute.Int("photo_id", int(photoID)),
	))

	start := time.Now()
	_, err := l.client.GetPhoto(ctx, &pb.GetPhotoRequest{
		CatId:   catID,
		PhotoId: photoID,
	})
	duration := time.Since(start)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return duration, err
}

// Close closes the gRPC connection.
func (l *CatPhotoLoad) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}

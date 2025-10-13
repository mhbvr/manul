package loadrunner

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"time"

	pb "github.com/mhbvr/manul/proto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	streamTracer = otel.Tracer("stream_load_runner")
)

// CatPhotoStreamLoad implements the Load interface using streaming gRPC.
type CatPhotoStreamLoad struct {
	*catPhotoData
	Addr         string `name:"addr" description:"Server address to connect"`
	Balancer     string `name:"balancer" description:"gRPC load balancing policy"`
	MinBatchSize int    `name:"min_batch_size" description:"Minimum number of photos to request per stream"`
	MaxBatchSize int    `name:"max_batch_size" description:"Maximum number of photos to request per stream"`
}

// NewCatPhotoStreamLoad creates a new streaming load implementation.
func NewCatPhotoStreamLoad() *CatPhotoStreamLoad {
	return &CatPhotoStreamLoad{}
}

func (l *CatPhotoStreamLoad) Options() map[string]string {
	return GetOptionsDesc(l)
}

// Init creates the gRPC connection and fetches available cat and photo IDs from the server.
func (l *CatPhotoStreamLoad) Init(ctx context.Context, options map[string]string) error {
	err := ParseOptions(options, l)
	if err != nil {
		return err
	}
	data, err := initCatPhotoData(ctx, l.Addr, l.Balancer)
	if err != nil {
		return err
	}
	l.catPhotoData = data
	return nil
}

// Job executes a single streaming photo retrieval operation.
// Returns the duration of the operation and any error that occurred.
func (l *CatPhotoStreamLoad) Job(ctx context.Context) (time.Duration, error) {
	ctx, span := streamTracer.Start(ctx, "get_cat_photos_stream", trace.WithNewRoot())
	defer span.End()

	// Calculate random batch size
	batchSize := l.MinBatchSize
	if l.MaxBatchSize > l.MinBatchSize {
		batchSize = l.MinBatchSize + rand.Intn(l.MaxBatchSize-l.MinBatchSize+1)
	}

	// Build a batch of random photo requests
	photoRequests := make([]*pb.PhotoRequest, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		catID, photoID, err := l.getRandomPhoto()
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return 0, err
		}

		photoRequests = append(photoRequests, &pb.PhotoRequest{
			CatId:   catID,
			PhotoId: photoID,
		})
	}

	span.AddEvent("requesting photos", trace.WithAttributes(
		attribute.Int("batch_size", len(photoRequests)),
	))

	start := time.Now()
	stream, err := l.client.GetPhotosStream(ctx, &pb.GetPhotosStreamRequest{
		PhotoRequests: photoRequests,
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return time.Since(start), err
	}

	// Receive all responses
	var receivedCount int
	var errorCount int
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return time.Since(start), err
		}

		receivedCount++
		if !resp.Success {
			errorCount++
		}
	}

	duration := time.Since(start)

	span.AddEvent("received responses", trace.WithAttributes(
		attribute.Int("received_count", receivedCount),
		attribute.Int("error_count", errorCount),
	))

	if errorCount > 0 {
		span.SetStatus(codes.Error, fmt.Sprintf("%d photos failed", errorCount))
		return duration, fmt.Errorf("%d out of %d photos failed", errorCount, receivedCount)
	}

	span.SetStatus(codes.Ok, "")
	return duration, nil
}

// Close closes the gRPC connection.
func (l *CatPhotoStreamLoad) Close() error {
	return l.catPhotoData.close()
}

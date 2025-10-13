package loadrunner

import (
	"context"
	"time"

	pb "github.com/mhbvr/manul/proto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("load_runner")
)

// CatPhotoLoad implements the Load interface for cat photo load testing.
type CatPhotoLoad struct {
	*catPhotoData
	Addr     string `name:"addr" description:"Server address to connect"`
	Balancer string `name:"balancer" description:"gRPC load balancing policy"`
}

// NewCatPhotoLoad creates a new CatPhotoLoad instance.
func NewCatPhotoLoad() Load {
	return &CatPhotoLoad{}
}

func (l *CatPhotoLoad) Options() map[string]string {
	return GetOptionsDesc(l)
}

// Init creates the gRPC connection and fetches available cat and photo IDs from the server.
func (l *CatPhotoLoad) Init(ctx context.Context, options map[string]string) error {
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

// Job executes a single cat photo retrieval operation.
// Returns the duration of the operation and any error that occurred.
func (l *CatPhotoLoad) Job(ctx context.Context) (time.Duration, error) {
	ctx, span := tracer.Start(ctx, "get_cat_photo_job", trace.WithNewRoot())
	defer span.End()

	// Pick random cat and photo
	catID, photoID, err := l.getRandomPhoto()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, err
	}

	span.AddEvent("looking for photo", trace.WithAttributes(
		attribute.Int("cat_id", int(catID)),
		attribute.Int("photo_id", int(photoID)),
	))

	start := time.Now()
	_, err = l.client.GetPhoto(ctx, &pb.GetPhotoRequest{
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
	return l.catPhotoData.close()
}

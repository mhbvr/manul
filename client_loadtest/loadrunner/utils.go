package loadrunner

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc"
)

// catPhotoData holds the common data for cat photo load implementations.
type catPhotoData struct {
	serverAddr string
	grpcOpts   []grpc.DialOption
	client     pb.CatPhotosServiceClient
	conn       *grpc.ClientConn
	cats       []uint64
	photos     map[uint64][]uint64
}

// initCatPhotoData initializes the gRPC connection and fetches cat/photo IDs.
func initCatPhotoData(ctx context.Context, serverAddr string, grpcOpts []grpc.DialOption) (*catPhotoData, error) {
	data := &catPhotoData{
		serverAddr: serverAddr,
		grpcOpts:   grpcOpts,
		photos:     make(map[uint64][]uint64),
		cats:       make([]uint64, 0),
	}

	// Create new gRPC connection
	conn, err := grpc.NewClient(serverAddr, grpcOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}

	data.conn = conn
	data.client = pb.NewCatPhotosServiceClient(conn)

	// Fetch available IDs
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Get all cat IDs
	catsResp, err := data.client.ListCats(ctx, &pb.ListCatsRequest{})
	if err != nil {
		data.conn.Close()
		return nil, err
	}

	// Get photo IDs for each cat, only keeping cats with photos
	for _, catID := range catsResp.CatIds {
		photosResp, err := data.client.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID})
		if err != nil {
			continue
		}
		if len(photosResp.PhotoIds) > 0 {
			data.cats = append(data.cats, catID)
			data.photos[catID] = photosResp.PhotoIds
		}
	}

	return data, nil
}

// close closes the gRPC connection.
func (d *catPhotoData) close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

// getRandomPhoto returns a random cat ID and photo ID.
// Returns an error if no cats are available.
func (d *catPhotoData) getRandomPhoto() (catID uint64, photoID uint64, err error) {
	if len(d.cats) == 0 {
		return 0, 0, fmt.Errorf("no cats available")
	}

	catID = d.cats[rand.Intn(len(d.cats))]
	photos := d.photos[catID]
	photoID = photos[rand.Intn(len(photos))]

	return catID, photoID, nil
}

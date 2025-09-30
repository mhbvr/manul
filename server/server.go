package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"time"

	"github.com/mhbvr/manul"
	"github.com/mhbvr/manul/db/bolt"
	"github.com/mhbvr/manul/db/filetree"
	"github.com/mhbvr/manul/db/pebble"
	pb "github.com/mhbvr/manul/proto"
	"golang.org/x/image/draw"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/orca"
	"google.golang.org/grpc/status"
)

type CatPhotosServer struct {
	pb.UnimplementedCatPhotosServiceServer
	dbReader     manul.DBReader
	orcaReporter *ORCAReporter
	readLimiter  chan struct{}
}

func NewCatPhotosServer(dbPath, dbType string, maxConcurrentReads int, orcaReporter *ORCAReporter) (*CatPhotosServer, error) {
	var dbReader manul.DBReader
	var err error

	switch dbType {
	case "filetree":
		dbReader, err = filetree.NewReader(dbPath)
	case "bolt":
		dbReader, err = bolt.NewReader(dbPath)
	case "pebble":
		dbReader, err = pebble.NewReader(dbPath)
	default:
		return nil, fmt.Errorf("unknown database type: %s (must be 'filetree', 'bolt', or 'pebble')", dbType)
	}

	if err != nil {
		return nil, err
	}

	var readLimiter chan struct{}
	if maxConcurrentReads > 0 {
		readLimiter = make(chan struct{}, maxConcurrentReads)
	}

	return &CatPhotosServer{
		dbReader:     dbReader,
		orcaReporter: orcaReporter,
		readLimiter:  readLimiter,
	}, nil
}

func (s *CatPhotosServer) Close() error {
	return s.dbReader.Close()
}

func getScaler(algorithm pb.ScalingAlgorithm) draw.Scaler {
	switch algorithm {
	case pb.ScalingAlgorithm_NEAREST_NEIGHBOR:
		return draw.NearestNeighbor
	case pb.ScalingAlgorithm_BILINEAR:
		return draw.BiLinear
	case pb.ScalingAlgorithm_CATMULL_ROM:
		return draw.CatmullRom
	case pb.ScalingAlgorithm_APPROX_BILINEAR:
		return draw.ApproxBiLinear
	default:
		return draw.BiLinear // default to bilinear
	}
}

func scaleImage(photoData []byte, targetWidth uint32, algorithm pb.ScalingAlgorithm) ([]byte, error) {
	// Decode the image
	img, _, err := image.Decode(bytes.NewReader(photoData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %v", err)
	}

	// Get current dimensions
	bounds := img.Bounds()
	currentWidth := bounds.Dx()
	currentHeight := bounds.Dy()

	// If target width is greater than or equal to current width, return original
	if int(targetWidth) >= currentWidth {
		return photoData, nil
	}

	// Calculate new height maintaining aspect ratio
	newWidth := int(targetWidth)
	newHeight := int(float64(currentHeight) * float64(newWidth) / float64(currentWidth))

	// Create a new image with the target dimensions
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Scale the image using the specified algorithm
	scaler := getScaler(algorithm)
	scaler.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	// Encode the scaled image as JPEG
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85})
	if err != nil {
		return nil, fmt.Errorf("failed to encode scaled image: %v", err)
	}

	return buf.Bytes(), nil
}

func (s *CatPhotosServer) ListCats(ctx context.Context, req *pb.ListCatsRequest) (*pb.ListCatsResponse, error) {

	catIds, err := s.dbReader.GetAllCatIDs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get cat IDs: %v", err)
	}

	return &pb.ListCatsResponse{
		CatIds: catIds,
	}, nil
}

func (s *CatPhotosServer) ListPhotos(ctx context.Context, req *pb.ListPhotosRequest) (*pb.ListPhotosResponse, error) {
	var photoIds []uint64
	var err error
	photoIds, err = s.dbReader.GetPhotoIDs(req.CatId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get photo IDs: %v", err)
	}

	if len(photoIds) == 0 {
		return nil, status.Errorf(codes.NotFound, "cat with ID %d not found", req.CatId)
	}

	return &pb.ListPhotosResponse{
		PhotoIds: photoIds,
	}, nil
}

func (s *CatPhotosServer) GetPhoto(ctx context.Context, req *pb.GetPhotoRequest) (*pb.GetPhotoResponse, error) {
	startTime := time.Now()
	orca.CallMetricsRecorderFromContext(ctx)
	var photoData []byte
	var err error
	defer func() {
		if s.orcaReporter != nil {
			s.orcaReporter.RecordRequest(time.Since(startTime))
		}
	}()

	if s.readLimiter != nil {
		s.readLimiter <- struct{}{}
	}
	photoData, err = s.dbReader.GetPhotoData(req.CatId, req.PhotoId)
	if s.readLimiter != nil {
		<-s.readLimiter
	}

	if err != nil {
		return nil, status.Errorf(codes.NotFound, "photo with cat_id=%d, photo_id=%d not found: %v", req.CatId, req.PhotoId, err)
	}

	// Apply scaling if width > 0
	if req.Width > 0 {
		scaledData, err := scaleImage(photoData, req.Width, req.ScalingAlgorithm)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to scale image: %v", err)
		}
		photoData = scaledData
	}

	return &pb.GetPhotoResponse{
		PhotoData: photoData,
	}, nil
}

func (s *CatPhotosServer) GetPhotosStream(req *pb.GetPhotosStreamRequest, stream pb.CatPhotosService_GetPhotosStreamServer) error {
	var err error
	startTime := time.Now()
	orca.CallMetricsRecorderFromContext(stream.Context())
	defer func() {
		if s.orcaReporter != nil {
			s.orcaReporter.RecordRequest(time.Since(startTime))
		}
	}()

	for _, photoReq := range req.PhotoRequests {
		// Get photo data
		response := &pb.GetPhotosStreamResponse{
			CatId:   photoReq.CatId,
			PhotoId: photoReq.PhotoId,
			Success: true,
		}

		if s.readLimiter != nil {
			s.readLimiter <- struct{}{}
		}
		response.PhotoData, err = s.dbReader.GetPhotoData(photoReq.CatId, photoReq.PhotoId)
		if s.readLimiter != nil {
			<-s.readLimiter
		}

		if err != nil {
			// Send error response
			response.Success = false
			response.ErrorMessage = err.Error()
		}

		// Apply scaling if width > 0
		if err == nil && req.Width > 0 {
			response.PhotoData, err = scaleImage(response.PhotoData, req.Width, req.ScalingAlgorithm)
			if err != nil {
				response.Success = false
				response.ErrorMessage = fmt.Sprintf("failed to scale image: %v", err)
			}
		}

		// Send the response
		if err := stream.Send(response); err != nil {
			return fmt.Errorf("failed to send response: %v", err)
		}
	}

	return nil
}

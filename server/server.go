package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mhbvr/manul"
	"github.com/mhbvr/manul/db/bolt"
	"github.com/mhbvr/manul/db/filetree"
	"github.com/mhbvr/manul/db/pebble"
	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/orca"
	"google.golang.org/grpc/status"
)

type CatPhotosServer struct {
	pb.UnimplementedCatPhotosServiceServer
	dbReader     manul.DBReader
	orcaReporter *ORCAReporter
}

func NewCatPhotosServer(dbPath, dbType string, orcaReporter *ORCAReporter) (*CatPhotosServer, error) {
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

	return &CatPhotosServer{
		dbReader:     dbReader,
		orcaReporter: orcaReporter,
	}, nil
}

func (s *CatPhotosServer) Close() error {
	return s.dbReader.Close()
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

	photoData, err = s.dbReader.GetPhotoData(req.CatId, req.PhotoId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "photo with cat_id=%d, photo_id=%d not found: %v", req.CatId, req.PhotoId, err)
	}

	return &pb.GetPhotoResponse{
		PhotoData: photoData,
	}, nil
}

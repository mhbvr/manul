package main

import (
	"context"

	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CatPhotosServer struct {
	pb.UnimplementedCatPhotosServiceServer
	dbReader *DBReader
}

func NewCatPhotosServer(dbDir string) (*CatPhotosServer, error) {
	dbReader, err := NewDBReader(dbDir)
	if err != nil {
		return nil, err
	}

	return &CatPhotosServer{
		dbReader: dbReader,
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
	photoIds, err := s.dbReader.GetPhotoIDs(req.CatId)
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
	photoData, err := s.dbReader.GetPhotoData(req.CatId, req.PhotoId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "photo with cat_id=%d, photo_id=%d not found: %v", req.CatId, req.PhotoId, err)
	}

	return &pb.GetPhotoResponse{
		PhotoData: photoData,
	}, nil
}

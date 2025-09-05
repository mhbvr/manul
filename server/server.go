package main

import (
	"context"

	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CatPhotosServer struct {
	pb.UnimplementedCatPhotosServiceServer
	catPhotos map[uint64]map[uint64][]byte
}

func NewCatPhotosServer() *CatPhotosServer {
	server := &CatPhotosServer{
		catPhotos: make(map[uint64]map[uint64][]byte),
	}
	server.initializeTestData()
	return server
}

func (s *CatPhotosServer) initializeTestData() {
	// Initialize with some test data
	s.catPhotos[1] = make(map[uint64][]byte)
	s.catPhotos[2] = make(map[uint64][]byte)
	s.catPhotos[3] = make(map[uint64][]byte)

	// Add dummy photo data (in real implementation, these would be actual image bytes)
	s.catPhotos[1][1] = []byte("dummy_cat_1_photo_1_data")
	s.catPhotos[1][2] = []byte("dummy_cat_1_photo_2_data")
	s.catPhotos[2][1] = []byte("dummy_cat_2_photo_1_data")
	s.catPhotos[3][1] = []byte("dummy_cat_3_photo_1_data")
	s.catPhotos[3][2] = []byte("dummy_cat_3_photo_2_data")
	s.catPhotos[3][3] = []byte("dummy_cat_3_photo_3_data")
}

func (s *CatPhotosServer) ListCats(ctx context.Context, req *pb.ListCatsRequest) (*pb.ListCatsResponse, error) {
	var catIds []uint64
	for catId := range s.catPhotos {
		catIds = append(catIds, catId)
	}
	
	return &pb.ListCatsResponse{
		CatIds: catIds,
	}, nil
}

func (s *CatPhotosServer) ListPhotos(ctx context.Context, req *pb.ListPhotosRequest) (*pb.ListPhotosResponse, error) {
	photos, exists := s.catPhotos[req.CatId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "cat with ID %d not found", req.CatId)
	}
	
	var photoIds []uint64
	for photoId := range photos {
		photoIds = append(photoIds, photoId)
	}
	
	return &pb.ListPhotosResponse{
		PhotoIds: photoIds,
	}, nil
}

func (s *CatPhotosServer) GetPhoto(ctx context.Context, req *pb.GetPhotoRequest) (*pb.GetPhotoResponse, error) {
	photos, catExists := s.catPhotos[req.CatId]
	if !catExists {
		return nil, status.Errorf(codes.NotFound, "cat with ID %d not found", req.CatId)
	}
	
	photoData, photoExists := photos[req.PhotoId]
	if !photoExists {
		return nil, status.Errorf(codes.NotFound, "photo with ID %d not found for cat %d", req.PhotoId, req.CatId)
	}
	
	return &pb.GetPhotoResponse{
		PhotoData: photoData,
	}, nil
}

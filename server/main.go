package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc"
)

var (
	host = flag.String("host", "localhost", "Server host")
	port = flag.Int("port", 8081, "Server port")
)

func main() {
	flag.Parse()
	
	addr := fmt.Sprintf("%s:%d", *host, *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	catPhotosServer := NewCatPhotosServer()
	pb.RegisterCatPhotosServiceServer(s, catPhotosServer)

	log.Printf("gRPC server listening on %s", addr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

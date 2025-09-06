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
	host  = flag.String("host", "localhost", "Server host")
	port  = flag.Int("port", 8081, "Server port")
	dbDir = flag.String("db", "", "Database directory path")
)

func main() {
	flag.Parse()
	
	if *dbDir == "" {
		log.Fatal("Database directory must be specified with -db flag")
	}
	
	addr := fmt.Sprintf("%s:%d", *host, *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	catPhotosServer, err := NewCatPhotosServer(*dbDir)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	defer catPhotosServer.Close()
	
	pb.RegisterCatPhotosServiceServer(s, catPhotosServer)

	log.Printf("gRPC server listening on %s (using database: %s)", addr, *dbDir)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

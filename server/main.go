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
	host   = flag.String("host", "localhost", "Server host")
	port   = flag.Int("port", 8081, "Server port")
	dbPath = flag.String("db", "", "Database path (directory for filetree, file for bolt/pebble)")
	dbType = flag.String("db-type", "filetree", "Database type: filetree, bolt, or pebble")
)

func main() {
	flag.Parse()
	
	if *dbPath == "" {
		log.Fatal("Database path must be specified with -db flag")
	}
	
	addr := fmt.Sprintf("%s:%d", *host, *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	catPhotosServer, err := NewCatPhotosServer(*dbPath, *dbType)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	defer catPhotosServer.Close()
	
	pb.RegisterCatPhotosServiceServer(s, catPhotosServer)

	log.Printf("gRPC server listening on %s (using %s database: %s)", addr, *dbType, *dbPath)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

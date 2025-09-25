package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	pb "github.com/mhbvr/manul/proto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
)

var (
	host        = flag.String("host", "localhost", "Server host")
	port        = flag.Int("port", 8081, "Server port")
	metricsPort = flag.Int("metrics-port", 8082, "Prometheus metrics port")
	dbPath      = flag.String("db", "", "Database path (directory for filetree, file for bolt/pebble)")
	dbType      = flag.String("db-type", "filetree", "Database type: filetree, bolt, or pebble")
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

	s := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)

	catPhotosServer, err := NewCatPhotosServer(*dbPath, *dbType)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	defer catPhotosServer.Close()

	pb.RegisterCatPhotosServiceServer(s, catPhotosServer)

	grpc_prometheus.Register(s)
	grpc_prometheus.EnableHandlingTimeHistogram()

	go func() {
		metricsAddr := fmt.Sprintf("%s:%d", *host, *metricsPort)
		http.Handle("/metrics", promhttp.Handler())
		log.Printf("Prometheus metrics server listening on %s", metricsAddr)
		if err := http.ListenAndServe(metricsAddr, nil); err != nil {
			log.Fatalf("Failed to serve metrics: %v", err)
		}
	}()

	log.Printf("gRPC server listening on %s (using %s database: %s)", addr, *dbType, *dbPath)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	"github.com/mhbvr/manul/k8s_watcher"
	"google.golang.org/grpc"
	"google.golang.org/grpc/channelz/service"
	"google.golang.org/grpc/reflection"
)

var (
	port        = flag.Int("port", 18000, "EDS server port")
	namespace   = flag.String("namespace", "default", "Kubernetes namespace to watch")
	serviceName = flag.String("service", "", "Service name to watch (required)")
	clusterName = flag.String("cluster", "", "Envoy cluster name (defaults to service name)")
	nodeID      = flag.String("node-id", "envoy-node", "Node ID for Envoy")
	kubeconfig  = flag.String("kubeconfig", "", "Path to kubeconfig file (optional, uses in-cluster config if not provided)")
)

func main() {
	flag.Parse()

	if *serviceName == "" {
		log.Fatal("Service name is required (use -service flag)")
	}

	if *clusterName == "" {
		*clusterName = *serviceName
	}

	log.Printf("Starting Envoy Control Plane for service: %s", *serviceName)
	log.Printf("Cluster name: %s", *clusterName)
	log.Printf("Namespace: %s", *namespace)
	log.Printf("Node ID: %s", *nodeID)
	log.Printf("Port: %d", *port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create Kubernetes watcher
	watcher, err := k8s_watcher.NewK8sWatcher(ctx, *namespace, *serviceName, *kubeconfig)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes watcher: %v", err)
	}

	// Create EDS server
	edsServer := NewEDSServer(*nodeID, *clusterName)

	edsServer.Start(watcher)

	// Start gRPC server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen on port %d: %v", *port, err)
	}

	grpcServer := grpc.NewServer()

	// Register the EDS service
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, edsServer.GetServer())

	// Register reflection service for debugging and service discovery
	reflection.Register(grpcServer)

	// Register Channelz service for gRPC debugging and monitoring
	service.RegisterChannelzServiceToServer(grpcServer)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping server...")
		cancel()
		grpcServer.GracefulStop()
	}()

	log.Printf("EDS server listening on port %d", *port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC server: %v", err)
	}
}

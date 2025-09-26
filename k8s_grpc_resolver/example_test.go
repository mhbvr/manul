package k8s_grpc_resolver_test

import (
	"context"
	"log"
	"time"

	"github.com/mhbvr/manul/k8s_grpc_resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func ExampleK8sResolver() {
	// Import the resolver to register it
	_ = k8s_grpc_resolver.Package

	// Use the k8s:// scheme to connect to a service
	// Format: k8s://service.namespace:port
	target := "k8s://my-service.default:8080"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	log.Println("Successfully connected to Kubernetes service")
}
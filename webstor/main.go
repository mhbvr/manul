package main

import (
	"flag"
	"log"
	"net/http"
	"os"
)

func main() {
	var (
		storageDir = flag.String("storage", "./files", "Directory to serve files from")
		addr       = flag.String("addr", ":8080", "Address to listen on")
	)
	flag.Parse()

	// Ensure storage directory exists
	if err := os.MkdirAll(*storageDir, 0755); err != nil {
		log.Fatalf("Failed to create storage directory: %v", err)
	}

	// Setup server with all middleware and routes
	handler, cleanup, err := SetupServer(*storageDir)
	if err != nil {
		log.Fatalf("Failed to setup server: %v", err)
	}
	defer cleanup()

	log.Printf("Starting webstore server on %s", *addr)
	log.Printf("Serving files from: %s", *storageDir)
	log.Printf("Endpoints:")
	log.Printf("  GET /list - List all files")
	log.Printf("  GET /download/{filename} - Download a file")
	log.Printf("  GET /metrics - Prometheus metrics")
	log.Printf("  GET /tracez - OpenTelemetry trace debugging")

	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
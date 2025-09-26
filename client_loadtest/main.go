package main

import (
	"flag"
	"log"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/channelz/service"
)

func main() {
	var (
		webAddr      = flag.String("web_addr", "localhost:8080", "Web interface host:port")
		channelzAddr = flag.String("channelz_addr", "localhost:8081", "Channelz gRPC server host:port")
		maxInflight  = flag.Int("max-inflight", 10000, "Maximum number of in-flight requests per runner")
	)
	flag.Parse()

	zpagesHandler, cleanup, err := InitializeTracing()
	if err != nil {
		log.Fatalf("Failed to initialize tracing: %v", err)
	}
	defer cleanup()

	loadTester, err := NewLoadTester(*maxInflight)
	if err != nil {
		log.Fatal(err)
	}
	webHandler := NewWebHandler(loadTester, nil)

	// Start channelz gRPC server
	go func() {
		lis, err := net.Listen("tcp", *channelzAddr)
		if err != nil {
			log.Fatalf("Failed to listen for channelz: %v", err)
		}
		s := grpc.NewServer()
		service.RegisterChannelzServiceToServer(s)
		log.Printf("Starting channelz gRPC server on %s", *channelzAddr)
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve channelz: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", webHandler.HandleIndex)
	mux.HandleFunc("POST /add-runner", webHandler.HandleAddRunner)
	mux.HandleFunc("POST /remove-runner", webHandler.HandleRemoveRunner)
	mux.HandleFunc("POST /update-runner", webHandler.HandleUpdateRunner)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("GET /tracez", zpagesHandler)

	log.Printf("Starting load tester web interface on %s", *webAddr)

	http.Handle("/", mux)
	log.Fatal(http.ListenAndServe(*webAddr, nil))
}

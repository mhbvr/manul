package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	var (
		webAddr     = flag.String("web_addr", "localhost:8080", "Web interface host:port")
		maxInflight = flag.Int("max-inflight", 10000, "Maximum number of in-flight requests per runner")
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

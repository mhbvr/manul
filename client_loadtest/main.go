package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	var (
		serverAddr  = flag.String("server", "localhost:8081", "gRPC server address")
		webAddr     = flag.String("web_addr", "localhost:8080", "Web interface host:port")
		maxInflight = flag.Int("max-inflight", 10, "Maximum number of in-flight requests")
		inflight    = flag.Int("inflight", 1, "Current number of in-flight requests")
		mode        = flag.String("mode", "asap", "Request mode: asap, stable, exponential")
		rps         = flag.Float64("rps", 1.0, "Requests per second (for stable/exponential modes)")
		timeout     = flag.Duration("timeout", 10*time.Second, "Timeout for individual requests")
	)
	flag.Parse()

	loadTester, err := NewLoadTester(*serverAddr, *maxInflight, &RunnerConfig{*inflight, *mode, *rps, *timeout})
	if err != nil {
		log.Fatal(err)
	}
	webHandler := NewWebHandler(loadTester)

	log.Printf("Starting load tester web interface on %s", *webAddr)
	log.Printf("Initial config: inflight=%d, max-inflight=%d, mode=%s, rps=%.2f, timeout=%v",
		*inflight, *maxInflight, *mode, *rps, *timeout)

	http.Handle("/", webHandler.HttpMux())
	log.Fatal(http.ListenAndServe(*webAddr, nil))
}

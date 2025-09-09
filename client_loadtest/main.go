package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	var (
		serverAddr  = flag.String("server", "localhost:8081", "gRPC server address")
		webAddr     = flag.String("web_addr", "localhost:8080", "Web interface host:port")
		maxInflight = flag.Int("max-inflight", 10000, "Maximum number of in-flight requests per runner")
	)
	flag.Parse()

	loadTester, err := NewLoadTester(*serverAddr, *maxInflight)
	if err != nil {
		log.Fatal(err)
	}
	webHandler := NewWebHandler(loadTester)

	log.Printf("Starting load tester web interface on %s", *webAddr)

	http.Handle("/", webHandler.HttpMux())
	log.Fatal(http.ListenAndServe(*webAddr, nil))
}

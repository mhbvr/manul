package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	pb "github.com/mhbvr/manul/proto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/channelz/service"
	"google.golang.org/grpc/orca"
)

// debugUnaryServerInterceptor logs all unary gRPC method calls when debug is enabled
func debugUnaryServerInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	start := time.Now()
	log.Printf("[DEBUG] gRPC unary request: method=%s req=%+v", info.FullMethod, req)

	resp, err := handler(ctx, req)
	duration := time.Since(start)

	if err != nil {
		log.Printf("[DEBUG] gRPC unary response: method=%s duration=%v error=%v", info.FullMethod, duration, err)
	} else {
		log.Printf("[DEBUG] gRPC unary response: method=%s duration=%v", info.FullMethod, duration)
	}

	return resp, err
}

// wrappedServerStream wraps grpc.ServerStream to intercept RecvMsg and SendMsg calls
type wrappedServerStream struct {
	grpc.ServerStream
	method string
}

func (w *wrappedServerStream) RecvMsg(m interface{}) error {
	err := w.ServerStream.RecvMsg(m)
	if err != nil {
		log.Printf("[DEBUG] gRPC stream RecvMsg: method=%s error=%v", w.method, err)
	} else {
		log.Printf("[DEBUG] gRPC stream RecvMsg: method=%s msg=%+v", w.method, m)
	}
	return err
}

func (w *wrappedServerStream) SendMsg(m interface{}) error {
	return w.ServerStream.SendMsg(m)
}

// debugStreamServerInterceptor logs all streaming gRPC method calls when debug is enabled
func debugStreamServerInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	start := time.Now()
	log.Printf("[DEBUG] gRPC stream start: method=%s", info.FullMethod)

	// Wrap the stream to log all RecvMsg and SendMsg calls
	wrappedStream := &wrappedServerStream{
		ServerStream: ss,
		method:       info.FullMethod,
	}

	err := handler(srv, wrappedStream)
	duration := time.Since(start)

	if err != nil {
		log.Printf("[DEBUG] gRPC stream end: method=%s duration=%v error=%v", info.FullMethod, duration, err)
	} else {
		log.Printf("[DEBUG] gRPC stream end: method=%s duration=%v", info.FullMethod, duration)
	}

	return err
}

var (
	host               = flag.String("host", "localhost", "Server host")
	port               = flag.Int("port", 8081, "Server port")
	metricsPort        = flag.Int("metrics-port", 8082, "Prometheus metrics port")
	dbPath             = flag.String("db", "", "Database path (directory for filetree, file for bolt/pebble)")
	dbType             = flag.String("db-type", "filetree", "Database type: filetree, bolt, or pebble")
	orcaEnabled        = flag.Bool("orca", false, "Enable ORCA load reporting")
	orcaThreshold      = flag.Int("orca-num-req-report", 10, "Update utilization after every N requests")
	maxConcurrentReads = flag.Int("max-concurrent-reads", 0, "Maximum number of concurrent database reads (0 = unlimited)")
	debug              = flag.Bool("debug", false, "Enable debug logging for all gRPC requests")
)

func main() {
	flag.Parse()

	if *debug {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		log.Println("Debug mode enabled - logging all gRPC requests")
	}

	if *dbPath == "" {
		log.Fatal("Database path must be specified with -db flag")
	}

	addr := fmt.Sprintf("%s:%d", *host, *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// Create ORCA reporter if enabled
	var orcaReporter *ORCAReporter
	var serverOptions []grpc.ServerOption

	if *orcaEnabled {
		orcaReporter = NewORCAReporter(*orcaThreshold)

		// Add call metrics interceptor for trailer-based reporting
		serverOptions = append(serverOptions, orca.CallMetricsServerOption(orcaReporter.GetServerMetricsProvider()))

		log.Printf("ORCA load reporting enabled (update after every %d requests)", *orcaThreshold)
	}

	// Build unary interceptor chain
	unaryInterceptors := []grpc.UnaryServerInterceptor{grpc_prometheus.UnaryServerInterceptor}
	if *debug {
		unaryInterceptors = append(unaryInterceptors, debugUnaryServerInterceptor)
	}
	serverOptions = append(serverOptions, grpc.ChainUnaryInterceptor(unaryInterceptors...))

	// Build stream interceptor chain
	streamInterceptors := []grpc.StreamServerInterceptor{grpc_prometheus.StreamServerInterceptor}
	if *debug {
		streamInterceptors = append(streamInterceptors, debugStreamServerInterceptor)
	}
	serverOptions = append(serverOptions, grpc.ChainStreamInterceptor(streamInterceptors...))

	s := grpc.NewServer(serverOptions...)

	catPhotosServer, err := NewCatPhotosServer(*dbPath, *dbType, *maxConcurrentReads, orcaReporter)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	defer catPhotosServer.Close()

	pb.RegisterCatPhotosServiceServer(s, catPhotosServer)

	// Register Channelz service for gRPC debugging and monitoring
	service.RegisterChannelzServiceToServer(s)

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

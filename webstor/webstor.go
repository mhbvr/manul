package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/zpages"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type FileInfo struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	CreationDate time.Time `json:"creation_date"`
}

type WebStore struct {
	storageDir string
	tracer     oteltrace.Tracer
}

var (
	// HTTP instrumentation metrics
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "webstor_http_request_duration_seconds",
			Help:    "Duration of HTTP requests",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "handler"},
	)

	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webstor_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "handler", "code"},
	)

	httpRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "webstor_http_requests_in_flight",
			Help: "Current number of HTTP requests being served",
		},
	)

	// Custom business metrics
	filesServed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "webstor_files_served_total",
			Help: "Total number of files served",
		},
	)

	bytesServed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "webstor_bytes_served_total",
			Help: "Total bytes served",
		},
	)
)

func init() {
	prometheus.MustRegister(httpRequestDuration)
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpRequestsInFlight)
	prometheus.MustRegister(filesServed)
	prometheus.MustRegister(bytesServed)
}

func NewWebStore(storageDir string) *WebStore {
	return &WebStore{
		storageDir: storageDir,
		tracer:     otel.Tracer("webstore"),
	}
}

// responseWriterWithStatus wraps http.ResponseWriter to capture status code
type responseWriterWithStatus struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (rw *responseWriterWithStatus) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterWithStatus) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = 200
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// loggingMiddleware logs each HTTP request with details
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code and bytes
		rw := &responseWriterWithStatus{
			ResponseWriter: w,
			statusCode:     200, // default status code
		}

		// Get client IP (handle potential proxy headers)
		clientIP := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			clientIP = strings.Split(xff, ",")[0]
		} else if xri := r.Header.Get("X-Real-IP"); xri != "" {
			clientIP = xri
		}

		// Call the next handler
		next.ServeHTTP(rw, r)

		// Log the request
		duration := time.Since(start)
		log.Printf("[%s] %s %s %d %d bytes %v %s \"%s\"",
			start.Format("2006-01-02 15:04:05"),
			r.Method,
			r.URL.Path,
			rw.statusCode,
			rw.bytesWritten,
			duration,
			clientIP,
			r.UserAgent(),
		)
	})
}

func (ws *WebStore) handleList(w http.ResponseWriter, r *http.Request) {
	_, span := ws.tracer.Start(r.Context(), "list_files")
	defer span.End()

	files, err := os.ReadDir(ws.storageDir)
	if err != nil {
		span.RecordError(err)
		http.Error(w, "Failed to read storage directory", http.StatusInternalServerError)
		return
	}

	var fileInfos []FileInfo
	for _, file := range files {
		if file.IsDir() {
			continue // Skip directories as requested
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		fileInfos = append(fileInfos, FileInfo{
			Name:         file.Name(),
			Size:         info.Size(),
			CreationDate: info.ModTime(),
		})
	}

	span.SetAttributes(attribute.Int("files.count", len(fileInfos)))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(fileInfos); err != nil {
		span.RecordError(err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (ws *WebStore) handleDownload(w http.ResponseWriter, r *http.Request) {
	ctx, span := ws.tracer.Start(r.Context(), "download_file")
	defer span.End()

	// Extract filename from URL path
	filename := strings.TrimPrefix(r.URL.Path, "/download/")
	if filename == "" {
		span.SetAttributes(attribute.String("error", "missing filename"))
		http.Error(w, "Filename is required", http.StatusBadRequest)
		return
	}

	// Security: prevent directory traversal
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		span.SetAttributes(attribute.String("error", "invalid filename"))
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	span.SetAttributes(attribute.String("file.name", filename))

	filePath := filepath.Join(ws.storageDir, filename)

	// Check if file exists and is not a directory
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			span.SetAttributes(attribute.String("error", "file not found"))
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			span.RecordError(err)
			http.Error(w, "Failed to access file", http.StatusInternalServerError)
		}
		return
	}

	if info.IsDir() {
		span.SetAttributes(attribute.String("error", "is directory"))
		http.Error(w, "Cannot download directory", http.StatusBadRequest)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		span.RecordError(err)
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	span.SetAttributes(
		attribute.Int64("file.size", info.Size()),
		attribute.String("file.content_type", "application/octet-stream"),
	)

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	written, err := io.Copy(w, file)
	if err != nil {
		span.RecordError(err)
		return
	}

	filesServed.Inc()
	bytesServed.Add(float64(written))
	span.SetAttributes(attribute.Int64("bytes.served", written))

	_ = ctx
}

func initializeTracing() (http.Handler, func(), error) {
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("webstore"),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource: %v", err)
	}

	zpagesProcessor := zpages.NewSpanProcessor()

	tp := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSpanProcessor(zpagesProcessor),
		trace.WithSampler(trace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tp.Shutdown(ctx)
	}

	return zpages.NewTracezHandler(zpagesProcessor), cleanup, nil
}

// SetupServer creates the HTTP server with all middleware and routes configured
func SetupServer(storageDir string) (http.Handler, func(), error) {
	// Initialize tracing
	zpagesHandler, cleanup, err := initializeTracing()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize tracing: %v", err)
	}

	// Create webstore instance
	ws := NewWebStore(storageDir)

	// Create HTTP multiplexer
	mux := http.NewServeMux()

	// Add handlers with promhttp instrumentation
	listHandler := promhttp.InstrumentHandlerDuration(
		httpRequestDuration.MustCurryWith(prometheus.Labels{"handler": "list"}),
		promhttp.InstrumentHandlerCounter(
			httpRequestsTotal.MustCurryWith(prometheus.Labels{"handler": "list"}),
			promhttp.InstrumentHandlerInFlight(
				httpRequestsInFlight,
				http.HandlerFunc(ws.handleList),
			),
		),
	)

	downloadHandler := promhttp.InstrumentHandlerDuration(
		httpRequestDuration.MustCurryWith(prometheus.Labels{"handler": "download"}),
		promhttp.InstrumentHandlerCounter(
			httpRequestsTotal.MustCurryWith(prometheus.Labels{"handler": "download"}),
			promhttp.InstrumentHandlerInFlight(
				httpRequestsInFlight,
				http.HandlerFunc(ws.handleDownload),
			),
		),
	)

	mux.Handle("GET /list", listHandler)
	mux.Handle("GET /download/{filename}", downloadHandler)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("GET /tracez", zpagesHandler)

	// Wrap the entire mux with middleware layers:
	// 1. Logging middleware (outermost)
	// 2. OpenTelemetry tracing middleware
	handler := loggingMiddleware(otelhttp.NewHandler(mux, "request"))

	return handler, cleanup, nil
}

package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the load tester
type Metrics struct {
	// Response counter by success/error status
	ResponseCounter *prometheus.CounterVec
	
	// Request latency histogram
	RequestLatency *prometheus.HistogramVec
}

// NewMetrics creates and registers new Prometheus metrics
func NewMetrics() *Metrics {
	return &Metrics{
		ResponseCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "loadtester_requests_total",
				Help: "Total number of requests sent by the load tester",
			},
			[]string{"status"}, // "success" or "error"
		),
		
		RequestLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "loadtester_request_duration_seconds",
				Help: "Request latency in seconds",
				Buckets: []float64{
					0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0,
				},
			},
			[]string{"status"}, // "success" or "error"
		),
	}
}

// RecordRequest records a completed request with its latency and status
func (m *Metrics) RecordRequest(durationSeconds float64, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	
	m.ResponseCounter.WithLabelValues(status).Inc()
	m.RequestLatency.WithLabelValues(status).Observe(durationSeconds)
}
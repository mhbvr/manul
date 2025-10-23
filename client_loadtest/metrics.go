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
			[]string{"status", "runner_id"}, // "success" or "error", runner identifier
		),

		RequestLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "loadtester_request_duration_seconds",
				Help: "Request latency in seconds",
				Buckets: []float64{
					0.001, 0.002, 0.004, 0.006, 0.008, 0.01, 0.02, 0.04, 0.06, 0.08, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0, 2.5, 5.0, 10.0,
				},
			},
			[]string{"status", "runner_id"}, // "success" or "error", runner identifier
		),
	}
}

// RecordRequest records a completed request with its latency and status
func (m *Metrics) RecordRequest(runnerID string, durationSeconds float64, success bool) {
	status := "ok"
	if !success {
		status = "error"
	}

	m.ResponseCounter.WithLabelValues(status, runnerID).Inc()
	m.RequestLatency.WithLabelValues(status, runnerID).Observe(durationSeconds)
}

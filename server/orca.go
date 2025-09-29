package main

import (
	"sync"
	"time"

	"google.golang.org/grpc/orca"
)

type ORCAReporter struct {
	serverMetrics orca.ServerMetricsRecorder
	mu            sync.RWMutex
	threshold     int
	requestCount  int
	totalReqTime  time.Duration
	start         time.Time
}

func NewORCAReporter(threshold int) *ORCAReporter {
	return &ORCAReporter{
		serverMetrics: orca.NewServerMetricsRecorder(),
		start:         time.Now(),
		threshold:     threshold,
	}
}

func (o *ORCAReporter) GetServerMetricsProvider() orca.ServerMetricsProvider {
	return o.serverMetrics
}

func (o *ORCAReporter) updateApplicationUtilization() {
	// Application utilization = total time of all requests / interval time
	intervalDuration := time.Since(o.start)
	if intervalDuration == 0 {
		return
	}
	appUtilization := float64(o.totalReqTime) / float64(intervalDuration)
	qps := float64(o.requestCount) / intervalDuration.Seconds()
	o.serverMetrics.SetApplicationUtilization(appUtilization)
	o.serverMetrics.SetQPS(qps)

	// Reset counters for next interval
	o.requestCount = 0
	o.totalReqTime = 0
	o.start = time.Now()
}

func (o *ORCAReporter) RecordRequest(duration time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.requestCount++
	o.totalReqTime += duration
	if o.requestCount > o.threshold {
		o.updateApplicationUtilization()
	}
}

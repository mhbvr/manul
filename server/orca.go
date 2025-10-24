package main

import (
	"context"
	"runtime/metrics"
	"sync"
	"time"

	"google.golang.org/grpc/orca"
)

type ORCAReporter struct {
	serverMetrics  orca.ServerMetricsRecorder
	mu             sync.Mutex
	updateInterval time.Duration
	requestCount   int
	cancel         context.CancelFunc
}

func NewORCAReporter(updateInterval time.Duration) *ORCAReporter {
	ctx, cancel := context.WithCancel(context.Background())
	reporter := &ORCAReporter{
		serverMetrics:  orca.NewServerMetricsRecorder(),
		updateInterval: updateInterval,
		cancel:         cancel,
	}

	// Start background goroutine to update CPU utilization
	go reporter.updateCPUUtilization(ctx)

	return reporter
}

func (o *ORCAReporter) GetServerMetricsProvider() orca.ServerMetricsProvider {
	return o.serverMetrics
}

func (o *ORCAReporter) updateCPUUtilization(ctx context.Context) {
	ticker := time.NewTicker(o.updateInterval)
	defer ticker.Stop()

	// Prepare sample slice for runtime metrics
	samples := []metrics.Sample{
		{Name: "/cpu/classes/user:cpu-seconds"},
		{Name: "/cpu/classes/total:cpu-seconds"},
	}

	var lastUserCPU, lastTotalCPU float64
	var lastTime time.Time

	// Get initial values
	metrics.Read(samples)
	lastUserCPU = samples[0].Value.Float64()
	lastTotalCPU = samples[1].Value.Float64()
	lastTime = time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Updade utilization only if some requests were send
			o.mu.Lock()
			numReq := o.requestCount
			o.requestCount = 0
			o.mu.Unlock()
			if numReq == 0 {
				continue
			}

			// Read current CPU metrics
			metrics.Read(samples)
			userCPU := samples[0].Value.Float64()
			totalCPU := samples[1].Value.Float64()

			userCPUDelta := userCPU - lastUserCPU
			totalCPUDelta := totalCPU - lastTotalCPU
			lastUserCPU = userCPU
			lastTotalCPU = totalCPU

			intervalDuration := time.Since(lastTime).Seconds()
			lastTime = time.Now()

			qps := float64(numReq) / intervalDuration

			var cpuUtilization float64
			if totalCPUDelta > 0 {
				cpuUtilization = userCPUDelta / totalCPUDelta
			}

			o.serverMetrics.SetCPUUtilization(cpuUtilization)
			o.serverMetrics.SetQPS(qps)
		}
	}
}

func (o *ORCAReporter) RecordRequest() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.requestCount++
}

func (o *ORCAReporter) Stop() {
	if o.cancel != nil {
		o.cancel()
	}
}

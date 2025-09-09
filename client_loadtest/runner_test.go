package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewRunner tests the Runner constructor with various parameters
func TestNewRunner(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *RunnerConfig
		job         func(context.Context) error
		maxInFlight int
		wantErr     bool
	}{
		{
			name: "valid parameters",
			cfg: &RunnerConfig{
				Inflight: 5,
				Mode:     "asap",
				Rps:      0,
				Timeout:  time.Second,
			},
			job:         func(ctx context.Context) error { return nil },
			maxInFlight: 10,
			wantErr:     false,
		},
		{
			name:        "nil config",
			cfg:         nil,
			job:         func(ctx context.Context) error { return nil },
			maxInFlight: 10,
			wantErr:     true,
		},
		{
			name: "nil job",
			cfg: &RunnerConfig{
				Inflight: 5,
				Mode:     "asap",
				Rps:      0,
				Timeout:  time.Second,
			},
			job:         nil,
			maxInFlight: 10,
			wantErr:     true,
		},
		{
			name: "zero maxInFlight",
			cfg: &RunnerConfig{
				Inflight: 5,
				Mode:     "asap",
				Rps:      0,
				Timeout:  time.Second,
			},
			job:         func(ctx context.Context) error { return nil },
			maxInFlight: 0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, err := NewRunner(tt.cfg, tt.job, tt.maxInFlight)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewRunner() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("NewRunner() unexpected error: %v", err)
				return
			}
			if runner == nil {
				t.Error("NewRunner() returned nil runner")
			}
		})
	}
}

// TestGetConfig tests the GetConfig method
func TestGetConfig(t *testing.T) {
	originalCfg := &RunnerConfig{
		Inflight: 5,
		Mode:     "stable",
		Rps:      10.0,
		Timeout:  2 * time.Second,
	}

	runner, err := NewRunner(originalCfg, func(ctx context.Context) error { return nil }, 10)
	if err != nil {
		t.Fatalf("NewRunner() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Start runner in background to handle readCfgChan
	go func() {
		runner.Run(ctx)
	}()

	// Wait a bit for runner to start
	time.Sleep(10 * time.Millisecond)

	cfg, err := runner.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig() failed: %v", err)
	}

	// Test that values are correct
	if cfg.Inflight != originalCfg.Inflight ||
		cfg.Mode != originalCfg.Mode ||
		cfg.Rps != originalCfg.Rps ||
		cfg.Timeout != originalCfg.Timeout {
		t.Error("GetConfig() returned incorrect values")
	}

	// Test that it returns a copy, not the original
	if cfg == originalCfg {
		t.Error("GetConfig() returned the original config instead of a copy")
	}
}

// TestASAPMode tests the "asap" execution mode
func TestASAPMode(t *testing.T) {
	var counter int64

	job := func(ctx context.Context) error {
		atomic.AddInt64(&counter, 1)
		time.Sleep(10 * time.Millisecond) // Simulate work
		return nil
	}

	runner, err := NewRunner(&RunnerConfig{
		Inflight: 3,
		Mode:     "asap",
		Rps:      0,
		Timeout:  time.Second,
	}, job, 5)
	if err != nil {
		t.Fatalf("NewRunner() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = runner.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() failed: %v", err)
	}

	// Should have executed multiple times
	finalCount := atomic.LoadInt64(&counter)
	if finalCount == 0 {
		t.Error("No jobs executed in ASAP mode")
	}

	t.Logf("Executed %d jobs in ASAP mode", finalCount)
}

// TestStableMode tests the "stable" execution mode with fixed intervals
func TestStableMode(t *testing.T) {
	var counter int64

	job := func(ctx context.Context) error {
		atomic.AddInt64(&counter, 1)
		return nil
	}

	// Set up for 5 RPS (200ms intervals)
	runner, err := NewRunner(&RunnerConfig{
		Inflight: 1,
		Mode:     "stable",
		Rps:      5.0,
		Timeout:  time.Second,
	}, job, 5)
	if err != nil {
		t.Fatalf("NewRunner() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = runner.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() failed: %v", err)
	}

	finalCount := atomic.LoadInt64(&counter)
	duration := time.Since(start)

	// Should execute roughly 2-3 times in 450ms at 5 RPS
	if finalCount < 1 || finalCount > 4 {
		t.Errorf("Expected 1-4 executions, got %d in %v", finalCount, duration)
	}

	t.Logf("Executed %d jobs in stable mode over %v", finalCount, duration)
}

// TestInflightLimiting tests that the in-flight limiting works correctly
func TestInflightLimiting(t *testing.T) {
	var activeJobs int64
	var maxActive int64
	var totalJobs int64

	job := func(ctx context.Context) error {
		current := atomic.AddInt64(&activeJobs, 1)
		defer atomic.AddInt64(&activeJobs, -1)

		// Update max if needed
		for {
			max := atomic.LoadInt64(&maxActive)
			if current <= max || atomic.CompareAndSwapInt64(&maxActive, max, current) {
				break
			}
		}

		atomic.AddInt64(&totalJobs, 1)
		time.Sleep(50 * time.Millisecond) // Simulate work
		return nil
	}

	runner, err := NewRunner(&RunnerConfig{
		Inflight: 3,
		Mode:     "asap",
		Rps:      0,
		Timeout:  time.Second,
	}, job, 10)
	if err != nil {
		t.Fatalf("NewRunner() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = runner.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() failed: %v", err)
	}

	maxActiveJobs := atomic.LoadInt64(&maxActive)
	totalExecuted := atomic.LoadInt64(&totalJobs)

	if maxActiveJobs > 3 {
		t.Errorf("Max active jobs exceeded limit: got %d, want ≤ 3", maxActiveJobs)
	}

	if totalExecuted == 0 {
		t.Error("No jobs executed")
	}

	t.Logf("Max concurrent jobs: %d, total executed: %d", maxActiveJobs, totalExecuted)
}

// TestDynamicConfigUpdate tests updating configuration during execution
func TestDynamicConfigUpdate(t *testing.T) {
	var counter int64

	job := func(ctx context.Context) error {
		atomic.AddInt64(&counter, 1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	runner, err := NewRunner(&RunnerConfig{
		Inflight: 1,
		Mode:     "stable",
		Rps:      5.0,
		Timeout:  time.Second,
	}, job, 10)
	if err != nil {
		t.Fatalf("NewRunner() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	// Start runner in background
	go func() {
		err := runner.Run(ctx)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Run() failed: %v", err)
		}
	}()

	// Wait a bit, then update config to faster rate
	time.Sleep(50 * time.Millisecond)
	newCfg := &RunnerConfig{
		Inflight: 2,
		Mode:     "asap",
		Rps:      0,
		Timeout:  time.Second,
	}

	err = runner.SetConfig(ctx, *newCfg)
	if err != nil {
		t.Errorf("SetConfig() failed: %v", err)
	}

	// Wait for context to finish
	<-ctx.Done()

	finalCount := atomic.LoadInt64(&counter)
	if finalCount == 0 {
		t.Error("No jobs executed")
	}

	t.Logf("Executed %d jobs with dynamic config update", finalCount)
}

// TestExponentialMode tests the "exponential" execution mode
func TestExponentialMode(t *testing.T) {
	var counter int64

	job := func(ctx context.Context) error {
		atomic.AddInt64(&counter, 1)
		return nil
	}

	runner, err := NewRunner(&RunnerConfig{
		Inflight: 1,
		Mode:     "exponential",
		Rps:      10.0, // Higher RPS to see some activity
		Timeout:  time.Second,
	}, job, 5)
	if err != nil {
		t.Fatalf("NewRunner() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = runner.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() failed: %v", err)
	}

	finalCount := atomic.LoadInt64(&counter)
	if finalCount == 0 {
		t.Error("No jobs executed in exponential mode")
	}

	t.Logf("Executed %d jobs in exponential mode", finalCount)
}

// TestJobTimeout tests that job timeouts are handled correctly
func TestJobTimeout(t *testing.T) {
	var counter int64

	job := func(ctx context.Context) error {
		atomic.AddInt64(&counter, 1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return nil
		}
	}

	runner, err := NewRunner(&RunnerConfig{
		Inflight: 1,
		Mode:     "asap",
		Rps:      0,
		Timeout:  50 * time.Millisecond, // Short timeout
	}, job, 5)
	if err != nil {
		t.Fatalf("NewRunner() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = runner.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() failed: %v", err)
	}

	finalCount := atomic.LoadInt64(&counter)
	if finalCount == 0 {
		t.Error("No jobs started")
	}

	t.Logf("Started %d jobs with timeouts", finalCount)
}

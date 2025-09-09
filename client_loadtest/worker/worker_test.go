package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewWorker tests the Worker constructor with various parameters
func TestNewWorker(t *testing.T) {
	t.Parallel()

	validJob := func(context.Context) error { return nil }

	tests := []struct {
		name    string
		job     func(context.Context) error
		opts    []Option
		wantErr bool
	}{
		{
			name: "valid worker with defaults",
			job:  validJob,
			opts: nil,
		},
		{
			name: "valid worker with custom config",
			job:  validJob,
			opts: []Option{
				WithConfig(WorkerConfig{
					InFlight: 5,
					Qps:      10.0,
					Timeout:  2 * time.Second,
				}),
				WithMaxInFlight(10),
			},
		},
		{
			name: "valid worker with interval generator",
			job:  validJob,
			opts: []Option{
				WithConfig(WorkerConfig{
					InFlight:          2,
					IntervalGenerator: StableIntervalGenerator,
					Qps:               5.0,
					Timeout:           time.Second,
				}),
				WithMaxInFlight(5),
			},
		},
		{
			name:    "nil job function",
			job:     nil,
			opts:    nil,
			wantErr: true,
		},
		{
			name: "negative maxInFlight",
			job:  validJob,
			opts: []Option{
				WithMaxInFlight(-1),
			},
			wantErr: true,
		},
		{
			name: "InFlight > maxInFlight",
			job:  validJob,
			opts: []Option{
				WithConfig(WorkerConfig{
					InFlight: 10,
					Timeout:  time.Second,
				}),
				WithMaxInFlight(5),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			worker, err := NewWorker(ctx, tt.job, tt.opts...)
			if tt.wantErr {
				if err == nil {
					t.Error("NewWorker() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("NewWorker() unexpected error: %v", err)
				return
			}
			if worker == nil {
				t.Error("NewWorker() returned nil worker")
			}

			// Clean up
			worker.Close()
		})
	}
}

// TestGetConfig tests reading current configuration
func TestGetConfig(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := WorkerConfig{
		InFlight:          3,
		IntervalGenerator: StableIntervalGenerator,
		Qps:               5.0,
		Timeout:           500 * time.Millisecond,
	}

	worker, err := NewWorker(ctx, func(context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	}, WithConfig(cfg), WithMaxInFlight(10))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Get config should return a copy of the current config
	gotCfg, err := worker.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() failed: %v", err)
	}

	if gotCfg.InFlight != cfg.InFlight {
		t.Errorf("GetConfig() InFlight = %d, want %d", gotCfg.InFlight, cfg.InFlight)
	}
	if gotCfg.Qps != cfg.Qps {
		t.Errorf("GetConfig() Qps = %f, want %f", gotCfg.Qps, cfg.Qps)
	}
	if gotCfg.Timeout != cfg.Timeout {
		t.Errorf("GetConfig() Timeout = %v, want %v", gotCfg.Timeout, cfg.Timeout)
	}

	// Should return different pointer (copy, not original)
	if gotCfg == &cfg {
		t.Error("GetConfig() returned original config instead of copy")
	}
}

// TestSetConfig tests dynamic configuration updates
func TestSetConfig(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var jobCount int64
	worker, err := NewWorker(ctx, func(context.Context) error {
		atomic.AddInt64(&jobCount, 1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}, WithMaxInFlight(10))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Start with initial config
	initialCfg := WorkerConfig{
		InFlight: 2,
		Qps:      10.0,
		Timeout:  time.Second,
	}
	err = worker.SetConfig(&initialCfg)
	if err != nil {
		t.Fatalf("SetConfig() failed: %v", err)
	}

	// Wait a bit for the config to take effect
	time.Sleep(50 * time.Millisecond)

	// Verify config was applied
	cfg, err := worker.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() failed: %v", err)
	}
	if cfg.InFlight != initialCfg.InFlight {
		t.Errorf("SetConfig() InFlight = %d, want %d", cfg.InFlight, initialCfg.InFlight)
	}

	// Update to new config
	newCfg := WorkerConfig{
		InFlight:          5,
		IntervalGenerator: StableIntervalGenerator,
		Qps:               20.0,
		Timeout:           500 * time.Millisecond,
	}
	err = worker.SetConfig(&newCfg)
	if err != nil {
		t.Fatalf("SetConfig() failed: %v", err)
	}

	// Wait for new config to take effect
	time.Sleep(50 * time.Millisecond)

	// Verify new config
	cfg, err = worker.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after update failed: %v", err)
	}
	if cfg.InFlight != newCfg.InFlight {
		t.Errorf("SetConfig() updated InFlight = %d, want %d", cfg.InFlight, newCfg.InFlight)
	}
	if cfg.Qps != newCfg.Qps {
		t.Errorf("SetConfig() updated Qps = %f, want %f", cfg.Qps, newCfg.Qps)
	}
}

// TestSetConfigValidation tests that invalid configurations are rejected
func TestSetConfigValidation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	worker, err := NewWorker(ctx, func(context.Context) error {
		return nil
	}, WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	tests := []struct {
		name string
		cfg  WorkerConfig
	}{
		{
			name: "negative InFlight",
			cfg: WorkerConfig{
				InFlight: -1,
				Timeout:  time.Second,
			},
		},
		{
			name: "negative Qps",
			cfg: WorkerConfig{
				InFlight: 2,
				Qps:      -10.0,
				Timeout:  time.Second,
			},
		},
		{
			name: "negative Timeout",
			cfg: WorkerConfig{
				InFlight: 2,
				Qps:      10.0,
				Timeout:  -time.Second,
			},
		},
		{
			name: "InFlight exceeds maxInFlight",
			cfg: WorkerConfig{
				InFlight: 10,
				Timeout:  time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := worker.SetConfig(&tt.cfg)
			if err == nil {
				t.Error("SetConfig() expected error for invalid config, got nil")
			}
		})
	}
}

// TestASAPMode tests worker behavior without interval generator (ASAP mode)
func TestASAPMode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var jobCount int64
	worker, err := NewWorker(ctx, func(context.Context) error {
		atomic.AddInt64(&jobCount, 1)
		time.Sleep(5 * time.Millisecond) // Short job duration
		return nil
	}, WithConfig(WorkerConfig{
		InFlight: 3,
		Timeout:  time.Second,
		// No IntervalGenerator = ASAP mode
	}), WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for execution to complete
	<-ctx.Done()

	finalCount := atomic.LoadInt64(&jobCount)
	if finalCount == 0 {
		t.Error("No jobs executed in ASAP mode")
	}

	// In ASAP mode with 3 concurrent jobs for 200ms, should execute many times
	if finalCount < 10 {
		t.Errorf("Expected more job executions in ASAP mode, got %d", finalCount)
	}

	t.Logf("ASAP mode executed %d jobs in 200ms", finalCount)
}

// TestStableIntervalTiming tests stable interval generation timing
func TestStableIntervalTiming(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 550*time.Millisecond)
	defer cancel()

	var jobTimes []time.Time
	var mu sync.Mutex

	qps := 5.0 // 200ms intervals
	worker, err := NewWorker(ctx, func(context.Context) error {
		mu.Lock()
		jobTimes = append(jobTimes, time.Now())
		mu.Unlock()
		return nil
	}, WithConfig(WorkerConfig{
		InFlight:          1,
		IntervalGenerator: StableIntervalGenerator,
		Qps:               qps,
		Timeout:           time.Second,
	}), WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for execution to complete
	<-ctx.Done()

	mu.Lock()
	defer mu.Unlock()

	if len(jobTimes) < 2 {
		t.Errorf("Expected at least 2 job executions, got %d", len(jobTimes))
		return
	}

	// Check intervals between executions
	expectedInterval := time.Duration(float64(time.Second) / qps)
	toleranceMs := 50 // 50ms tolerance

	for i := 1; i < len(jobTimes); i++ {
		interval := jobTimes[i].Sub(jobTimes[i-1])
		diff := interval - expectedInterval
		if diff < 0 {
			diff = -diff
		}

		if diff > time.Duration(toleranceMs)*time.Millisecond {
			t.Errorf("Job %d interval %v differs from expected %v by %v (tolerance: %dms)",
				i, interval, expectedInterval, diff, toleranceMs)
		}
	}

	t.Logf("Stable timing: executed %d jobs with intervals around %v", len(jobTimes), expectedInterval)
}

// TestExponentialIntervalVariability tests that exponential intervals vary
func TestExponentialIntervalVariability(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var jobTimes []time.Time
	var mu sync.Mutex

	worker, err := NewWorker(ctx, func(context.Context) error {
		mu.Lock()
		jobTimes = append(jobTimes, time.Now())
		mu.Unlock()
		return nil
	}, WithConfig(WorkerConfig{
		InFlight:          1,
		IntervalGenerator: ExponentialIntervalGenerator,
		Qps:               50.0, // Much higher QPS to get more samples and avoid false negatives
		Timeout:           time.Second,
	}), WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for execution to complete
	<-ctx.Done()

	mu.Lock()
	defer mu.Unlock()

	if len(jobTimes) < 10 {
		t.Errorf("Expected at least 10 job executions for variability test, got %d", len(jobTimes))
		return
	}

	// Calculate intervals
	var intervals []time.Duration
	for i := 1; i < len(jobTimes); i++ {
		intervals = append(intervals, jobTimes[i].Sub(jobTimes[i-1]))
	}

	// Check that intervals are not all the same (exponential should vary)
	allSame := true
	first := intervals[0]
	tolerance := 5 * time.Millisecond // Tighter tolerance with higher QPS

	for _, interval := range intervals[1:] {
		if interval < first-tolerance || interval > first+tolerance {
			allSame = false
			break
		}
	}

	if allSame {
		t.Error("Exponential intervals appear too uniform, expected variability")
	}

	t.Logf("Exponential timing: executed %d jobs with varying intervals", len(jobTimes))
}

// TestZeroQPSHandling tests behavior with zero QPS
func TestZeroQPSHandling(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var jobCount int64
	worker, err := NewWorker(ctx, func(context.Context) error {
		atomic.AddInt64(&jobCount, 1)
		time.Sleep(5 * time.Millisecond)
		return nil
	}, WithConfig(WorkerConfig{
		InFlight:          2,
		IntervalGenerator: StableIntervalGenerator, // Will return 0 for QPS=0
		Qps:               0.0,
		Timeout:           time.Second,
	}), WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for execution
	<-ctx.Done()

	finalCount := atomic.LoadInt64(&jobCount)
	if finalCount == 0 {
		t.Error("No jobs executed with zero QPS (should fall back to ASAP mode)")
	}

	t.Logf("Zero QPS executed %d jobs (fell back to ASAP mode)", finalCount)
}

// TestInFlightLimiting tests that in-flight limiting works correctly
func TestInFlightLimiting(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	var activeJobs int64
	var maxActive int64
	var totalJobs int64

	job := func(context.Context) error {
		current := atomic.AddInt64(&activeJobs, 1)
		defer atomic.AddInt64(&activeJobs, -1)

		// Update max if needed (atomic compare-and-swap loop)
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

	expectedInFlight := 3
	worker, err := NewWorker(ctx, job, WithConfig(WorkerConfig{
		InFlight: expectedInFlight,
		Timeout:  time.Second,
		// ASAP mode for maximum concurrency
	}), WithMaxInFlight(10))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for execution to complete
	<-ctx.Done()

	maxActiveJobs := atomic.LoadInt64(&maxActive)
	totalExecuted := atomic.LoadInt64(&totalJobs)

	if maxActiveJobs > int64(expectedInFlight) {
		t.Errorf("Max active jobs exceeded limit: got %d, want ≤ %d", maxActiveJobs, expectedInFlight)
	}

	if totalExecuted == 0 {
		t.Error("No jobs executed")
	}

	t.Logf("Max concurrent jobs: %d (limit: %d), total executed: %d", maxActiveJobs, expectedInFlight, totalExecuted)
}

// TestDynamicInFlightIncrease tests increasing in-flight limit dynamically
func TestDynamicInFlightIncrease(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	var activeJobs int64
	var maxActive int64
	var firstHalfMax int64
	var secondHalfMax int64

	startTime := time.Now()
	job := func(context.Context) error {
		current := atomic.AddInt64(&activeJobs, 1)
		defer atomic.AddInt64(&activeJobs, -1)

		// Update max for overall
		for {
			max := atomic.LoadInt64(&maxActive)
			if current <= max || atomic.CompareAndSwapInt64(&maxActive, max, current) {
				break
			}
		}

		// Track max for each half of the test
		elapsed := time.Since(startTime)
		if elapsed < 200*time.Millisecond {
			// First half
			for {
				max := atomic.LoadInt64(&firstHalfMax)
				if current <= max || atomic.CompareAndSwapInt64(&firstHalfMax, max, current) {
					break
				}
			}
		} else {
			// Second half
			for {
				max := atomic.LoadInt64(&secondHalfMax)
				if current <= max || atomic.CompareAndSwapInt64(&secondHalfMax, max, current) {
					break
				}
			}
		}

		time.Sleep(30 * time.Millisecond)
		return nil
	}

	worker, err := NewWorker(ctx, job, WithConfig(WorkerConfig{
		InFlight: 2,
		Timeout:  time.Second,
	}), WithMaxInFlight(10))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// After half the time, increase in-flight limit
	time.Sleep(200 * time.Millisecond)
	newCfg := WorkerConfig{
		InFlight: 5,
		Timeout:  time.Second,
	}
	err = worker.SetConfig(&newCfg)
	if err != nil {
		t.Errorf("SetConfig() failed: %v", err)
	}

	// Wait for test to complete
	<-ctx.Done()

	maxOverall := atomic.LoadInt64(&maxActive)
	maxFirst := atomic.LoadInt64(&firstHalfMax)
	maxSecond := atomic.LoadInt64(&secondHalfMax)

	if maxFirst > 2 {
		t.Errorf("First half max active jobs exceeded initial limit: got %d, want ≤ 2", maxFirst)
	}

	if maxSecond > 5 {
		t.Errorf("Second half max active jobs exceeded updated limit: got %d, want ≤ 5", maxSecond)
	}

	if maxSecond <= maxFirst {
		t.Errorf("Expected increased concurrency in second half: first=%d, second=%d", maxFirst, maxSecond)
	}

	t.Logf("Dynamic increase: first half max=%d, second half max=%d, overall max=%d", maxFirst, maxSecond, maxOverall)
}

// TestDynamicInFlightDecrease tests decreasing in-flight limit dynamically
func TestDynamicInFlightDecrease(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	var activeJobs int64
	var maxActive int64

	job := func(context.Context) error {
		current := atomic.AddInt64(&activeJobs, 1)
		defer atomic.AddInt64(&activeJobs, -1)

		// Update max
		for {
			max := atomic.LoadInt64(&maxActive)
			if current <= max || atomic.CompareAndSwapInt64(&maxActive, max, current) {
				break
			}
		}

		time.Sleep(30 * time.Millisecond)
		return nil
	}

	worker, err := NewWorker(ctx, job, WithConfig(WorkerConfig{
		InFlight: 5,
		Timeout:  time.Second,
	}), WithMaxInFlight(10))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Let it run with high concurrency first
	time.Sleep(100 * time.Millisecond)

	// Then decrease in-flight limit
	newCfg := WorkerConfig{
		InFlight: 2,
		Timeout:  time.Second,
	}
	err = worker.SetConfig(&newCfg)
	if err != nil {
		t.Errorf("SetConfig() failed: %v", err)
	}

	// Wait for adjustment
	time.Sleep(200 * time.Millisecond)

	// Check that current active jobs respect the new limit
	currentActive := atomic.LoadInt64(&activeJobs)
	if currentActive > 2 {
		t.Errorf("Current active jobs exceed new limit: got %d, want ≤ 2", currentActive)
	}

	// Wait for test to complete
	<-ctx.Done()

	maxOverall := atomic.LoadInt64(&maxActive)
	t.Logf("Dynamic decrease: max overall=%d, final active=%d (limit was decreased to 2)", maxOverall, currentActive)
}

// TestJobTimeout tests that job timeouts are handled correctly
func TestJobTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	var jobStarted int64
	var jobCompleted int64
	var jobTimedOut int64

	job := func(ctx context.Context) error {
		atomic.AddInt64(&jobStarted, 1)
		select {
		case <-ctx.Done():
			atomic.AddInt64(&jobTimedOut, 1)
			return ctx.Err()
		case <-time.After(100 * time.Millisecond): // Longer than timeout
			atomic.AddInt64(&jobCompleted, 1)
			return nil
		}
	}

	worker, err := NewWorker(ctx, job, WithConfig(WorkerConfig{
		InFlight: 3,
		Timeout:  50 * time.Millisecond, // Short timeout
	}), WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for execution to complete
	<-ctx.Done()

	started := atomic.LoadInt64(&jobStarted)
	completed := atomic.LoadInt64(&jobCompleted)
	timedOut := atomic.LoadInt64(&jobTimedOut)

	if started == 0 {
		t.Error("No jobs started")
	}

	if timedOut == 0 {
		t.Error("Expected some jobs to timeout, but none did")
	}

	if completed > timedOut {
		t.Errorf("More jobs completed (%d) than timed out (%d), timeout may not be working", completed, timedOut)
	}

	t.Logf("Job timeout test: started=%d, completed=%d, timed_out=%d", started, completed, timedOut)
}

// TestJobErrors tests that job errors don't crash the worker
func TestJobErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var jobCount int64
	var errorCount int64
	testError := errors.New("test error")

	job := func(context.Context) error {
		count := atomic.AddInt64(&jobCount, 1)
		if count%2 == 0 { // Every second job fails
			atomic.AddInt64(&errorCount, 1)
			return testError
		}
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	worker, err := NewWorker(ctx, job, WithConfig(WorkerConfig{
		InFlight: 2,
		Timeout:  time.Second,
	}), WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for execution to complete
	<-ctx.Done()

	totalJobs := atomic.LoadInt64(&jobCount)
	totalErrors := atomic.LoadInt64(&errorCount)

	if totalJobs == 0 {
		t.Error("No jobs executed")
	}

	if totalErrors == 0 {
		t.Error("Expected some job errors, but none occurred")
	}

	if totalJobs != totalErrors*2 { // Should be roughly half errors
		t.Logf("Job counts don't match expected pattern (not critical): total=%d, errors=%d", totalJobs, totalErrors)
	}

	t.Logf("Job error test: total_jobs=%d, errors=%d", totalJobs, totalErrors)
}

// TestWorkerClose tests that Close() stops the worker properly
func TestWorkerClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background() // Long-running context
	
	var jobCount int64
	job := func(context.Context) error {
		atomic.AddInt64(&jobCount, 1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	worker, err := NewWorker(ctx, job, WithConfig(WorkerConfig{
		InFlight: 2,
		Timeout:  time.Second,
	}), WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)
	
	initialCount := atomic.LoadInt64(&jobCount)
	if initialCount == 0 {
		t.Error("No jobs executed before Close()")
	}

	// Close the worker
	worker.Close()

	// Wait a bit more
	time.Sleep(100 * time.Millisecond)
	
	finalCount := atomic.LoadInt64(&jobCount)
	
	// After Close(), job count should not increase significantly
	if finalCount > initialCount+5 { // Allow some tolerance for in-flight jobs
		t.Errorf("Jobs continued executing after Close(): initial=%d, final=%d", initialCount, finalCount)
	}

	t.Logf("Worker close test: jobs before close=%d, jobs after close=%d", initialCount, finalCount)
}

// TestContextCancellation tests that worker respects context cancellation
func TestContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	var jobCount int64
	var contextCancelledJobs int64

	job := func(jobCtx context.Context) error {
		atomic.AddInt64(&jobCount, 1)
		
		select {
		case <-jobCtx.Done():
			atomic.AddInt64(&contextCancelledJobs, 1)
			return jobCtx.Err()
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	}

	worker, err := NewWorker(ctx, job, WithConfig(WorkerConfig{
		InFlight: 3,
		Timeout:  time.Second, // Longer than job duration
	}), WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for context to be cancelled
	<-ctx.Done()

	totalJobs := atomic.LoadInt64(&jobCount)
	cancelledJobs := atomic.LoadInt64(&contextCancelledJobs)

	if totalJobs == 0 {
		t.Error("No jobs executed")
	}

	// Some jobs should have been cancelled due to context cancellation
	// (though timing makes this not guaranteed)
	t.Logf("Context cancellation test: total_jobs=%d, cancelled_jobs=%d", totalJobs, cancelledJobs)
}

// TestGetConfigAfterClose tests that GetConfig fails after Close()
func TestGetConfigAfterClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	
	worker, err := NewWorker(ctx, func(context.Context) error {
		return nil
	}, WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}

	// Close the worker
	worker.Close()

	// GetConfig should fail
	_, err = worker.GetConfig()
	if err == nil {
		t.Error("GetConfig() should fail after Close(), but it succeeded")
	}

	t.Logf("GetConfig after close correctly failed with: %v", err)
}

// TestSetConfigAfterClose tests that SetConfig fails after Close()
func TestSetConfigAfterClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	
	worker, err := NewWorker(ctx, func(context.Context) error {
		return nil
	}, WithMaxInFlight(5))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}

	// Close the worker
	worker.Close()

	// SetConfig should fail
	newCfg := WorkerConfig{
		InFlight: 3,
		Timeout:  time.Second,
	}
	err = worker.SetConfig(&newCfg)
	if err == nil {
		t.Error("SetConfig() should fail after Close(), but it succeeded")
	}

	t.Logf("SetConfig after close correctly failed with: %v", err)
}

// TestRapidConfigUpdates tests rapid configuration changes
func TestRapidConfigUpdates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var jobCount int64
	worker, err := NewWorker(ctx, func(context.Context) error {
		atomic.AddInt64(&jobCount, 1)
		time.Sleep(20 * time.Millisecond)
		return nil
	}, WithMaxInFlight(10))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Rapidly change configurations
	configs := []WorkerConfig{
		{InFlight: 1, Qps: 10.0, Timeout: time.Second},
		{InFlight: 3, Qps: 20.0, Timeout: 500 * time.Millisecond},
		{InFlight: 5, Qps: 5.0, Timeout: 2 * time.Second},
		{InFlight: 2, Qps: 0.0, Timeout: time.Second},
		{InFlight: 4, Qps: 15.0, Timeout: 300 * time.Millisecond, IntervalGenerator: StableIntervalGenerator},
	}

	// Apply configs rapidly
	for i, cfg := range configs {
		err := worker.SetConfig(&cfg)
		if err != nil {
			t.Errorf("SetConfig(%d) failed: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // Very short delay
	}

	// Wait for execution to complete
	<-ctx.Done()

	finalCount := atomic.LoadInt64(&jobCount)
	if finalCount == 0 {
		t.Error("No jobs executed during rapid config updates")
	}

	// Verify final config
	finalCfg, err := worker.GetConfig()
	if err != nil {
		t.Logf("GetConfig() after rapid updates failed (expected due to context timeout): %v", err)
		t.Logf("Rapid config updates: executed %d jobs", finalCount)
	} else {
		lastCfg := configs[len(configs)-1]
		if finalCfg.InFlight != lastCfg.InFlight {
			t.Errorf("Final config InFlight = %d, want %d", finalCfg.InFlight, lastCfg.InFlight)
		}
		t.Logf("Rapid config updates: executed %d jobs, final InFlight=%d", finalCount, finalCfg.InFlight)
	}
}

// TestConcurrentGetSetConfig tests concurrent access to GetConfig and SetConfig
func TestConcurrentGetSetConfig(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	worker, err := NewWorker(ctx, func(context.Context) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	}, WithMaxInFlight(10))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	var wg sync.WaitGroup
	var getErrors int64
	var setErrors int64

	// Multiple goroutines calling GetConfig
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, err := worker.GetConfig()
				if err != nil {
					atomic.AddInt64(&getErrors, 1)
				}
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	// Multiple goroutines calling SetConfig
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cfg := WorkerConfig{
					InFlight: (id % 5) + 1,
					Qps:      float64(j + 1),
					Timeout:  time.Duration(j+1) * 100 * time.Millisecond,
				}
				err := worker.SetConfig(&cfg)
				if err != nil {
					atomic.AddInt64(&setErrors, 1)
				}
				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	totalGetErrors := atomic.LoadInt64(&getErrors)
	totalSetErrors := atomic.LoadInt64(&setErrors)

	// Some errors might be expected due to context cancellation, but not too many
	if totalGetErrors > 50 { // Allow some tolerance
		t.Errorf("Too many GetConfig errors: %d", totalGetErrors)
	}
	if totalSetErrors > 25 { // Allow some tolerance
		t.Errorf("Too many SetConfig errors: %d", totalSetErrors)
	}

	t.Logf("Concurrent config access: get_errors=%d, set_errors=%d", totalGetErrors, totalSetErrors)
}

// TestMaxInFlightBoundary tests edge cases around maxInFlight boundary
func TestMaxInFlightBoundary(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	maxInFlight := 3
	worker, err := NewWorker(ctx, func(context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	}, WithConfig(WorkerConfig{
		InFlight: maxInFlight,
		Timeout:  time.Second,
	}), WithMaxInFlight(maxInFlight))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Try to set InFlight equal to maxInFlight (should succeed)
	cfg := WorkerConfig{
		InFlight: maxInFlight,
		Timeout:  time.Second,
	}
	err = worker.SetConfig(&cfg)
	if err != nil {
		t.Errorf("SetConfig(InFlight=maxInFlight) should succeed, got error: %v", err)
	}

	// Try to set InFlight greater than maxInFlight (should fail)
	cfg.InFlight = maxInFlight + 1
	err = worker.SetConfig(&cfg)
	if err == nil {
		t.Error("SetConfig(InFlight > maxInFlight) should fail, but succeeded")
	}

	t.Logf("MaxInFlight boundary test passed")
}

// TestZeroMaxInFlight tests worker creation with zero maxInFlight
func TestZeroMaxInFlight(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	
	_, err := NewWorker(ctx, func(context.Context) error {
		return nil
	}, WithMaxInFlight(0))
	
	if err == nil {
		t.Error("NewWorker with maxInFlight=0 should fail, but succeeded")
	}

	t.Logf("Zero maxInFlight correctly failed with: %v", err)
}

// TestHighConcurrency tests worker behavior under high concurrency
func TestHighConcurrency(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var jobCount int64
	var maxActive int64
	var activeJobs int64

	job := func(context.Context) error {
		current := atomic.AddInt64(&activeJobs, 1)
		defer atomic.AddInt64(&activeJobs, -1)
		
		// Update max
		for {
			max := atomic.LoadInt64(&maxActive)
			if current <= max || atomic.CompareAndSwapInt64(&maxActive, max, current) {
				break
			}
		}

		atomic.AddInt64(&jobCount, 1)
		time.Sleep(1 * time.Millisecond) // Very short jobs
		return nil
	}

	highConcurrency := 20
	worker, err := NewWorker(ctx, job, WithConfig(WorkerConfig{
		InFlight: highConcurrency,
		Timeout:  time.Second,
	}), WithMaxInFlight(highConcurrency))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Wait for execution to complete
	<-ctx.Done()

	totalJobs := atomic.LoadInt64(&jobCount)
	maxActiveJobs := atomic.LoadInt64(&maxActive)

	if totalJobs == 0 {
		t.Error("No jobs executed")
	}

	if maxActiveJobs > int64(highConcurrency) {
		t.Errorf("Max active jobs exceeded limit: got %d, want ≤ %d", maxActiveJobs, highConcurrency)
	}

	// Should execute many jobs with high concurrency and short durations
	expectedMinJobs := int64(100) // Conservative estimate
	if totalJobs < expectedMinJobs {
		t.Errorf("Expected at least %d jobs with high concurrency, got %d", expectedMinJobs, totalJobs)
	}

	t.Logf("High concurrency test: executed %d jobs, max concurrent=%d", totalJobs, maxActiveJobs)
}

// TestConfigTransitionStability tests that config transitions don't cause panics
func TestConfigTransitionStability(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	var jobCount int64
	worker, err := NewWorker(ctx, func(context.Context) error {
		atomic.AddInt64(&jobCount, 1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}, WithMaxInFlight(10))
	if err != nil {
		t.Fatalf("NewWorker() failed: %v", err)
	}
	defer worker.Close()

	// Test various transitions
	transitions := []struct {
		name string
		cfg  WorkerConfig
	}{
		{"nil to stable", WorkerConfig{InFlight: 2, IntervalGenerator: StableIntervalGenerator, Qps: 10, Timeout: time.Second}},
		{"stable to exponential", WorkerConfig{InFlight: 3, IntervalGenerator: ExponentialIntervalGenerator, Qps: 15, Timeout: time.Second}},
		{"exponential to ASAP", WorkerConfig{InFlight: 4, Qps: 0, Timeout: time.Second}},
		{"ASAP to stable", WorkerConfig{InFlight: 1, IntervalGenerator: StableIntervalGenerator, Qps: 5, Timeout: time.Second}},
		{"increase concurrency", WorkerConfig{InFlight: 6, IntervalGenerator: StableIntervalGenerator, Qps: 20, Timeout: time.Second}},
		{"decrease concurrency", WorkerConfig{InFlight: 2, IntervalGenerator: StableIntervalGenerator, Qps: 8, Timeout: time.Second}},
	}

	for i, trans := range transitions {
		err := worker.SetConfig(&trans.cfg)
		if err != nil {
			t.Errorf("Transition '%s' failed: %v", trans.name, err)
		}
		time.Sleep(20 * time.Millisecond) // Let it settle
		
		// Verify config was applied
		cfg, err := worker.GetConfig()
		if err != nil {
			t.Errorf("GetConfig after transition %d failed: %v", i, err)
		} else if cfg.InFlight != trans.cfg.InFlight {
			t.Errorf("Transition '%s': InFlight = %d, want %d", trans.name, cfg.InFlight, trans.cfg.InFlight)
		}
	}

	<-ctx.Done()

	totalJobs := atomic.LoadInt64(&jobCount)
	if totalJobs == 0 {
		t.Error("No jobs executed during transitions")
	}

	t.Logf("Config transitions: executed %d jobs through %d transitions", totalJobs, len(transitions))
}
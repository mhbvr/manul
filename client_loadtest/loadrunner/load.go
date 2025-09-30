package loadrunner

import (
	"context"
	"time"
)

// Load defines the interface for load testing operations.
// Implementations provide initialization logic and job execution logic.
type Load interface {
	// Init initializes the load testing environment.
	// This is called once before starting workers.
	// It should prepare any necessary resources or fetch initial data.
	Init(ctx context.Context) error

	// Job executes a single load testing operation.
	// This is called repeatedly by workers during load testing.
	// Returns the duration of the operation and any error that occurred.
	Job(ctx context.Context) (time.Duration, error)

	// Close cleans up resources used by the load testing implementation.
	Close() error
}

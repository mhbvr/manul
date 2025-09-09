package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/zpages"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// InitializeTracing sets up OpenTelemetry tracing with zpages
func InitializeTracing() (http.Handler, func(), error) {
	// Create resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("client-loadtest"),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource: %v", err)
	}

	// Create zpages span processor
	zpagesProcessor := zpages.NewSpanProcessor()

	// Create trace provider with zpages span processor
	tp := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSpanProcessor(zpagesProcessor), // This enables zpages functionality
		trace.WithSampler(trace.AlwaysSample()),  // Sample all traces for load testing
	)

	// Set global trace provider
	otel.SetTracerProvider(tp)

	// Return cleanup function
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tp.Shutdown(ctx)
	}

	return zpages.NewTracezHandler(zpagesProcessor), cleanup, nil
}

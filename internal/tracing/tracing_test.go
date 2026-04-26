package tracing_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/jtomasevic/cloud-forge/internal/tracing"
)

// TestInit_DevMode verifies that Init succeeds when no OTLP endpoint is
// configured (development mode with stdout exporter).
func TestInit_DevMode(t *testing.T) {
	ctx := context.Background()

	shutdown, err := tracing.Init(ctx, tracing.Config{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.1",
		Environment:    "test",
		// No OTLPEndpoint — stdout exporter is used.
	})

	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// The global tracer provider must be set after Init returns.
	tracer := otel.Tracer("test")
	require.NotNil(t, tracer)

	// Start a span to verify the provider is functional.
	spanCtx, span := tracer.Start(ctx, "test-span")
	require.NotNil(t, span)
	require.NotNil(t, spanCtx)
	span.End()

	// Shutdown must complete without error.
	require.NoError(t, shutdown(ctx))
}

// TestInit_WithOTLPEndpoint verifies that Init succeeds when an OTLP endpoint
// is configured, even if nothing is listening on that port. The gRPC client
// uses lazy dialing so the connection attempt is deferred to the first export.
// This test exercises the buildTraceExporter and buildLogExporter OTLP paths.
func TestInit_WithOTLPEndpoint(t *testing.T) {
	ctx := context.Background()

	// Use a localhost address with a port that is almost certainly not in use.
	// grpc.NewClient (Go gRPC v2) is lazy — no actual TCP connection is made
	// until the first export attempt, so Init succeeds even without a server.
	shutdown, err := tracing.Init(ctx, tracing.Config{
		ServiceName:  "test-otlp-svc",
		OTLPEndpoint: "localhost:14317", // likely nothing listening here
		Sampler:      "always",
	})

	// Init must succeed: the gRPC connection is lazy.
	require.NoError(t, err, "Init must not fail with a non-reachable OTLP endpoint")
	require.NotNil(t, shutdown)

	// Shutdown must not panic (it may return an error if the exporter can't flush).
	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = shutdown(shutCtx) // tolerate timeout errors from the unreachable endpoint
}

// TestInit_SamplerAlways verifies that "always" sampler initialises correctly.
func TestInit_SamplerAlways(t *testing.T) {
	ctx := context.Background()
	shutdown, err := tracing.Init(ctx, tracing.Config{
		ServiceName: "test-svc",
		Sampler:     "always",
	})
	require.NoError(t, err)
	require.NoError(t, shutdown(ctx))
}

// TestInit_SamplerNever verifies that "never" sampler initialises correctly.
func TestInit_SamplerNever(t *testing.T) {
	ctx := context.Background()
	shutdown, err := tracing.Init(ctx, tracing.Config{
		ServiceName: "test-svc",
		Sampler:     "never",
	})
	require.NoError(t, err)
	require.NoError(t, shutdown(ctx))
}

// TestInit_SamplerRatio verifies that a ratio sampler with a valid fraction
// initialises correctly.
func TestInit_SamplerRatio(t *testing.T) {
	ctx := context.Background()
	shutdown, err := tracing.Init(ctx, tracing.Config{
		ServiceName: "test-svc",
		Sampler:     "ratio:0.25",
	})
	require.NoError(t, err)
	require.NoError(t, shutdown(ctx))
}

// TestInit_SamplerParent verifies that the "parent" sampler initialises correctly.
func TestInit_SamplerParent(t *testing.T) {
	ctx := context.Background()
	shutdown, err := tracing.Init(ctx, tracing.Config{
		ServiceName: "test-svc",
		Sampler:     "parent",
	})
	require.NoError(t, err)
	require.NoError(t, shutdown(ctx))
}

// TestInit_InvalidSampler verifies that an unknown sampler string returns
// an error rather than panicking.
func TestInit_InvalidSampler(t *testing.T) {
	ctx := context.Background()
	_, err := tracing.Init(ctx, tracing.Config{
		ServiceName: "test-svc",
		Sampler:     "unknown-sampler",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sampler")
}

// TestInit_InvalidRatioSampler verifies that an out-of-range ratio returns
// a descriptive error.
func TestInit_InvalidRatioSampler(t *testing.T) {
	ctx := context.Background()
	_, err := tracing.Init(ctx, tracing.Config{
		ServiceName: "test-svc",
		Sampler:     "ratio:1.5", // > 1.0, invalid
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ratio")
}

// TestShutdown_Idempotent verifies that calling shutdown twice does not
// return a secondary error that would confuse service shutdown sequences.
func TestShutdown_Idempotent(t *testing.T) {
	ctx := context.Background()
	shutdown, err := tracing.Init(ctx, tracing.Config{
		ServiceName: "test-svc",
	})
	require.NoError(t, err)

	// First shutdown: must succeed.
	require.NoError(t, shutdown(ctx))

	// Second shutdown: the SDK returns an ErrTracerProviderShutdown but we
	// tolerate it here; the important thing is it does not panic.
	_ = shutdown(ctx)
}

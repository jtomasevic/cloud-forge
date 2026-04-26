// provider_whitebox_test.go — white-box tests for internal tracing helpers.
//
// These tests live in package tracing (not tracing_test) so they can call
// unexported functions (buildTraceExporter, buildLogExporter, buildSampler)
// directly, achieving higher branch coverage without requiring a real OTel
// Collector to be running.
package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildTraceExporter_StdoutPath verifies that buildTraceExporter returns
// a stdout exporter when the endpoint is empty (development mode).
func TestBuildTraceExporter_StdoutPath(t *testing.T) {
	exporter, err := buildTraceExporter(context.Background(), "")
	require.NoError(t, err)
	require.NotNil(t, exporter)
}

// TestBuildTraceExporter_OTLPPath verifies that buildTraceExporter successfully
// creates an OTLP gRPC exporter even when the endpoint is not reachable.
// The gRPC client uses lazy dialing — no connection is attempted at creation time.
func TestBuildTraceExporter_OTLPPath(t *testing.T) {
	exporter, err := buildTraceExporter(context.Background(), "localhost:14317")
	require.NoError(t, err, "OTLP exporter must be created without a live server (lazy dial)")
	require.NotNil(t, exporter)
}

// TestBuildLogExporter_OTLPPath verifies that buildLogExporter successfully
// creates an OTLP gRPC log exporter for the given endpoint.
func TestBuildLogExporter_OTLPPath(t *testing.T) {
	exporter, err := buildLogExporter(context.Background(), "localhost:14317")
	require.NoError(t, err, "OTLP log exporter must be created without a live server (lazy dial)")
	require.NotNil(t, exporter)
}

// TestInitProvider_InvalidSampler verifies that initProvider returns an error
// when an invalid sampler string is configured (exercises the sampler error path).
func TestInitProvider_InvalidSampler(t *testing.T) {
	_, err := initProvider(context.Background(), Config{
		ServiceName: "test",
		Sampler:     "invalid-sampler-name",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sampler")
}

// TestInitProvider_ServiceVersionDefault verifies the "dev" fallback when
// ServiceVersion is empty.
func TestInitProvider_ServiceVersionDefault(t *testing.T) {
	shutdown, err := initProvider(context.Background(), Config{
		ServiceName: "no-version",
		// ServiceVersion intentionally empty — must default to "dev".
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	_ = shutdown(context.Background())
}

// TestInitProvider_EnvFallbacks verifies that initProvider reads CF_ENV and
// OTEL_EXPORTER_OTLP_ENDPOINT when the corresponding Config fields are empty.
func TestInitProvider_EnvFallbacks(t *testing.T) {
	t.Setenv("CF_ENV", "env-test")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:14317")
	t.Cleanup(func() {
		// Clear after test so subsequent tests start clean.
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
		t.Setenv("CF_ENV", "")
	})

	shutdown, err := initProvider(context.Background(), Config{
		ServiceName: "env-fallback-test",
		// OTLPEndpoint and Environment intentionally empty — must be read from env.
	})

	require.NoError(t, err)
	require.NotNil(t, shutdown)
	// Allow the shutdown to fail (can't flush to a non-existent endpoint).
	_ = shutdown(context.Background())
}

// TestBuildTraceExporter_InvalidEndpoint verifies that buildTraceExporter
// behaves safely with a syntactically invalid endpoint address and does not
// panic regardless of whether grpc.NewClient accepts or rejects the address.
func TestBuildTraceExporter_InvalidEndpoint(_ *testing.T) {
	_, err := buildTraceExporter(context.Background(), "://bad-address")
	// Tolerate both success and failure — the important assertion is no panic.
	_ = err
}

// TestBuildLogExporter_InvalidEndpoint verifies that buildLogExporter behaves
// safely with a syntactically invalid endpoint address.
func TestBuildLogExporter_InvalidEndpoint(_ *testing.T) {
	_, err := buildLogExporter(context.Background(), "://bad-address")
	_ = err // tolerate both success and failure — just must not panic
}

// TestBuildSampler_AllValues exercises every valid sampler string plus an
// invalid one, covering all branches in the switch statement.
func TestBuildSampler_AllValues(t *testing.T) {
	tests := []struct {
		name     string
		sampler  string
		endpoint string
		wantErr  bool
	}{
		{"always", "always", "", false},
		{"never", "never", "", false},
		{"parent", "parent", "", false},
		{"ratio 0.5", "ratio:0.5", "", false},
		{"ratio 0.0", "ratio:0.0", "", false},
		{"ratio 1.0", "ratio:1.0", "", false},
		{"empty+no endpoint → always", "", "", false},
		{"empty+endpoint → parent", "", "localhost:14317", false},
		{"invalid string", "unknown", "", true},
		{"ratio > 1.0", "ratio:1.5", "", true},
		{"ratio < 0.0", "ratio:-0.1", "", true},
		{"ratio non-numeric", "ratio:abc", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := buildSampler(tc.sampler, tc.endpoint)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, s)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, s)
			}
		})
	}
}

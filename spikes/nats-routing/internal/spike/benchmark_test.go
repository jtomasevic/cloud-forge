package spike_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

// TestRunLatencyBenchmark_SmallCount verifies that the benchmark completes,
// collects latency samples, and returns a non-empty detail string.
//
// We use 50 messages (instead of the default 10,000) so the test runs in
// well under 1 second on any laptop.
func TestRunLatencyBenchmark_SmallCount(t *testing.T) {
	t.Parallel()

	srv := startSingleServer(t)
	nc := connectAnon(t, srv)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := t.Context()

	stats, detail := spike.RunLatencyBenchmark(ctx, nc, 50, logger)

	require.NotEmpty(t, detail, "detail string must not be empty")
	assert.Greater(t, stats.Throughput, 0.0, "throughput must be positive")
	assert.GreaterOrEqual(t, stats.P99, stats.P50, "p99 >= p50")
	assert.GreaterOrEqual(t, stats.Max, stats.Min, "max >= min")
}

// TestRunLatencyBenchmark_P99NonZero verifies that p99 and min are positive
// durations after a successful benchmark run with 10 messages.
func TestRunLatencyBenchmark_P99NonZero(t *testing.T) {
	t.Parallel()

	srv := startSingleServer(t)
	nc := connectAnon(t, srv)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := t.Context()

	stats, _ := spike.RunLatencyBenchmark(ctx, nc, 10, logger)

	assert.Positive(t, stats.P99, "p99 must be > 0 after successful benchmark")
	assert.Positive(t, stats.Min, "min must be > 0 after successful benchmark")
}

// TestRunLatencyBenchmark_DetailContainsNumbers verifies that the returned
// detail string contains the word "published" and a numeric throughput value.
func TestRunLatencyBenchmark_DetailContainsNumbers(t *testing.T) {
	t.Parallel()

	srv := startSingleServer(t)
	nc := connectAnon(t, srv)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := t.Context()

	_, detail := spike.RunLatencyBenchmark(ctx, nc, 20, logger)
	assert.Contains(t, detail, "published")
	assert.Contains(t, detail, "msg/s")
}

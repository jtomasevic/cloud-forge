package spike_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

// TestComputePercentiles_Typical verifies p50/p95/p99 with a known sorted set.
// Using 100 evenly-spaced values (1ms…100ms) makes the expected percentiles
// easy to calculate by hand.
func TestComputePercentiles_Typical(t *testing.T) {
	t.Parallel()

	latencies := make([]time.Duration, 100)
	for i := range latencies {
		// 1ms, 2ms, … 100ms
		latencies[i] = time.Duration(i+1) * time.Millisecond
	}
	elapsed := 100 * time.Millisecond

	stats := spike.ComputePercentiles(latencies, elapsed)

	// ceil(50/100 * 100) - 1 = 50 - 1 = 49  → index 49 → 50ms
	assert.Equal(t, 50*time.Millisecond, stats.P50)
	// ceil(95/100 * 100) - 1 = 95 - 1 = 94  → index 94 → 95ms
	assert.Equal(t, 95*time.Millisecond, stats.P95)
	// ceil(99/100 * 100) - 1 = 99 - 1 = 98  → index 98 → 99ms
	assert.Equal(t, 99*time.Millisecond, stats.P99)
	assert.Equal(t, 1*time.Millisecond, stats.Min)
	assert.Equal(t, 100*time.Millisecond, stats.Max)
	// Throughput = 100 messages / 0.1s = 1000 msg/s
	assert.InDelta(t, 1000.0, stats.Throughput, 1.0)
}

// TestComputePercentiles_SingleElement verifies that a single-element slice is
// handled without panic and that all percentiles equal the one value.
func TestComputePercentiles_SingleElement(t *testing.T) {
	t.Parallel()

	latencies := []time.Duration{5 * time.Millisecond}
	stats := spike.ComputePercentiles(latencies, time.Second)

	assert.Equal(t, 5*time.Millisecond, stats.P50)
	assert.Equal(t, 5*time.Millisecond, stats.P95)
	assert.Equal(t, 5*time.Millisecond, stats.P99)
	assert.Equal(t, 5*time.Millisecond, stats.Min)
	assert.Equal(t, 5*time.Millisecond, stats.Max)
}

// TestComputePercentiles_TwoElements verifies boundary behaviour with only
// two latency values.
func TestComputePercentiles_TwoElements(t *testing.T) {
	t.Parallel()

	latencies := []time.Duration{1 * time.Millisecond, 9 * time.Millisecond}
	stats := spike.ComputePercentiles(latencies, time.Second)

	assert.Equal(t, 1*time.Millisecond, stats.Min)
	assert.Equal(t, 9*time.Millisecond, stats.Max)
	// With n=2: p50 → ceil(1.0)-1 = 0 → 1ms; p99 → ceil(1.98)-1 = 1 → 9ms
	assert.Equal(t, 1*time.Millisecond, stats.P50)
	assert.Equal(t, 9*time.Millisecond, stats.P99)
}

// TestComputePercentiles_UnsortedInput verifies that the function sorts the
// input in-place and still produces correct percentiles.
func TestComputePercentiles_UnsortedInput(t *testing.T) {
	t.Parallel()

	// Deliberately unsorted.
	latencies := []time.Duration{
		9 * time.Millisecond,
		1 * time.Millisecond,
		5 * time.Millisecond,
	}
	stats := spike.ComputePercentiles(latencies, time.Second)

	assert.Equal(t, 1*time.Millisecond, stats.Min)
	assert.Equal(t, 9*time.Millisecond, stats.Max)
}

// TestComputePercentiles_OddCount verifies correct behaviour with an odd
// number of elements (3), where index arithmetic can hit off-by-one errors.
func TestComputePercentiles_OddCount(t *testing.T) {
	t.Parallel()

	latencies := []time.Duration{
		1 * time.Millisecond,
		5 * time.Millisecond,
		9 * time.Millisecond,
	}
	stats := spike.ComputePercentiles(latencies, 3*time.Second)

	assert.Equal(t, 1*time.Millisecond, stats.Min)
	assert.Equal(t, 9*time.Millisecond, stats.Max)
	// p50 with n=3: ceil(1.5)-1 = 1 → 5ms
	assert.Equal(t, 5*time.Millisecond, stats.P50)
}

func TestComputePercentiles_Throughput(t *testing.T) {
	t.Parallel()

	latencies := make([]time.Duration, 500)
	for i := range latencies {
		latencies[i] = time.Millisecond
	}
	elapsed := 500 * time.Millisecond // 500 messages in 0.5s = 1000 msg/s

	stats := spike.ComputePercentiles(latencies, elapsed)

	assert.InDelta(t, 1000.0, stats.Throughput, 1.0)
}

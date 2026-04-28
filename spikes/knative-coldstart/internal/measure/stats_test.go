package measure

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── ComputeStats ─────────────────────────────────────────────────────────────

// TestComputeStats_AllSuccess verifies percentile computation over a clean
// sample set with no failures.
func TestComputeStats_AllSuccess(t *testing.T) {
	// Build 10 samples with durations 100ms, 200ms … 1000ms.
	// With these values:
	//   sorted: [100,200,300,400,500,600,700,800,900,1000] ms
	//   nearest-rank p50  → idx ceil(0.50×10)−1 = 4 → 500ms
	//   nearest-rank p75  → idx ceil(0.75×10)−1 = 7 → 800ms
	//   nearest-rank p95  → idx ceil(0.95×10)−1 = 9 → 1000ms
	//   nearest-rank p99  → idx ceil(0.99×10)−1 = 9 → 1000ms
	samples := make([]Sample, 10)
	for i := range samples {
		samples[i] = Sample{TTFB: time.Duration(i+1) * 100 * time.Millisecond}
	}

	stats := ComputeStats(VariantMinimal, samples)

	assert.Equal(t, VariantMinimal, stats.Variant)
	assert.Equal(t, ImageSizes[VariantMinimal], stats.ImageSize)
	assert.Equal(t, 10, stats.SampleCount)
	assert.Equal(t, 0, stats.FailCount)
	assert.Equal(t, 500*time.Millisecond, stats.P50)
	assert.Equal(t, 800*time.Millisecond, stats.P75)
	assert.Equal(t, 1000*time.Millisecond, stats.P95)
	assert.Equal(t, 1000*time.Millisecond, stats.P99)
	assert.Equal(t, 100*time.Millisecond, stats.Min)
	assert.Equal(t, 1000*time.Millisecond, stats.Max)
}

// TestComputeStats_AllFailed verifies that a fully failed sample set produces
// zero duration values and the correct fail count.
func TestComputeStats_AllFailed(t *testing.T) {
	samples := []Sample{
		{Error: errors.New("timeout")},
		{Error: errors.New("network error")},
		{Error: errors.New("503")},
	}

	stats := ComputeStats(VariantMedium, samples)

	assert.Equal(t, 3, stats.SampleCount)
	assert.Equal(t, 3, stats.FailCount)
	// All durations must be zero — no successful samples to compute from.
	assert.Equal(t, time.Duration(0), stats.P50)
	assert.Equal(t, time.Duration(0), stats.P95)
	assert.Equal(t, time.Duration(0), stats.Min)
	assert.Equal(t, time.Duration(0), stats.Max)
}

// TestComputeStats_MixedSamples verifies that failed samples are excluded from
// percentile computation while still being counted in FailCount.
func TestComputeStats_MixedSamples(t *testing.T) {
	// 2 failures + 3 successes (200ms, 400ms, 600ms).
	// With 3 successful durations sorted [200, 400, 600]:
	//   p50 → idx ceil(0.50×3)−1 = 1 → 400ms
	//   p95 → idx ceil(0.95×3)−1 = 2 → 600ms
	samples := []Sample{
		{Error: errors.New("timeout")},
		{TTFB: 200 * time.Millisecond},
		{Error: errors.New("network error")},
		{TTFB: 400 * time.Millisecond},
		{TTFB: 600 * time.Millisecond},
	}

	stats := ComputeStats(VariantHeavy, samples)

	assert.Equal(t, 5, stats.SampleCount)
	assert.Equal(t, 2, stats.FailCount)
	assert.Equal(t, 400*time.Millisecond, stats.P50)
	assert.Equal(t, 600*time.Millisecond, stats.P95)
	assert.Equal(t, 200*time.Millisecond, stats.Min)
	assert.Equal(t, 600*time.Millisecond, stats.Max)
}

// TestComputeStats_SingleSample verifies behaviour at the minimum-size edge case.
func TestComputeStats_SingleSample(t *testing.T) {
	samples := []Sample{
		{TTFB: 1200 * time.Millisecond},
	}

	stats := ComputeStats(VariantMinimal, samples)

	// With a single sample all percentiles must equal that one value.
	assert.Equal(t, 1200*time.Millisecond, stats.P50)
	assert.Equal(t, 1200*time.Millisecond, stats.P75)
	assert.Equal(t, 1200*time.Millisecond, stats.P95)
	assert.Equal(t, 1200*time.Millisecond, stats.P99)
	assert.Equal(t, 1200*time.Millisecond, stats.Min)
	assert.Equal(t, 1200*time.Millisecond, stats.Max)
}

// TestComputeStats_EmptySamples verifies that an empty slice produces a zeroed Stats.
func TestComputeStats_EmptySamples(t *testing.T) {
	stats := ComputeStats(VariantMinimal, nil)
	assert.Equal(t, 0, stats.SampleCount)
	assert.Equal(t, time.Duration(0), stats.P95)
}

// ─── percentileAt ─────────────────────────────────────────────────────────────

// TestPercentileAt_EdgeCases verifies boundary conditions for the nearest-rank formula.
func TestPercentileAt_EdgeCases(t *testing.T) {
	// An empty slice must return 0 without panicking.
	assert.Equal(t, time.Duration(0), percentileAt(nil, 50))

	// A single-element slice must return that element for any percentile.
	single := []time.Duration{500 * time.Millisecond}
	assert.Equal(t, 500*time.Millisecond, percentileAt(single, 0))
	assert.Equal(t, 500*time.Millisecond, percentileAt(single, 50))
	assert.Equal(t, 500*time.Millisecond, percentileAt(single, 100))
}

// ─── FormatDuration ───────────────────────────────────────────────────────────

// TestFormatDuration covers both the millisecond and second display paths.
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        time.Duration
		expected string
	}{
		{"zero", 0, "0ms"},
		{"500ms", 500 * time.Millisecond, "500ms"},
		{"999ms", 999 * time.Millisecond, "999ms"},
		{"exactly 1s", time.Second, "1.00s"},
		{"1.23s", 1230 * time.Millisecond, "1.23s"},
		{"7.44s", 7440 * time.Millisecond, "7.44s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, FormatDuration(tc.d))
		})
	}
}

// ─── Stats.PassesThreshold ────────────────────────────────────────────────────

// TestPassesThreshold verifies the three cases: pass, fail, and empty samples.
func TestPassesThreshold(t *testing.T) {
	t.Run("passes when p95 within threshold", func(t *testing.T) {
		s := Stats{Variant: VariantMinimal, P95: 2 * time.Second, SampleCount: 10}
		assert.True(t, s.PassesThreshold(), "2s p95 must be under 3s minimal threshold")
	})

	t.Run("fails when p95 exceeds threshold", func(t *testing.T) {
		s := Stats{Variant: VariantMinimal, P95: 4 * time.Second, SampleCount: 10}
		assert.False(t, s.PassesThreshold(), "4s p95 must exceed 3s minimal threshold")
	})

	t.Run("fails when all samples failed", func(t *testing.T) {
		s := Stats{Variant: VariantMinimal, SampleCount: 5, FailCount: 5}
		assert.False(t, s.PassesThreshold())
	})

	t.Run("fails when sample count is zero", func(t *testing.T) {
		s := Stats{Variant: VariantMinimal}
		assert.False(t, s.PassesThreshold())
	})

	t.Run("passes for unknown variant", func(t *testing.T) {
		s := Stats{Variant: "unknown", P95: 100 * time.Second, SampleCount: 5}
		// No threshold entry → defaults to pass so unknown variants don't block CI.
		assert.True(t, s.PassesThreshold())
	})
}

// ─── BenchmarkResult.AllPassed ───────────────────────────────────────────────

// TestAllPassed covers the empty result, all-pass, and mixed-pass cases.
func TestAllPassed(t *testing.T) {
	t.Run("empty results returns false", func(t *testing.T) {
		r := BenchmarkResult{Results: map[Variant]Stats{}}
		assert.False(t, r.AllPassed())
	})

	t.Run("all variants pass", func(t *testing.T) {
		r := BenchmarkResult{Results: map[Variant]Stats{
			VariantMinimal: {Variant: VariantMinimal, P95: 1 * time.Second, SampleCount: 5},
			VariantMedium:  {Variant: VariantMedium, P95: 2 * time.Second, SampleCount: 5},
			VariantHeavy:   {Variant: VariantHeavy, P95: 3 * time.Second, SampleCount: 5},
		}}
		assert.True(t, r.AllPassed())
	})

	t.Run("one variant fails", func(t *testing.T) {
		r := BenchmarkResult{Results: map[Variant]Stats{
			VariantMinimal: {Variant: VariantMinimal, P95: 1 * time.Second, SampleCount: 5},
			// Heavy exceeds its 10s threshold.
			VariantHeavy: {Variant: VariantHeavy, P95: 12 * time.Second, SampleCount: 5},
		}}
		require.False(t, r.AllPassed())
	})
}

// ─── P95Threshold ─────────────────────────────────────────────────────────────

// TestP95Threshold ensures the exported accessor returns the correct values.
func TestP95Threshold(t *testing.T) {
	assert.Equal(t, 3*time.Second, P95Threshold(VariantMinimal))
	assert.Equal(t, 5*time.Second, P95Threshold(VariantMedium))
	assert.Equal(t, 10*time.Second, P95Threshold(VariantHeavy))
	assert.Equal(t, time.Duration(0), P95Threshold("nonexistent"))
}

// ─── DefaultConfig ────────────────────────────────────────────────────────────

// TestDefaultConfig verifies that DefaultConfig returns non-zero sensible values.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 10, cfg.Samples)
	assert.Equal(t, "default", cfg.Namespace)
	assert.NotEmpty(t, cfg.BaseURL)
	assert.Greater(t, cfg.ScaleDownTimeout, time.Duration(0))
	assert.Greater(t, cfg.RequestTimeout, time.Duration(0))
	assert.Greater(t, cfg.PollInterval, time.Duration(0))
}

package measure

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// ComputeStats calculates percentile statistics over a slice of Samples.
//
// Failed samples (where Sample.Error != nil) are counted in Stats.FailCount
// but excluded from percentile calculations.  If all samples failed, the
// returned Stats has all duration fields set to zero.
//
// The percentile method used is "nearest rank":
//
//	index = ceil(p/100 × n) − 1   (0-based, clamped to [0, n-1])
//
// This matches the method used in Spike 0.6 (nats-routing/internal/spike/stats.go)
// and the behaviour of most standard benchmark tools (wrk, hey, etc.).
func ComputeStats(variant Variant, samples []Sample) Stats {
	s := Stats{
		Variant:     variant,
		ImageSize:   ImageSizes[variant],
		SampleCount: len(samples),
	}

	// Separate successful TTFB values from failed attempts.
	var durations []time.Duration
	for _, sample := range samples {
		if sample.Error != nil {
			s.FailCount++
			continue
		}
		durations = append(durations, sample.TTFB)
	}

	// Nothing to compute if every sample failed.
	if len(durations) == 0 {
		return s
	}

	// Sort ascending so that index-based percentile extraction is correct.
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	n := len(durations)
	s.Min = durations[0]
	s.Max = durations[n-1]
	s.P50 = percentileAt(durations, 50)
	s.P75 = percentileAt(durations, 75)
	s.P95 = percentileAt(durations, 95)
	s.P99 = percentileAt(durations, 99)

	return s
}

// percentileAt returns the duration at percentile p (0–100) in a pre-sorted slice.
//
// It uses the nearest-rank formula: index = ceil(p/100 × n) − 1, clamped to
// [0, n-1].  The result is always a value that exists in the slice — no
// interpolation is performed.
//
// Callers are responsible for ensuring sorted is non-empty; an empty slice
// returns 0.
func percentileAt(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	// ceil(p/100 × n) − 1 gives the 0-based index.
	// For p in (0, 100] and n ≥ 1 the result is always in [0, n-1].
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	// Clamp defensively in case of floating-point edge cases.
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// FormatDuration formats a duration as a human-readable string suitable for
// display in the benchmark table.
//
//   - Durations ≥ 1 s   → "1.23s"
//   - Durations < 1 s   → "456ms"
//   - Zero duration      → "0ms"
func FormatDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

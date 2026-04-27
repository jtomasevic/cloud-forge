package spike

import (
	"math"
	"sort"
	"time"
)

// ComputePercentiles sorts the provided latency slice in-place and returns a
// populated [LatencyStats] with p50, p95, p99, min, max, and throughput.
//
// The function uses the "nearest rank" method: for percentile p over n values
// the index is ceil(p/100 × n) − 1, clamped to [0, n−1].
//
// elapsed is the wall-clock duration for the entire benchmark run; it is used
// to compute the throughput (messages per second).
//
// Preconditions:
//   - len(latencies) > 0 (callers must guard against the empty case)
func ComputePercentiles(latencies []time.Duration, elapsed time.Duration) LatencyStats {
	// Sort ascending so index-based percentile extraction works correctly.
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	n := len(latencies)

	// pct returns the duration at the given percentile using the nearest-rank
	// formula.  The result is always within the bounds of the slice.
	pct := func(p float64) time.Duration {
		// ceil(p/100 * n) − 1 gives the 0-based index.
		// For p in (0,100] and n ≥ 1, idx is always in [0, n-1].
		idx := int(math.Ceil(p/100*float64(n))) - 1
		return latencies[idx]
	}

	return LatencyStats{
		P50: pct(50),
		P95: pct(95),
		P99: pct(99),
		Min: latencies[0],
		Max: latencies[n-1],
		// Throughput is the inverse of the average inter-message time.
		Throughput: float64(n) / elapsed.Seconds(),
	}
}

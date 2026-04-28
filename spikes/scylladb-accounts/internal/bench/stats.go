package bench

import (
	"math"
	"sort"
	"time"
)

// Percentile returns the p-th percentile (0–100) of the given sample slice.
// The slice is sorted in place; pass a copy if the original order matters.
//
// Uses the nearest-rank method: index = ceil(p/100 * n) - 1.
// Returns 0 if samples is empty.
func Percentile(samples Samples, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sort.Sort(samples)
	// nearest-rank: rank = ceil(p / 100 * n)
	rank := int(math.Ceil(p / 100.0 * float64(len(samples))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(samples) {
		rank = len(samples)
	}
	return samples[rank-1]
}

// MinDuration returns the smallest value in samples.
// Returns 0 if samples is empty.
func MinDuration(samples Samples) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	m := samples[0]
	for _, s := range samples[1:] {
		if s < m {
			m = s
		}
	}
	return m
}

// MaxDuration returns the largest value in samples.
// Returns 0 if samples is empty.
func MaxDuration(samples Samples) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	m := samples[0]
	for _, s := range samples[1:] {
		if s > m {
			m = s
		}
	}
	return m
}

// BuildResult computes p50/p95/p99/min/max from raw samples and attaches
// metadata into a Result.  samples is sorted in place.
func BuildResult(name BenchName, samples Samples, errs int, total time.Duration) Result {
	// Sort once; Percentile would sort per call otherwise.
	sort.Sort(samples)
	return Result{
		Name:          name,
		Ops:           len(samples),
		P50:           Percentile(samples, 50),
		P95:           Percentile(samples, 95),
		P99:           Percentile(samples, 99),
		Min:           MinDuration(samples),
		Max:           MaxDuration(samples),
		TotalDuration: total,
		Errors:        errs,
	}
}

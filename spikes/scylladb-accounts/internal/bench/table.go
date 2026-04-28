package bench

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	// thresholdHotPath is the p99 target for the CF-Router API key lookup.
	// Source: docs/3-Introduce-CF-VPC.md § 10.5
	thresholdHotPath = 2 * time.Millisecond

	// thresholdMV is the p99 target for materialized-view queries.
	// These are login-path or JWT-resolution lookups — less frequent.
	thresholdMV = 5 * time.Millisecond
)

// PrintResults writes a formatted table of benchmark results to w.
// Each row includes the benchmark name, operation count, percentiles,
// throughput, error count, and a PASS/FAIL verdict against the p99 threshold.
//
// Example output:
//
//	┌──────────────────────────────────┬──────┬───────┬───────┬───────┬──────────────┬──────┬────────┐
//	│ Benchmark                        │  Ops │  p50  │  p95  │  p99  │  Throughput  │ Err  │ Verdict│
//	├──────────────────────────────────┼──────┼───────┼───────┼───────┼──────────────┼──────┼────────┤
//	│ api_key_lookup (QUORUM)          │ 2000 │ 0.8ms │ 1.2ms │ 1.5ms │  3200 ops/s  │    0 │  PASS  │
//	└──────────────────────────────────┴──────┴───────┴───────┴───────┴──────────────┴──────┴────────┘
func PrintResults(w io.Writer, results []Result) {
	const (
		colBench = 36
		colOps   = 6
		colLat   = 7
		colTput  = 14
		colErr   = 6
		colVerdt = 8
	)

	sep := func(l, m, r, cross string) string {
		parts := []string{
			strings.Repeat("─", colBench+2),
			strings.Repeat("─", colOps+2),
			strings.Repeat("─", colLat+2),
			strings.Repeat("─", colLat+2),
			strings.Repeat("─", colLat+2),
			strings.Repeat("─", colTput+2),
			strings.Repeat("─", colErr+2),
			strings.Repeat("─", colVerdt+2),
		}
		return l + strings.Join(parts, cross) + r
	}

	fmt.Fprintln(w, sep("┌", "─", "┐", "┬"))
	fmt.Fprintf(w, "│ %-*s │ %*s │ %*s │ %*s │ %*s │ %*s │ %*s │ %*s │\n",
		colBench, "Benchmark",
		colOps, "Ops",
		colLat, "p50",
		colLat, "p95",
		colLat, "p99",
		colTput, "Throughput",
		colErr, "Err",
		colVerdt, "Verdict",
	)
	fmt.Fprintln(w, sep("├", "─", "┤", "┼"))

	for _, r := range results {
		_, threshold := verdictFor(BenchName(r.Name))
		ok := r.P99 <= threshold && r.Errors == 0
		verdictStr := "PASS"
		if !ok {
			verdictStr = "FAIL"
		}
		fmt.Fprintf(w, "│ %-*s │ %*d │ %*s │ %*s │ %*s │ %*s │ %*d │ %*s │\n",
			colBench, r.Name,
			colOps, r.Ops,
			colLat, fmtDur(r.P50),
			colLat, fmtDur(r.P95),
			colLat, fmtDur(r.P99),
			colTput, fmt.Sprintf("%.0f ops/s", r.Throughput()),
			colErr, r.Errors,
			colVerdt, verdictStr,
		)
	}

	fmt.Fprintln(w, sep("└", "─", "┘", "┴"))
	fmt.Fprintf(w, "\n  Targets: hot-path p99 < %s | MV query p99 < %s\n",
		thresholdHotPath, thresholdMV)
}

// PrintLWTResult writes a single LWT correctness + latency summary to w.
func PrintLWTResult(w io.Writer, r LWTResult) {
	correct := "YES ✓"
	if !r.Correct() {
		correct = fmt.Sprintf("NO ✗  (winners=%d, losers=%d)", r.Winners, r.Losers)
	}
	errStr := ""
	if r.Errors > 0 {
		errStr = fmt.Sprintf("  errors=%d ⚠", r.Errors)
	}
	fmt.Fprintf(w, "\n  %-36s  p50=%-7s  p99=%-7s  winners=%-3d  losers=%-3d  correct=%s%s\n",
		r.Name,
		fmtDur(r.P50),
		fmtDur(r.P99),
		r.Winners,
		r.Losers,
		correct,
		errStr,
	)
}

// verdictFor returns the p99 threshold for the given benchmark name.
// MV queries have a 5 ms target; everything else (hot path) has 2 ms.
func verdictFor(name BenchName) (string, time.Duration) {
	switch name {
	case BenchMVSlug, BenchMVEmail:
		return "MV", thresholdMV
	default:
		return "hot-path", thresholdHotPath
	}
}

// fmtDur formats a duration compactly in milliseconds with two decimal places.
func fmtDur(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	return fmt.Sprintf("%.2fms", ms)
}

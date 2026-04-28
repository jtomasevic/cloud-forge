package measure

import (
	"fmt"
	"io"
	"strings"
)

// PrintTable renders the full benchmark report to w.
//
// The report consists of four sections:
//  1. Header box — cluster, Knative version, platform, and run timestamp.
//  2. Results table — one row per variant with p50/p75/p95/p99/min/max.
//  3. Threshold analysis — per-variant pass (✓) / warn (⚠) / fail (✗) symbols.
//  4. Recommendation — min-replica defaults derived from the analysis.
//
// The output is designed to be readable in an 88-column terminal.
// It is also suitable for inclusion verbatim in FINDINGS.md.
func PrintTable(w io.Writer, result BenchmarkResult) {
	printHeader(w, result)
	printStatsTable(w, result)
	printThresholdAnalysis(w, result)
	printRecommendation(w, result)
}

// printHeader renders the top-of-table metadata box and column headers.
func printHeader(w io.Writer, result BenchmarkResult) {
	ts := result.StartedAt.UTC().Format("2006-01-02 15:04 UTC")
	knVer := result.KnativeVersion
	if knVer == "" {
		knVer = "unknown"
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "╔══════════════════════════════════════════════════════════════════════════════════════╗")
	fmt.Fprintln(w, "║  CloudForge — Knative Scale-to-Zero Cold Start Benchmark                            ║")
	fmt.Fprintf(w,  "║  Cluster : cloudforge-dev (k3d)    Knative Serving %-7s   net-kourier          ║\n", knVer)
	fmt.Fprintf(w,  "║  Platform: %-44s Started: %-18s  ║\n", result.Platform, ts)
	fmt.Fprintln(w, "╠══════════════╦══════════╦══════════╦═════════╦═════════╦═════════╦═════════╦════════╣")
	fmt.Fprintf(w,  "║ %-12s ║ %-8s ║ %-8s ║ %-7s ║ %-7s ║ %-7s ║ %-7s ║ %-6s ║\n",
		"Variant", "Image", "p50", "p75", "p95", "p99", "min", "max")
	fmt.Fprintln(w, "╠══════════════╬══════════╬══════════╬═════════╬═════════╬═════════╬═════════╬════════╣")
}

// printStatsTable renders one data row per variant and the closing border.
func printStatsTable(w io.Writer, result BenchmarkResult) {
	for _, v := range AllVariants {
		s, ok := result.Results[v]
		if !ok {
			// Variant was not measured — print a row with dashes.
			fmt.Fprintf(w, "║ %-12s ║ %8s ║ %8s ║ %7s ║ %7s ║ %7s ║ %7s ║ %6s ║\n",
				string(v), "-", "-", "-", "-", "-", "-", "-")
			continue
		}

		fmt.Fprintf(w, "║ %-12s ║ %8s ║ %8s ║ %7s ║ %7s ║ %7s ║ %7s ║ %6s ║\n",
			string(v),
			s.ImageSize,
			FormatDuration(s.P50),
			FormatDuration(s.P75),
			FormatDuration(s.P95),
			FormatDuration(s.P99),
			FormatDuration(s.Min),
			FormatDuration(s.Max),
		)
	}
	fmt.Fprintln(w, "╚══════════════╩══════════╩══════════╩═════════╩═════════╩═════════╩═════════╩════════╝")
	fmt.Fprintln(w)
}

// printThresholdAnalysis prints a per-variant pass/warn/fail line.
//
// Symbols:
//   - ✓  p95 is within the threshold and has headroom (below 80% of threshold).
//   - ⚠  p95 is within the threshold but close (≥ 80% of threshold).
//   - ✗  p95 exceeds the threshold.
func printThresholdAnalysis(w io.Writer, result BenchmarkResult) {
	fmt.Fprintf(w, "─── Threshold Analysis (p95) %s\n", strings.Repeat("─", 56))

	for _, v := range AllVariants {
		s, ok := result.Results[v]
		if !ok {
			fmt.Fprintf(w, "  ? %-10s: not measured\n", string(v))
			continue
		}

		threshold := s.Threshold()
		var symbol, note string

		switch {
		case !s.PassesThreshold():
			// Strictly exceeds the threshold.
			symbol = "✗"
			note = fmt.Sprintf("EXCEEDS %s threshold. min-replicas=1 REQUIRED.", FormatDuration(threshold))
		case threshold > 0 && s.P95 > threshold*8/10:
			// Within threshold but ≥ 80% of it — worth a warning.
			symbol = "⚠"
			note = fmt.Sprintf("within %s threshold but close. Recommend min-replicas=1 for AI functions.",
				FormatDuration(threshold))
		default:
			symbol = "✓"
			note = fmt.Sprintf("below %s threshold. Scale-to-zero is safe.", FormatDuration(threshold))
		}

		fmt.Fprintf(w, "  %s %-10s: p95 %-8s — %s\n", symbol, string(v), FormatDuration(s.P95), note)
	}

	fmt.Fprintln(w)
}

// printRecommendation writes the min-replica guidance section.
//
// It outputs the platform-wide default followed by per-variant overrides for
// any variant that failed or was borderline.
func printRecommendation(w io.Writer, result BenchmarkResult) {
	fmt.Fprintf(w, "─── Recommendation %s\n", strings.Repeat("─", 66))
	fmt.Fprintln(w, "  CF-FunctionTrigger default  minScale=0  maxScale=10")

	for _, v := range AllVariants {
		s, ok := result.Results[v]
		if !ok {
			continue
		}
		threshold := s.Threshold()
		switch {
		case !s.PassesThreshold():
			fmt.Fprintf(w, "  Override for %-10s    minScale=1  (enforced by admission webhook)\n", string(v))
		case threshold > 0 && s.P95 > threshold*8/10:
			fmt.Fprintf(w, "  Override for %-10s    minScale=1  (strongly recommended for AI functions)\n", string(v))
		}
	}

	fmt.Fprintln(w)

	if result.AllPassed() {
		fmt.Fprintln(w, "  ✓ All variants pass their p95 thresholds.")
	} else {
		fmt.Fprintln(w, "  ⚠ One or more variants exceed their p95 threshold — see analysis above.")
	}

	fmt.Fprintln(w)
}

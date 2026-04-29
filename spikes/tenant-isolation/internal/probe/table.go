package probe

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Terminal table formatter
// ──────────────────────────────────────────────────────────────────────────────

// PrintResults writes a formatted results table and a go/no-go verdict to w.
// The table has four columns: Test, Verdict, Duration, and Evidence (truncated).
func PrintResults(w io.Writer, results []TestResult) {
	const (
		col1 = 28 // test name
		col2 = 7  // verdict
		col3 = 10 // duration
		col4 = 60 // evidence
	)

	sep := strings.Repeat("─", col1+col2+col3+col4+7)
	header := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s",
		col1, "TEST", col2, "VERDICT", col3, "DURATION", col4, "EVIDENCE")

	fmt.Fprintln(w, sep)
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, sep)

	for _, r := range results {
		fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s\n",
			col1, r.Name,
			col2, colorVerdict(r.Verdict),
			col3, r.Duration.Round(time.Millisecond),
			col4, truncate(r.Evidence, col4),
		)
	}

	fmt.Fprintln(w, sep)

	overall := OverallVerdict(results)
	counts := CountByVerdict(results)
	fmt.Fprintf(w, "\n  Overall: %s   PASS=%d  FAIL=%d  SKIP=%d\n\n",
		colorVerdict(overall),
		counts[VerdictPass],
		counts[VerdictFail],
		counts[VerdictSkip],
	)
}

// PrintMetrics writes the key→value metrics for every test result, grouped by test.
func PrintMetrics(w io.Writer, results []TestResult) {
	for _, r := range results {
		if len(r.Metrics) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n── %s (%s) ─────\n", r.Name, r.Verdict)
		for k, v := range r.Metrics {
			fmt.Fprintf(w, "   %-40s %s\n", k, v)
		}
	}
}

// PrintSizingFormula writes the host-cluster sizing table derived from overhead metrics.
func PrintSizingFormula(w io.Writer, avgCPUMilli, avgMemMB int64) {
	rows := SizingFormula(avgCPUMilli, avgMemMB, []int{10, 50, 100, 200})
	if len(rows) == 0 {
		return
	}
	const sep = "─────────────────────────────────────────────────────────"
	fmt.Fprintln(w, "\n"+sep)
	fmt.Fprintf(w, "  HOST CLUSTER SIZING FORMULA (per vCluster: %dm CPU / %dMi RAM)\n", avgCPUMilli, avgMemMB)
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "  %-10s  %-15s  %-15s\n", "TENANTS", "TOTAL CPU (m)", "TOTAL RAM (GiB)")
	fmt.Fprintln(w, sep)
	for _, r := range rows {
		fmt.Fprintf(w, "  %-10d  %-15d  %-15.1f\n", r.Tenants, r.TotalCPUM, r.TotalMemGB)
	}
	fmt.Fprintln(w, sep)
}

// ── colour helpers (ANSI escape codes only on TTYs; plain fallback otherwise) ──

// colorVerdict wraps the verdict string in ANSI colour codes.
// PASS → green, FAIL → red, SKIP → yellow.
func colorVerdict(v Verdict) string {
	switch v {
	case VerdictPass:
		return "\033[32m" + string(v) + "\033[0m"
	case VerdictFail:
		return "\033[31m" + string(v) + "\033[0m"
	case VerdictSkip:
		return "\033[33m" + string(v) + "\033[0m"
	default:
		return string(v)
	}
}

// StripANSI removes ANSI escape sequences from s.
// Used in tests to compare verdicts without colour codes.
func StripANSI(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm'
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // skip 'm'
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

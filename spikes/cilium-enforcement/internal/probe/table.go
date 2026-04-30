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

// PrintResults writes a formatted results table and overall verdict to w.
func PrintResults(w io.Writer, results []TestResult) {
	const (
		col1 = 24 // test name
		col2 = 7  // verdict
		col3 = 10 // duration
		col4 = 60 // evidence
	)
	sep := strings.Repeat("─", col1+col2+col3+col4+7)
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s\n",
		col1, "TEST", col2, "VERDICT", col3, "DURATION", col4, "EVIDENCE")
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
		colorVerdict(overall), counts[VerdictPass], counts[VerdictFail], counts[VerdictSkip])
}

// PrintMetrics writes key→value metrics for every test that has them.
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

// ── Colour helpers ────────────────────────────────────────────────────────────

// colorVerdict wraps the verdict string in ANSI colour codes.
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

// StripANSI removes ANSI escape sequences from s. Used in tests.
func StripANSI(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

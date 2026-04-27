package spike

import (
	"fmt"
	"io"
	"strings"
	"time"

	"log/slog"
)

// PrintResults writes a structured summary of all spike findings to w.
//
// The format mirrors the "Questions This Spike Must Answer" table in
// docs/plan/0-foundation-and-spikes.md so an engineer can copy-paste into
// FINDINGS.md with minimal editing.
//
// PrintResults returns true if all critical spike questions (Q1, Q2, Q4) passed
// so callers can decide whether to exit non-zero without calling os.Exit
// themselves.
func PrintResults(r SpikeResult, w io.Writer, logger *slog.Logger) bool {
	sep := strings.Repeat("─", 72)
	lines := []string{
		"",
		sep,
		"  SPIKE 0.6 — NATS JetStream Multi-Tenant Routing — RESULTS",
		sep,
		"",
		fmt.Sprintf("  Q1  Dynamic provisioning without restart   %s", PassLabel(r.Q1Pass)),
		fmt.Sprintf("      %s", r.Q1Detail),
		"",
		fmt.Sprintf("  Q2  Cross-account isolation complete        %s", PassLabel(r.Q2Pass)),
		fmt.Sprintf("      %s", r.Q2Detail),
		"",
		"  Q3  Publish latency (1KB CloudEvent, JetStream sync publish)",
		fmt.Sprintf("      p50 %-10s  p95 %-10s  p99 %-10s", r.Q3Stats.P50, r.Q3Stats.P95, r.Q3Stats.P99),
		fmt.Sprintf("      min %-10s  max %-10s  throughput %.0f msg/s", r.Q3Stats.Min, r.Q3Stats.Max, r.Q3Stats.Throughput),
		fmt.Sprintf("      threshold: p99 < 5ms  →  %s", PassLabel(r.Q3Stats.P99 < 5*time.Millisecond)),
		"",
		fmt.Sprintf("  Q4  Content-based routing implemented       %s", PassLabel(r.Q4Pass)),
		fmt.Sprintf("      %s", r.Q4Detail),
		"",
		fmt.Sprintf("  Q5  50 accounts provisioned in < 2m         %s", PassLabel(r.Q5Pass)),
		fmt.Sprintf("      Duration: %s", r.Q5Duration.Round(time.Millisecond)),
		fmt.Sprintf("      %s", r.Q5Detail),
		"",
		sep,
	}

	// Write the human-readable block.
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}

	// Emit a structured log record for CI log parsers.
	logger.Info("spike results",
		"q1_pass", r.Q1Pass,
		"q2_pass", r.Q2Pass,
		"q3_p99", r.Q3Stats.P99.String(),
		"q3_pass", r.Q3Stats.P99 < 5*time.Millisecond,
		"q4_pass", r.Q4Pass,
		"q5_pass", r.Q5Pass,
		"q5_duration", r.Q5Duration.String(),
	)

	critical := r.Q1Pass && r.Q2Pass && r.Q4Pass

	if critical {
		fmt.Fprintln(w, "\n  ✓ All critical spike questions passed.")
	} else {
		fmt.Fprintln(w, "\n  ✗ One or more critical spike questions failed. See FINDINGS.md.")
	}

	return critical
}

// PassLabel returns a human-readable PASS/FAIL marker for terminal output.
func PassLabel(ok bool) string {
	if ok {
		return "PASS ✓"
	}
	return "FAIL ✗"
}

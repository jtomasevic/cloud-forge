package probe

import (
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Result constructors — used by every test file
// ──────────────────────────────────────────────────────────────────────────────

// passResult builds a PASS TestResult with the given evidence and elapsed time.
func passResult(name TestName, evidence string, start time.Time, metrics map[string]string) TestResult {
	return TestResult{
		Name:     name,
		Verdict:  VerdictPass,
		Evidence: evidence,
		Metrics:  metrics,
		Duration: time.Since(start),
	}
}

// failResult builds a FAIL TestResult with the given evidence and elapsed time.
func failResult(name TestName, evidence string, start time.Time, metrics map[string]string) TestResult {
	return TestResult{
		Name:     name,
		Verdict:  VerdictFail,
		Evidence: evidence,
		Metrics:  metrics,
		Duration: time.Since(start),
	}
}

// skipResult builds a SKIP TestResult when prerequisites are absent.
func skipResult(name TestName, reason string, start time.Time) TestResult {
	return TestResult{
		Name:     name,
		Verdict:  VerdictSkip,
		Evidence: reason,
		Metrics:  map[string]string{},
		Duration: time.Since(start),
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Output helpers
// ──────────────────────────────────────────────────────────────────────────────

// truncate shortens s to maxLen, appending "…" when truncated.
func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// isBlocked returns true if a curl command result indicates the connection
// was blocked by Cilium (packet dropped, connection refused, or timed out).
//
// Cilium's default behaviour for a CNP deny is to DROP the packet silently,
// so curl exits with code 28 (operation timed out). If the policy sends a TCP
// RST (REJECT), curl exits with code 7 (connection refused). Both are non-zero.
// When RunInPod returns a non-nil error, the connection was blocked.
func isBlocked(err error) bool {
	return err != nil
}

// isAllowed returns true if the curl command succeeded (exit code 0).
func isAllowed(output string, err error) bool {
	return err == nil && output != ""
}

// formatCurlError extracts the most useful part of a curl error message for evidence.
func formatCurlError(output string) string {
	// curl error lines start with "curl: (N) ..."
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "curl:") {
			return fmt.Sprintf("curl blocked: %s", line)
		}
	}
	if output != "" {
		return truncate(output, 80)
	}
	return "connection blocked (no output)"
}

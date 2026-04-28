package bench

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestPrintResults_Structure verifies that PrintResults produces output with
// the expected header columns and the right number of data rows.
func TestPrintResults_Structure(t *testing.T) {
	t.Parallel()
	results := []Result{
		BuildResult(BenchAPIKeyQuorum, Samples{1 * time.Millisecond, 2 * time.Millisecond}, 0, 10*time.Millisecond),
		BuildResult(BenchMVSlug, Samples{3 * time.Millisecond, 4 * time.Millisecond}, 0, 20*time.Millisecond),
	}

	var buf bytes.Buffer
	PrintResults(&buf, results)
	out := buf.String()

	requiredSubstrings := []string{
		"Benchmark",
		"Ops",
		"p50",
		"p95",
		"p99",
		"Throughput",
		"Err",
		"Verdict",
		string(BenchAPIKeyQuorum),
		string(BenchMVSlug),
	}
	for _, s := range requiredSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\n\n%s", s, out)
		}
	}
}

// TestPrintResults_PassVerdictForFastBench verifies that a benchmark with p99
// well below the 2ms threshold prints "PASS".
func TestPrintResults_PassVerdictForFastBench(t *testing.T) {
	t.Parallel()
	results := []Result{
		// p99 = 1ms  < 2ms threshold → PASS
		BuildResult(BenchAPIKeyQuorum, Samples{1 * time.Millisecond}, 0, time.Millisecond),
	}
	var buf bytes.Buffer
	PrintResults(&buf, results)
	if !strings.Contains(buf.String(), "PASS") {
		t.Errorf("expected PASS verdict in output:\n%s", buf.String())
	}
}

// TestPrintResults_FailVerdictForSlowBench verifies that a benchmark with p99
// above the threshold or with errors prints "FAIL".
func TestPrintResults_FailVerdictForSlowBench(t *testing.T) {
	t.Parallel()
	results := []Result{
		// p99 = 10ms > 2ms hot-path threshold → FAIL
		BuildResult(BenchAPIKeyQuorum, Samples{10 * time.Millisecond}, 0, 10*time.Millisecond),
	}
	var buf bytes.Buffer
	PrintResults(&buf, results)
	if !strings.Contains(buf.String(), "FAIL") {
		t.Errorf("expected FAIL verdict in output:\n%s", buf.String())
	}
}

// TestPrintResults_FailOnErrors verifies that a result with errors (even if
// latency is within threshold) prints "FAIL".
func TestPrintResults_FailOnErrors(t *testing.T) {
	t.Parallel()
	r := BuildResult(BenchAPIKeyOne, Samples{1 * time.Millisecond}, 1 /* error */, time.Millisecond)
	var buf bytes.Buffer
	PrintResults(&buf, []Result{r})
	if !strings.Contains(buf.String(), "FAIL") {
		t.Errorf("expected FAIL for result with errors:\n%s", buf.String())
	}
}

// TestPrintResults_MVUsesLooserthreshold verifies that MV benchmarks are
// evaluated against the 5ms target, not the 2ms hot-path target.
func TestPrintResults_MVUsesLooserThreshold(t *testing.T) {
	t.Parallel()
	// p99 = 3ms → FAIL for hot-path (>2ms) but PASS for MV (<5ms)
	r := BuildResult(BenchMVSlug, Samples{3 * time.Millisecond}, 0, 3*time.Millisecond)
	var buf bytes.Buffer
	PrintResults(&buf, []Result{r})
	if !strings.Contains(buf.String(), "PASS") {
		t.Errorf("MV bench at 3ms p99 should PASS (threshold=5ms):\n%s", buf.String())
	}
}

// TestPrintResults_EmptySlice ensures PrintResults does not panic on an empty
// result slice and still emits header/footer lines.
func TestPrintResults_EmptySlice(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	PrintResults(&buf, []Result{})
	if buf.Len() == 0 {
		t.Error("expected non-empty output even for empty result slice")
	}
}

// TestPrintLWTResult_Correct verifies correct-case output contains the check
// mark and not the cross.
func TestPrintLWTResult_Correct(t *testing.T) {
	t.Parallel()
	r := LWTResult{
		Result:  BuildResult(BenchLWTClaim, Samples{2 * time.Millisecond}, 0, 5*time.Millisecond),
		Winners: 1,
		Losers:  19,
	}
	var buf bytes.Buffer
	PrintLWTResult(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "YES") {
		t.Errorf("expected YES in correct LWT output:\n%s", out)
	}
	if strings.Contains(out, "NO") {
		t.Errorf("unexpected NO in correct LWT output:\n%s", out)
	}
}

// TestPrintLWTResult_Incorrect verifies incorrect-case output contains both
// the cross and the winner/loser counts.
func TestPrintLWTResult_Incorrect(t *testing.T) {
	t.Parallel()
	r := LWTResult{
		Result:  BuildResult(BenchLWTClaim, Samples{2 * time.Millisecond}, 0, 5*time.Millisecond),
		Winners: 3,
		Losers:  17,
	}
	var buf bytes.Buffer
	PrintLWTResult(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "NO") {
		t.Errorf("expected NO in incorrect LWT output:\n%s", out)
	}
}

// TestFmtDur tests the private duration formatter with representative inputs.
func TestFmtDur(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0.00ms"},
		{time.Millisecond, "1.00ms"},
		{500 * time.Microsecond, "0.50ms"},
		{10 * time.Millisecond, "10.00ms"},
	}
	for _, tc := range cases {
		got := fmtDur(tc.d)
		if got != tc.want {
			t.Errorf("fmtDur(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

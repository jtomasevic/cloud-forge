package probe

import (
	"bytes"
	"testing"
	"time"
)

func TestPrintResults_ContainsTestNames(t *testing.T) {
	results := []TestResult{
		{Name: TestCrossNamespaceDeny, Verdict: VerdictPass, Duration: 5 * time.Second,
			Evidence: "blocked", Metrics: map[string]string{}},
		{Name: TestPolicyTrace, Verdict: VerdictSkip, Duration: 100 * time.Millisecond,
			Evidence: "no cilium", Metrics: map[string]string{}},
	}
	var buf bytes.Buffer
	PrintResults(&buf, results)
	out := buf.String()
	for _, sub := range []string{"cross_namespace_deny", "policy_trace", "PASS", "SKIP"} {
		if !hasSubstr(StripANSI(out), sub) {
			t.Errorf("PrintResults output missing %q", sub)
		}
	}
}

func TestPrintMetrics_ShowsMetrics(t *testing.T) {
	results := []TestResult{
		{Name: TestCrossNamespaceDeny, Verdict: VerdictPass,
			Metrics: map[string]string{"echo_pod_ip": "10.0.0.5"}},
	}
	var buf bytes.Buffer
	PrintMetrics(&buf, results)
	out := buf.String()
	if !hasSubstr(out, "echo_pod_ip") {
		t.Errorf("PrintMetrics output missing 'echo_pod_ip', got: %s", out)
	}
}

func TestPrintMetrics_EmptyMetrics(t *testing.T) {
	results := []TestResult{
		{Name: TestPolicyTrace, Verdict: VerdictSkip, Metrics: map[string]string{}},
	}
	var buf bytes.Buffer
	PrintMetrics(&buf, results)
	// No output for empty metrics.
	if buf.Len() != 0 {
		t.Errorf("PrintMetrics should produce no output for empty metrics, got: %s", buf.String())
	}
}

func TestColorVerdict_ANSI(t *testing.T) {
	cases := []struct {
		v    Verdict
		want string // part that should appear after stripping ANSI
	}{
		{VerdictPass, "PASS"},
		{VerdictFail, "FAIL"},
		{VerdictSkip, "SKIP"},
	}
	for _, c := range cases {
		got := StripANSI(colorVerdict(c.v))
		if got != c.want {
			t.Errorf("StripANSI(colorVerdict(%s)) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestStripANSI(t *testing.T) {
	input := "\033[32mPASS\033[0m"
	got := StripANSI(input)
	if got != "PASS" {
		t.Errorf("StripANSI(%q) = %q, want %q", input, got, "PASS")
	}
}

package probe

import (
	"strings"
	"testing"
	"time"
)

// ── StripANSI ─────────────────────────────────────────────────────────────────

func TestStripANSI(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"\033[32mPASS\033[0m", "PASS"},
		{"\033[31mFAIL\033[0m", "FAIL"},
		{"\033[33mSKIP\033[0m", "SKIP"},
		{"no color", "no color"},
		{"", ""},
	}
	for _, c := range cases {
		got := StripANSI(c.input)
		if got != c.want {
			t.Errorf("StripANSI(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ── colorVerdict ──────────────────────────────────────────────────────────────

func TestColorVerdict_ContainsVerdict(t *testing.T) {
	for _, v := range []Verdict{VerdictPass, VerdictFail, VerdictSkip} {
		colored := colorVerdict(v)
		plain := StripANSI(colored)
		if plain != string(v) {
			t.Errorf("colorVerdict(%s) stripped = %q, want %q", v, plain, v)
		}
	}
}

// ── PrintResults ──────────────────────────────────────────────────────────────

func TestPrintResults_ContainsAllTests(t *testing.T) {
	results := []TestResult{
		{Name: TestNetworkIsolation, Verdict: VerdictPass, Evidence: "blocked", Duration: 1 * time.Second, Metrics: map[string]string{}},
		{Name: TestProvisioningSpeed, Verdict: VerdictFail, Evidence: "too slow", Duration: 2 * time.Second, Metrics: map[string]string{}},
		{Name: TestCiliumEnforcement, Verdict: VerdictSkip, Evidence: "no cilium", Duration: 0, Metrics: map[string]string{}},
	}

	var sb strings.Builder
	PrintResults(&sb, results)
	out := sb.String()

	// Check all test names appear
	for _, name := range []string{"network_isolation", "provisioning_speed", "cilium_enforcement"} {
		if !hasSubstr(out, name) {
			t.Errorf("PrintResults output missing test name %q", name)
		}
	}

	// Check evidence appears (may be truncated)
	if !hasSubstr(out, "blocked") {
		t.Errorf("PrintResults output missing evidence 'blocked'")
	}

	// Check overall summary line
	if !hasSubstr(StripANSI(out), "Overall:") {
		t.Errorf("PrintResults output missing 'Overall:' summary")
	}
}

func TestPrintResults_SummaryCountsCorrect(t *testing.T) {
	results := []TestResult{
		{Name: TestNetworkIsolation, Verdict: VerdictPass, Metrics: map[string]string{}},
		{Name: TestProvisioningSpeed, Verdict: VerdictFail, Metrics: map[string]string{}},
		{Name: TestCiliumEnforcement, Verdict: VerdictSkip, Metrics: map[string]string{}},
	}

	var sb strings.Builder
	PrintResults(&sb, results)
	out := StripANSI(sb.String())

	if !hasSubstr(out, "PASS=1") {
		t.Errorf("expected PASS=1 in output: %q", out)
	}
	if !hasSubstr(out, "FAIL=1") {
		t.Errorf("expected FAIL=1 in output: %q", out)
	}
	if !hasSubstr(out, "SKIP=1") {
		t.Errorf("expected SKIP=1 in output: %q", out)
	}
}

// ── PrintMetrics ──────────────────────────────────────────────────────────────

func TestPrintMetrics_ShowsMetrics(t *testing.T) {
	results := []TestResult{
		{
			Name:    TestNetworkIsolation,
			Verdict: VerdictPass,
			Metrics: map[string]string{"tenant_b_echo_ip": "10.100.2.5"},
		},
		{
			Name:    TestProvisioningSpeed,
			Verdict: VerdictPass,
			Metrics: map[string]string{}, // no metrics → skipped
		},
	}

	var sb strings.Builder
	PrintMetrics(&sb, results)
	out := sb.String()

	if !hasSubstr(out, "10.100.2.5") {
		t.Errorf("PrintMetrics missing metric value 'tenant_b_echo_ip': %q", out)
	}
}

func TestPrintMetrics_EmptyMetrics(t *testing.T) {
	results := []TestResult{
		{Name: TestCiliumEnforcement, Verdict: VerdictSkip, Metrics: map[string]string{}},
	}
	var sb strings.Builder
	PrintMetrics(&sb, results)
	// Should produce no output for empty metrics
	if sb.Len() != 0 {
		t.Errorf("expected no output for empty metrics, got: %q", sb.String())
	}
}

// ── PrintSizingFormula ────────────────────────────────────────────────────────

func TestPrintSizingFormula_ContainsRows(t *testing.T) {
	var sb strings.Builder
	PrintSizingFormula(&sb, 50, 256)
	out := sb.String()

	for _, sub := range []string{"10", "50", "200", "CPU", "RAM"} {
		if !hasSubstr(out, sub) {
			t.Errorf("PrintSizingFormula output missing %q", sub)
		}
	}
}

func TestPrintSizingFormula_ZeroOverhead(t *testing.T) {
	// Zero overhead — should not panic, should produce no output
	var sb strings.Builder
	PrintSizingFormula(&sb, 0, 0)
	// Even with zero values, the function should produce a table (not crash)
}

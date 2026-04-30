package probe

import (
	"testing"
	"time"
)

func TestAllTestsContainsAll(t *testing.T) {
	names := map[TestName]bool{
		TestCrossNamespaceDeny:  true,
		TestIntraNamespaceAllow: true,
		TestPlatformIsolation:   true,
		TestPolicyTrace:         true,
		TestVClusterCoexistence: true,
	}
	if len(allTests) != len(names) {
		t.Fatalf("allTests length = %d, want %d", len(allTests), len(names))
	}
	for _, n := range allTests {
		if !names[n] {
			t.Errorf("unexpected test in allTests: %q", n)
		}
	}
}

func TestTestResult_Pass(t *testing.T) {
	cases := []struct {
		v    Verdict
		want bool
	}{
		{VerdictPass, true},
		{VerdictFail, false},
		{VerdictSkip, false},
	}
	for _, c := range cases {
		r := TestResult{Verdict: c.v}
		if got := r.Pass(); got != c.want {
			t.Errorf("verdict %q Pass() = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestTestResult_String(t *testing.T) {
	r := TestResult{
		Name:     TestCrossNamespaceDeny,
		Verdict:  VerdictPass,
		Duration: 1234 * time.Millisecond,
	}
	s := r.String()
	for _, sub := range []string{"PASS", "cross_namespace_deny", "1.234s"} {
		if !hasSubstr(s, sub) {
			t.Errorf("String() = %q, missing %q", s, sub)
		}
	}
}

func TestTestResult_MetricOrEmpty(t *testing.T) {
	r := TestResult{Metrics: map[string]string{"k": "v"}}
	if got := r.MetricOrEmpty("k"); got != "v" {
		t.Errorf("got %q, want %q", got, "v")
	}
	if got := r.MetricOrEmpty("missing"); got != "-" {
		t.Errorf("got %q, want -", got)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TenantANamespace == "" {
		t.Error("TenantANamespace must not be empty")
	}
	if cfg.TenantBNamespace == "" {
		t.Error("TenantBNamespace must not be empty")
	}
	if cfg.TenantANamespace == cfg.TenantBNamespace {
		t.Error("TenantA and TenantB namespaces must differ")
	}
	if cfg.PlatformNamespace == "" {
		t.Error("PlatformNamespace must not be empty")
	}
	if cfg.VClusterNamespace == "" {
		t.Error("VClusterNamespace must not be empty")
	}
	if cfg.PodReadyTimeout == 0 {
		t.Error("PodReadyTimeout must be non-zero")
	}
	if cfg.ExecTimeout == 0 {
		t.Error("ExecTimeout must be non-zero")
	}
	if cfg.ConnectTimeoutSeconds <= 0 {
		t.Error("ConnectTimeoutSeconds must be positive")
	}
}

// ── helpers.go ────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello w…"},
		{"", 5, ""},
		{"newline\nhere", 20, "newline here"},
	}
	for _, c := range cases {
		got := truncate(c.in, c.max)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestIsBlocked(t *testing.T) {
	if !isBlocked(errFake("timeout")) {
		t.Error("isBlocked with error should return true")
	}
	if isBlocked(nil) {
		t.Error("isBlocked(nil) should return false")
	}
}

func TestIsAllowed(t *testing.T) {
	if !isAllowed("pong\n", nil) {
		t.Error("isAllowed with output and no error should return true")
	}
	if isAllowed("", errFake("refused")) {
		t.Error("isAllowed with error should return false")
	}
	if isAllowed("", nil) {
		t.Error("isAllowed with empty output should return false")
	}
}

func TestFormatCurlError(t *testing.T) {
	out := "curl: (28) Operation timed out after 5001 milliseconds"
	got := formatCurlError(out)
	if !hasSubstr(got, "curl blocked") {
		t.Errorf("formatCurlError should contain 'curl blocked', got %q", got)
	}
	if !hasSubstr(got, "(28)") {
		t.Errorf("formatCurlError should contain curl code, got %q", got)
	}
}

func TestFormatCurlError_FallbackToOutput(t *testing.T) {
	// No "curl:" prefix — fall back to truncated output.
	got := formatCurlError("some non-curl error text")
	if !hasSubstr(got, "non-curl error text") {
		t.Errorf("formatCurlError fallback = %q, expected truncated output", got)
	}
}

func TestFormatCurlError_EmptyOutput(t *testing.T) {
	got := formatCurlError("")
	if got != "connection blocked (no output)" {
		t.Errorf("formatCurlError('') = %q, want 'connection blocked (no output)'", got)
	}
}

func TestColorVerdict_Default(t *testing.T) {
	// Unknown verdict should return the raw string (no ANSI codes).
	got := colorVerdict(Verdict("UNKNOWN"))
	if got != "UNKNOWN" {
		t.Errorf("colorVerdict(UNKNOWN) = %q, want plain UNKNOWN", got)
	}
}

// errFake returns a simple error for use in table tests.
type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func errFake(msg string) error { return fakeErr(msg) }

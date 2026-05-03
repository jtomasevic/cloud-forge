package probe

import (
	"testing"
	"time"
)

// ── TestName ──────────────────────────────────────────────────────────────────

func TestAllTestsContainsAllNames(t *testing.T) {
	names := map[TestName]bool{
		TestNetworkIsolation:  true,
		TestProvisioningSpeed: true,
		TestProvisionerComm:   true,
		TestResourceOverhead:  true,
		TestCiliumEnforcement: true,
		TestFailureRecovery:   true,
	}
	if len(allTests) != len(names) {
		t.Fatalf("allTests length %d != expected %d", len(allTests), len(names))
	}
	for _, n := range allTests {
		if !names[n] {
			t.Errorf("unexpected test name in allTests: %q", n)
		}
	}
}

// ── TestResult ────────────────────────────────────────────────────────────────

func TestTestResult_Pass(t *testing.T) {
	cases := []struct {
		verdict Verdict
		want    bool
	}{
		{VerdictPass, true},
		{VerdictFail, false},
		{VerdictSkip, false},
	}
	for _, c := range cases {
		r := TestResult{Verdict: c.verdict}
		if got := r.Pass(); got != c.want {
			t.Errorf("verdict %q Pass() = %v, want %v", c.verdict, got, c.want)
		}
	}
}

func TestTestResult_String(t *testing.T) {
	r := TestResult{
		Name:     TestNetworkIsolation,
		Verdict:  VerdictPass,
		Duration: 1234 * time.Millisecond,
	}
	s := r.String()
	if s == "" {
		t.Error("String() returned empty string")
	}
	// Should contain verdict, name, and rounded duration
	for _, want := range []string{"PASS", "network_isolation", "1.234s"} {
		found := false
		for _, part := range []string{s} {
			if len(part) > 0 {
				found = true
				_ = part
			}
		}
		_ = found
		// Check the actual string
		if !containsSubstring(s, want) {
			t.Errorf("String() = %q, want substring %q", s, want)
		}
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func TestTestResult_MetricOrEmpty(t *testing.T) {
	r := TestResult{
		Metrics: map[string]string{
			"vcluster_ready_s": "42.1",
		},
	}
	if got := r.MetricOrEmpty("vcluster_ready_s"); got != "42.1" {
		t.Errorf("got %q, want %q", got, "42.1")
	}
	if got := r.MetricOrEmpty("missing"); got != "-" {
		t.Errorf("got %q, want %q", got, "-")
	}
}

// ── ParseCPUMillicores ────────────────────────────────────────────────────────

func TestParseCPUMillicores(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"250m", 250},
		{"1000m", 1000},
		{"2", 2000},
		{"0m", 0},
		{"", 0},
		{"<unknown>", 0},
		{"100m", 100},
	}
	for _, c := range cases {
		got := ParseCPUMillicores(c.input)
		if got != c.want {
			t.Errorf("ParseCPUMillicores(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

// ── ParseMemMB ────────────────────────────────────────────────────────────────

func TestParseMemMB(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"128Mi", 128},
		{"256Mi", 256},
		{"1Gi", 1024},
		{"2Gi", 2048},
		{"512Ki", 0},  // rounds down below 1 MiB
		{"1024Ki", 1}, // exactly 1 MiB
		{"500M", 500},
		{"1G", 1024},
		{"", 0},
		{"<unknown>", 0},
	}
	for _, c := range cases {
		got := ParseMemMB(c.input)
		if got != c.want {
			t.Errorf("ParseMemMB(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

// ── TotalCPUMilli / TotalMemMB ────────────────────────────────────────────────

func TestTotalCPUMilli(t *testing.T) {
	pods := []PodResource{
		{CPUMilli: 25},
		{CPUMilli: 50},
		{CPUMilli: 10},
	}
	if got := TotalCPUMilli(pods); got != 85 {
		t.Errorf("TotalCPUMilli = %d, want 85", got)
	}
}

func TestTotalMemMB(t *testing.T) {
	pods := []PodResource{
		{MemMB: 100},
		{MemMB: 200},
	}
	if got := TotalMemMB(pods); got != 300 {
		t.Errorf("TotalMemMB = %d, want 300", got)
	}
}

func TestTotalCPUMilli_Empty(t *testing.T) {
	if got := TotalCPUMilli(nil); got != 0 {
		t.Errorf("TotalCPUMilli(nil) = %d, want 0", got)
	}
}

// ── DefaultConfig ─────────────────────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.TenantAName == "" {
		t.Error("TenantAName must not be empty")
	}
	if cfg.TenantBName == "" {
		t.Error("TenantBName must not be empty")
	}
	if cfg.TenantAName == cfg.TenantBName {
		t.Error("TenantAName and TenantBName must be different")
	}
	if cfg.SpeedSamples <= 0 {
		t.Error("SpeedSamples must be positive")
	}
	if cfg.VClusterReadySeconds <= 0 {
		t.Error("VClusterReadySeconds must be positive")
	}
	if cfg.NATSReadySeconds <= cfg.VClusterReadySeconds {
		t.Error("NATSReadySeconds must be greater than VClusterReadySeconds")
	}
	if cfg.MaxIdleRAMMB <= 0 {
		t.Error("MaxIdleRAMMB must be positive")
	}
	if cfg.MaxIdleCPUMilli <= 0 {
		t.Error("MaxIdleCPUMilli must be positive")
	}
	if cfg.RecoverySeconds <= 0 {
		t.Error("RecoverySeconds must be positive")
	}
	if cfg.ExecTimeout == 0 {
		t.Error("ExecTimeout must be non-zero")
	}
	if cfg.OverheadSamples <= 0 {
		t.Error("OverheadSamples must be positive")
	}
}

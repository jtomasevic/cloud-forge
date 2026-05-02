package probe

import (
	"context"
	"testing"
	"time"
)

// ── isConnectionBlocked ───────────────────────────────────────────────────────

func TestIsConnectionBlocked(t *testing.T) {
	cases := []struct {
		output string
		want   bool
	}{
		{"nc: connection refused", true},
		{"connect to host: No route to host", true},
		{"nc: network unreachable", true},
		{"connection timed out", true},
		{"host is unreachable", true},
		{"nc (10.100.2.5:8080) open", false}, // successful connection
		{"10.100.2.5 (10.100.2.5:8080) succeeded", false},
		{"", true},                   // empty = blocked
		{"some unknown error", true}, // ambiguous = blocked
	}
	for _, c := range cases {
		got := isConnectionBlocked(c.output)
		if got != c.want {
			t.Errorf("isConnectionBlocked(%q) = %v, want %v", c.output, got, c.want)
		}
	}
}

// ── isDNSBlocked ──────────────────────────────────────────────────────────────

func TestIsDNSBlocked(t *testing.T) {
	cases := []struct {
		output string
		want   bool
	}{
		{"** server can't find echo.default.svc.cluster.local: NXDOMAIN", true},
		{"nslookup: can't find echo.default.svc.cluster.local", true},
		{"Name or service not known", true},
		{"connection refused", true},
		{"no servers could be reached", true},
		// Successful resolution — isolation failure
		{"Server: 10.96.0.10\nAddress: 10.96.0.53\nName: echo.default.svc.cluster.local\nAddresses: 10.200.0.5", false},
		{"", true}, // empty = blocked
	}
	for _, c := range cases {
		got := isDNSBlocked(c.output)
		if got != c.want {
			t.Errorf("isDNSBlocked(%q) = %v, want %v", c.output, got, c.want)
		}
	}
}

// ── buildIsolationEvidence ────────────────────────────────────────────────────

func TestBuildIsolationEvidence(t *testing.T) {
	cases := []struct {
		ir          IsolationResult
		ip          string
		wantPass    bool
		wantSubstrs []string
	}{
		{
			ir:          IsolationResult{DirectIPBlocked: true, DNSBlocked: true},
			ip:          "10.100.2.5",
			wantPass:    true,
			wantSubstrs: []string{"10.100.2.5", "BLOCKED ✓", "DNS=BLOCKED"},
		},
		{
			ir:          IsolationResult{DirectIPBlocked: false, DNSBlocked: true},
			ip:          "10.100.2.5",
			wantPass:    false,
			wantSubstrs: []string{"REACHABLE ✗"},
		},
		{
			ir:          IsolationResult{DirectIPBlocked: true, DNSBlocked: false},
			ip:          "10.100.2.5",
			wantPass:    false,
			wantSubstrs: []string{"DNS=RESOLVED ✗"},
		},
	}
	for _, c := range cases {
		ev := buildIsolationEvidence(c.ir, c.ip)
		for _, sub := range c.wantSubstrs {
			if !hasSubstr(ev, sub) {
				t.Errorf("evidence %q missing expected substring %q", ev, sub)
			}
		}
	}
}

// ── RunTestNetworkIsolation — unit test with FakeClient ───────────────────────

func TestRunTestNetworkIsolation_Pass(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "echo-server",
		PodIPResult:         "10.100.2.5",
		RunInPodResponses: []RunInPodResponse{
			// netprobe ready handled by WaitPodReady, this is called for nc + nslookup
			{Output: "nc: connection refused"},                                        // direct IP blocked
			{Output: "nslookup: can't find echo.default.svc.cluster.local: NXDOMAIN"}, // DNS blocked
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestNetworkIsolation(ctx, fc, DefaultConfig(), "kc-a.yaml", "kc-b.yaml")

	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s: %s", r.Verdict, r.Evidence)
	}
	if fc.ApplyCalls < 2 {
		t.Errorf("expected at least 2 Apply calls (echo + netprobe), got %d", fc.ApplyCalls)
	}
}

func TestRunTestNetworkIsolation_FailDirectIP(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "echo-server",
		PodIPResult:         "10.100.2.5",
		RunInPodResponses: []RunInPodResponse{
			{Output: "10.100.2.5 (10.100.2.5:8080) succeeded"}, // direct IP reachable!
			{Output: "nslookup: can't find echo: NXDOMAIN"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestNetworkIsolation(ctx, fc, DefaultConfig(), "kc-a.yaml", "kc-b.yaml")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when direct IP is reachable, got %s", r.Verdict)
	}
}

func TestRunTestNetworkIsolation_FailApply(t *testing.T) {
	fc := &FakeClient{
		ApplyErr: errApplyFailed,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestNetworkIsolation(ctx, fc, DefaultConfig(), "kc-a.yaml", "kc-b.yaml")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when Apply errors, got %s", r.Verdict)
	}
}

func TestRunTestNetworkIsolation_FailPodIPEmpty(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "echo-server",
		PodIPResult:         "", // empty IP
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestNetworkIsolation(ctx, fc, DefaultConfig(), "kc-a.yaml", "kc-b.yaml")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when pod IP is empty, got %s", r.Verdict)
	}
}

// errApplyFailed is a sentinel error used in tests.
var errApplyFailed = &testError{"apply failed"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

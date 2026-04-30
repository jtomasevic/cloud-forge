package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── Strategy 1: cilium-dbg policy trace ──────────────────────────────────────

func TestRunTestPolicyTrace_Pass(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: []string{"cilium-abc123"},
		RunInPodResponses: []RunInPodResponse{
			{Output: "Tracing From: [...]\nFinal verdict: DENIED\n", Err: nil},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s: %s", r.Verdict, r.Evidence)
	}
	if !hasSubstr(r.Evidence, "DENIED") {
		t.Errorf("evidence should contain DENIED, got: %s", r.Evidence)
	}
}

func TestRunTestPolicyTrace_Fail_TraceAllowed(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: []string{"cilium-abc123"},
		RunInPodResponses: []RunInPodResponse{
			{Output: "Tracing From: [...]\nFinal verdict: ALLOWED\n", Err: nil},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when trace shows ALLOWED, got %s", r.Verdict)
	}
}

func TestRunTestPolicyTrace_Fail_NoVerdictInOutput(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: []string{"cilium-abc123"},
		RunInPodResponses: []RunInPodResponse{
			// cilium-dbg ran cleanly (err==nil) but output contains no verdict.
			{Output: "some unexpected output", Err: nil},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when verdict not found in output, got %s", r.Verdict)
	}
}

// TestRunTestPolicyTrace_Fail_TraceNonZeroExit covers the case where
// cilium-dbg ran and exited non-zero for a reason unrelated to availability
// (e.g. network timeout talking to the agent socket).
func TestRunTestPolicyTrace_Fail_TraceNonZeroExit(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: []string{"cilium-abc123"},
		RunInPodResponses: []RunInPodResponse{
			{Output: "dial unix: connection refused", Err: errors.New("exit status 1")},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL for non-zero exit with non-availability error, got %s", r.Verdict)
	}
}

// ── Strategy 2: hubble observe fallback ──────────────────────────────────────

// TestRunTestPolicyTrace_HubbleFallback_Pass covers the primary failure mode
// observed in Cilium v1.17: `cilium policy trace` is not present, but
// `hubble observe` finds DROPPED flows confirming the deny rule is active.
func TestRunTestPolicyTrace_HubbleFallback_Pass(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: []string{"cilium-abc123"},
		RunInPodResponses: []RunInPodResponse{
			// cilium-dbg prints "Available Commands:" help page when "trace"
			// subcommand is not recognised (observed behaviour in Cilium v1.17).
			{
				Output: "Usage:  cilium-dbg policy [command]\n\nAvailable Commands:\n  selectors\n  validate\n",
				Err:    errors.New("exit status 1"),
			},
			// Hubble fallback: DROPPED flow found in ring buffer.
			{Output: "DROPPED flow from cilium-tenant-b to cilium-tenant-a port 8080", Err: nil},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS via hubble fallback, got %s: %s", r.Verdict, r.Evidence)
	}
	if !hasSubstr(r.Evidence, "DROPPED") {
		t.Errorf("evidence should contain DROPPED, got: %s", r.Evidence)
	}
}

func TestRunTestPolicyTrace_HubbleFallback_Fail(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: []string{"cilium-abc123"},
		RunInPodResponses: []RunInPodResponse{
			// cilium-dbg not available.
			{
				Output: "",
				Err:    errors.New("executable file not found in $PATH"),
			},
			// Hubble ran but shows no DROPPED flows — policy not enforcing.
			{Output: "FORWARDED flow from cilium-tenant-b to cilium-tenant-a port 8080", Err: nil},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when hubble shows no DROPPED flows, got %s", r.Verdict)
	}
}

func TestRunTestPolicyTrace_Skip_BothUnavailable(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: []string{"cilium-abc123"},
		RunInPodResponses: []RunInPodResponse{
			// cilium-dbg not available.
			{Output: "", Err: errors.New("executable file not found in $PATH")},
			// Hubble also not available.
			{Output: "", Err: errors.New("hubble: executable file not found in $PATH")},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictSkip {
		t.Errorf("expected SKIP when both strategies unavailable, got %s: %s", r.Verdict, r.Evidence)
	}
}

// ── Skip paths ────────────────────────────────────────────────────────────────

func TestRunTestPolicyTrace_Skip_NoCiliumPods(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: nil, // empty — no cilium agent pods found
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictSkip {
		t.Errorf("expected SKIP when no cilium pods, got %s", r.Verdict)
	}
}

func TestRunTestPolicyTrace_Skip_GetPodsError(t *testing.T) {
	fc := &FakeClient{
		PodsByLabelErr: errors.New("kube-system not found"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPolicyTrace(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictSkip {
		t.Errorf("expected SKIP when GetPodsByLabel errors, got %s", r.Verdict)
	}
}

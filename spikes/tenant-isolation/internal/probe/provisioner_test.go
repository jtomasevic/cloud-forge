package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── isTenantBScopedOut ────────────────────────────────────────────────────────

func TestIsTenantBScopedOut(t *testing.T) {
	cases := []struct {
		output string
		err    error
		want   bool
	}{
		{"", errors.New("no such pod"), true},           // RunInPod itself fails → isolated
		{"Error from server (NotFound): ...", nil, true}, // kubectl says not found
		{"forbidden", nil, true},
		{"unauthorized", nil, true},
		{"configmap \"cf-provisioner-test\" deleted", nil, false}, // delete succeeded → broken
		{"", nil, true}, // empty, ambiguous → treat as isolated
	}
	for _, c := range cases {
		got := isTenantBScopedOut(c.output, c.err)
		if got != c.want {
			t.Errorf("isTenantBScopedOut(%q, %v) = %v, want %v", c.output, c.err, got, c.want)
		}
	}
}

// ── formatScopeProof ──────────────────────────────────────────────────────────

func TestFormatScopeProof(t *testing.T) {
	// With error
	out := formatScopeProof("", errors.New("pod not found"))
	if !hasSubstr(out, "pod not found") {
		t.Errorf("formatScopeProof with error: %q missing error text", out)
	}

	// With long output — should be truncated
	long := "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789EXTRA"
	out = formatScopeProof(long, nil)
	if len(out) > 90 { // 80 chars + "…"
		t.Errorf("formatScopeProof did not truncate: len=%d", len(out))
	}

	// With empty output
	out = formatScopeProof("", nil)
	if !hasSubstr(out, "not accessible") {
		t.Errorf("formatScopeProof empty: %q", out)
	}
}

// ── RunTestProvisionerComm with FakeClient ────────────────────────────────────

func TestRunTestProvisionerComm_Pass(t *testing.T) {
	fc := &FakeClient{
		// RunInPod for the cross-tenant delete attempt returns "not found"
		RunInPodResponses: []RunInPodResponse{
			{Output: "Error from server (NotFound): configmap not found", Err: nil},
		},
		PodsByLabel: []string{}, // no pods, but Apply succeeding is the primary signal
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestProvisionerComm(ctx, fc, DefaultConfig(), "kc-a.yaml", "kc-b.yaml")
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s: %s", r.Verdict, r.Evidence)
	}
	if fc.ApplyCalls < 1 {
		t.Error("expected at least 1 Apply call")
	}
}

func TestRunTestProvisionerComm_FailApply(t *testing.T) {
	fc := &FakeClient{
		ApplyErr: errors.New("api server unreachable"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestProvisionerComm(ctx, fc, DefaultConfig(), "kc-a.yaml", "kc-b.yaml")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when Apply errors, got %s", r.Verdict)
	}
}

func TestRunTestProvisionerComm_FailIsolationBroken(t *testing.T) {
	fc := &FakeClient{
		// Apply succeeds, but the cross-tenant RunInPod shows "deleted" — isolation broken
		RunInPodResponses: []RunInPodResponse{
			{Output: `configmap "cf-provisioner-test" deleted`, Err: nil},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestProvisionerComm(ctx, fc, DefaultConfig(), "kc-a.yaml", "kc-b.yaml")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when tenant-B can delete tenant-A resource, got %s: %s", r.Verdict, r.Evidence)
	}
}

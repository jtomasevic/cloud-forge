package probe

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRunTestCrossNamespaceDeny_Pass(t *testing.T) {
	// Simulate: curl times out (connection blocked by Cilium DROP).
	fc := &FakeClient{
		WaitPodReadyPodName: "echo",
		PodIPResult:         "10.0.0.5",
		RunInPodResponses: []RunInPodResponse{
			{Output: "curl: (28) Operation timed out", Err: errors.New("exit status 28")},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCrossNamespaceDeny(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS (connection blocked), got %s: %s", r.Verdict, r.Evidence)
	}
	if !hasSubstr(r.Evidence, "blocked") {
		t.Errorf("evidence should mention 'blocked', got: %s", r.Evidence)
	}
}

func TestRunTestCrossNamespaceDeny_Fail_ConnectionAllowed(t *testing.T) {
	// Simulate: curl succeeds (CNP not enforcing).
	fc := &FakeClient{
		WaitPodReadyPodName: "echo",
		PodIPResult:         "10.0.0.5",
		RunInPodResponses: []RunInPodResponse{
			{Output: "pong", Err: nil},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCrossNamespaceDeny(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL (connection succeeded), got %s", r.Verdict)
	}
}

func TestRunTestCrossNamespaceDeny_Fail_ApplyError(t *testing.T) {
	fc := &FakeClient{
		ApplyErr: errors.New("api server unavailable"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCrossNamespaceDeny(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when Apply errors, got %s", r.Verdict)
	}
}

func TestRunTestCrossNamespaceDeny_Fail_WaitTimeout(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyErr: errors.New("timed out waiting for pod"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCrossNamespaceDeny(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when pod wait times out, got %s", r.Verdict)
	}
}

func TestRunTestCrossNamespaceDeny_Fail_NoPodIP(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "echo",
		PodIPErr:            errors.New("pod has no IP"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCrossNamespaceDeny(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when pod IP unavailable, got %s", r.Verdict)
	}
}

// TestRunTestCrossNamespaceDeny_Fail_SecondApplyError tests the CNP apply failure
// (Apply succeeds for namespace but fails for the CNP).
func TestRunTestCrossNamespaceDeny_Fail_SecondApplyError(t *testing.T) {
	// We need Apply to fail on the third call (CNP apply for tenant-A).
	// Use a custom client that counts Apply calls.
	fc := &applyCountFake{failOnCall: 3}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCrossNamespaceDeny(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when CNP apply fails, got %s", r.Verdict)
	}
}

// applyCountFake fails Apply on the Nth call.
type applyCountFake struct {
	FakeClient
	failOnCall int
	calls      int
}

func (f *applyCountFake) Apply(ctx context.Context, kc string, yaml []byte) error {
	f.calls++
	if f.calls == f.failOnCall {
		return errFake("apply failed on call " + fmt.Sprintf("%d", f.calls))
	}
	return nil
}

func (f *applyCountFake) WaitPodReady(ctx context.Context, kc, ns, sel string, t time.Duration) (string, time.Duration, error) {
	return f.FakeClient.WaitPodReady(ctx, kc, ns, sel, t)
}

func (f *applyCountFake) PodIP(ctx context.Context, kc, ns, sel string) (string, error) {
	return f.FakeClient.PodIP(ctx, kc, ns, sel)
}

func (f *applyCountFake) RunInPod(ctx context.Context, kc, ns, pod, ctr string, cmd []string) (string, error) {
	return f.FakeClient.RunInPod(ctx, kc, ns, pod, ctr, cmd)
}

func (f *applyCountFake) GetPodsByLabel(ctx context.Context, kc, ns, sel string) ([]string, error) {
	return f.FakeClient.GetPodsByLabel(ctx, kc, ns, sel)
}

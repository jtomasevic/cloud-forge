package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunTestIntraNamespaceAllow_Pass(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "allow-probe",
		PodIPResult:         "10.0.0.5",
		RunInPodResponses: []RunInPodResponse{
			{Output: "pong\n", Err: nil}, // curl succeeds
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestIntraNamespaceAllow(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s: %s", r.Verdict, r.Evidence)
	}
	if !hasSubstr(r.Evidence, "curl success") {
		t.Errorf("evidence should mention 'curl success', got: %s", r.Evidence)
	}
}

func TestRunTestIntraNamespaceAllow_Fail_CurlFails(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "allow-probe",
		PodIPResult:         "10.0.0.5",
		RunInPodResponses: []RunInPodResponse{
			{Output: "curl: (28) Operation timed out", Err: errors.New("exit status 28")},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestIntraNamespaceAllow(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when curl fails, got %s", r.Verdict)
	}
}

func TestRunTestIntraNamespaceAllow_Fail_WaitError(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyErr: errors.New("pod not ready"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestIntraNamespaceAllow(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when wait fails, got %s", r.Verdict)
	}
}

func TestRunTestIntraNamespaceAllow_Fail_NoPodIP(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "allow-probe",
		PodIPErr:            errors.New("no IP"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestIntraNamespaceAllow(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when echo IP not found, got %s", r.Verdict)
	}
}

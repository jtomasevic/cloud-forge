package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunTestPlatformIsolation_Pass(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "platform-svc",
		PodIPResult:         "10.1.0.10",
		RunInPodResponses: []RunInPodResponse{
			{Output: "curl: (28) Operation timed out", Err: errors.New("exit status 28")},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPlatformIsolation(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS (platform blocked), got %s: %s", r.Verdict, r.Evidence)
	}
	if !hasSubstr(r.Evidence, "blocked") {
		t.Errorf("evidence should mention 'blocked', got: %s", r.Evidence)
	}
}

func TestRunTestPlatformIsolation_Fail_ConnectionAllowed(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "platform-svc",
		PodIPResult:         "10.1.0.10",
		RunInPodResponses: []RunInPodResponse{
			{Output: "cf-system\n", Err: nil},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPlatformIsolation(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when platform namespace is reachable, got %s", r.Verdict)
	}
}

func TestRunTestPlatformIsolation_Fail_NoPodIP(t *testing.T) {
	fc := &FakeClient{
		WaitPodReadyPodName: "platform-svc",
		PodIPErr:            errors.New("pod not found"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPlatformIsolation(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when pod IP unavailable, got %s", r.Verdict)
	}
}

func TestRunTestPlatformIsolation_Fail_ApplyError(t *testing.T) {
	fc := &FakeClient{
		ApplyErr: errors.New("forbidden"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestPlatformIsolation(ctx, fc, DefaultConfig())
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when apply fails, got %s", r.Verdict)
	}
}

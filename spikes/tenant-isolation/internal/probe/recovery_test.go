package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── buildRecoveryEvidence ─────────────────────────────────────────────────────

func TestBuildRecoveryEvidence(t *testing.T) {
	ev := buildRecoveryEvidence("vcluster-0", 35*time.Second, 60*time.Second, true)
	for _, sub := range []string{"vcluster-0", "35s", "1m0s", "NATS=running"} {
		if !hasSubstr(ev, sub) {
			t.Errorf("buildRecoveryEvidence %q missing %q", ev, sub)
		}
	}
}

func TestBuildRecoveryEvidence_NATSNotRunning(t *testing.T) {
	ev := buildRecoveryEvidence("vcluster-0", 35*time.Second, 60*time.Second, false)
	if !hasSubstr(ev, "not-detected") {
		t.Errorf("expected 'not-detected' in evidence when NATS not running: %q", ev)
	}
}

// ── RunTestFailureRecovery with FakeClient ────────────────────────────────────

func TestRunTestFailureRecovery_Pass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RecoverySeconds = 60

	fc := &FakeClient{
		PodsByLabel:         []string{"vcluster-0", "nats-0"},
		WaitPodReadyPodName: "vcluster-0",
		WaitPodReadyElapsed: 30 * time.Second, // well within 60s
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestFailureRecovery(ctx, fc, cfg, "vcluster-tenant-a")
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s: %s", r.Verdict, r.Evidence)
	}
	if len(fc.DeletePodCalls) != 1 {
		t.Errorf("expected 1 DeletePod call, got %d", len(fc.DeletePodCalls))
	}
}

func TestRunTestFailureRecovery_FailNoVClusterPod(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel: nil, // no vCluster pod found
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestFailureRecovery(ctx, fc, DefaultConfig(), "vcluster-tenant-a")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when no vCluster pod found, got %s", r.Verdict)
	}
}

func TestRunTestFailureRecovery_FailDeleteError(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel:  []string{"vcluster-0"},
		DeletePodErr: errors.New("forbidden"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestFailureRecovery(ctx, fc, DefaultConfig(), "vcluster-tenant-a")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when delete errors, got %s", r.Verdict)
	}
}

func TestRunTestFailureRecovery_FailSlowRecovery(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RecoverySeconds = 60

	fc := &FakeClient{
		PodsByLabel:         []string{"vcluster-0"},
		WaitPodReadyPodName: "vcluster-0",
		WaitPodReadyElapsed: 90 * time.Second, // exceeds 60s threshold
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestFailureRecovery(ctx, fc, cfg, "vcluster-tenant-a")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when recovery time exceeds threshold, got %s", r.Verdict)
	}
}

func TestRunTestFailureRecovery_FailWaitError(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel:      []string{"vcluster-0"},
		WaitPodReadyErr:  errors.New("context deadline exceeded"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestFailureRecovery(ctx, fc, DefaultConfig(), "vcluster-tenant-a")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when WaitPodReady errors, got %s", r.Verdict)
	}
}

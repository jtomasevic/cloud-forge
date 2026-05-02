package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── percentileDuration ────────────────────────────────────────────────────────

func TestPercentileDuration(t *testing.T) {
	cases := []struct {
		ds   []time.Duration
		p    int
		want time.Duration
	}{
		{[]time.Duration{1, 2, 3, 4, 5}, 50, 3},
		{[]time.Duration{1, 2, 3, 4, 5}, 95, 5},
		{[]time.Duration{10, 20}, 50, 20}, {[]time.Duration{100}, 50, 100},
		{nil, 50, 0},
		{[]time.Duration{5, 1, 3, 2, 4}, 0, 1}, // unsorted input
	}
	for _, c := range cases {
		got := percentileDuration(c.ds, c.p)
		if got != c.want {
			t.Errorf("percentileDuration(%v, %d) = %d, want %d", c.ds, c.p, got, c.want)
		}
	}
}

// ── computeSpeedStats ─────────────────────────────────────────────────────────

func TestComputeSpeedStats_NoFailures(t *testing.T) {
	samples := []SpeedSample{
		{VClusterReadyElapsed: 30 * time.Second, NATSReadyElapsed: 60 * time.Second},
		{VClusterReadyElapsed: 45 * time.Second, NATSReadyElapsed: 90 * time.Second},
		{VClusterReadyElapsed: 40 * time.Second, NATSReadyElapsed: 70 * time.Second},
	}
	s := computeSpeedStats(samples)
	if s.Samples != 3 {
		t.Errorf("Samples = %d, want 3", s.Samples)
	}
	if s.FailedSamples != 0 {
		t.Errorf("FailedSamples = %d, want 0", s.FailedSamples)
	}
	if s.P50VCluster == 0 {
		t.Error("P50VCluster must not be zero")
	}
}

func TestComputeSpeedStats_WithFailures(t *testing.T) {
	samples := []SpeedSample{
		{VClusterReadyElapsed: 30 * time.Second, NATSReadyElapsed: 60 * time.Second},
		{Err: errors.New("timeout")},
		{VClusterReadyElapsed: 40 * time.Second, NATSReadyElapsed: 70 * time.Second},
	}
	s := computeSpeedStats(samples)
	if s.FailedSamples != 1 {
		t.Errorf("FailedSamples = %d, want 1", s.FailedSamples)
	}
	if s.Samples != 3 {
		t.Errorf("Samples = %d, want 3", s.Samples)
	}
}

func TestComputeSpeedStats_AllFailed(t *testing.T) {
	samples := []SpeedSample{
		{Err: errors.New("x")},
		{Err: errors.New("y")},
	}
	s := computeSpeedStats(samples)
	if s.FailedSamples != 2 {
		t.Errorf("FailedSamples = %d, want 2", s.FailedSamples)
	}
	if s.P50VCluster != 0 {
		t.Error("P50VCluster should be zero when all samples failed")
	}
}

// ── formatSpeedStats ──────────────────────────────────────────────────────────

func TestFormatSpeedStats(t *testing.T) {
	s := SpeedStats{
		Samples: 3, FailedSamples: 0,
		P50VCluster: 35 * time.Second, P95VCluster: 44 * time.Second,
		P50NATS: 65 * time.Second, P95NATS: 89 * time.Second,
	}
	out := formatSpeedStats(s)
	for _, sub := range []string{"samples=3", "failed=0", "vCluster", "NATS"} {
		if !hasSubstr(out, sub) {
			t.Errorf("formatSpeedStats output %q missing %q", out, sub)
		}
	}
}

// ── RunTestProvisioningSpeed with FakeClient ──────────────────────────────────

func TestRunTestProvisioningSpeed_Pass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SpeedSamples = 2
	cfg.VClusterReadySeconds = 90
	cfg.NATSReadySeconds = 180

	fc := &FakeClient{
		WaitPodReadyPodName: "nats-0",
		WaitPodReadyElapsed: 45 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestProvisioningSpeed(ctx, fc, cfg, "kc.yaml", 30*time.Second)
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s: %s", r.Verdict, r.Evidence)
	}
	if fc.ApplyCalls != 2 {
		t.Errorf("expected 2 Apply calls for 2 samples, got %d", fc.ApplyCalls)
	}
}

func TestRunTestProvisioningSpeed_FailVClusterThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SpeedSamples = 1
	cfg.VClusterReadySeconds = 10 // very tight threshold

	fc := &FakeClient{
		WaitPodReadyPodName: "nats-0",
		WaitPodReadyElapsed: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// vClusterReadyElapsed=120s >> 10s threshold
	r := RunTestProvisioningSpeed(ctx, fc, cfg, "kc.yaml", 120*time.Second)
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when vCluster p95 exceeds threshold, got %s", r.Verdict)
	}
}

func TestRunTestProvisioningSpeed_FailApply(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SpeedSamples = 1

	fc := &FakeClient{ApplyErr: errors.New("apply failed")}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestProvisioningSpeed(ctx, fc, cfg, "kc.yaml", 30*time.Second)
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when apply errors, got %s", r.Verdict)
	}
}

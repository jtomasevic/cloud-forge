package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── CountByVerdict ────────────────────────────────────────────────────────────

func TestCountByVerdict(t *testing.T) {
	results := []TestResult{
		{Verdict: VerdictPass},
		{Verdict: VerdictPass},
		{Verdict: VerdictFail},
		{Verdict: VerdictSkip},
	}
	counts := CountByVerdict(results)
	if counts[VerdictPass] != 2 {
		t.Errorf("PASS count = %d, want 2", counts[VerdictPass])
	}
	if counts[VerdictFail] != 1 {
		t.Errorf("FAIL count = %d, want 1", counts[VerdictFail])
	}
	if counts[VerdictSkip] != 1 {
		t.Errorf("SKIP count = %d, want 1", counts[VerdictSkip])
	}
}

func TestCountByVerdict_Empty(t *testing.T) {
	counts := CountByVerdict(nil)
	for _, v := range []Verdict{VerdictPass, VerdictFail, VerdictSkip} {
		if counts[v] != 0 {
			t.Errorf("count[%s] = %d, want 0", v, counts[v])
		}
	}
}

// ── AllPassed ─────────────────────────────────────────────────────────────────

func TestAllPassed(t *testing.T) {
	cases := []struct {
		results []TestResult
		want    bool
	}{
		{[]TestResult{{Verdict: VerdictPass}, {Verdict: VerdictSkip}}, true},
		{[]TestResult{{Verdict: VerdictPass}, {Verdict: VerdictFail}}, false},
		{[]TestResult{{Verdict: VerdictFail}}, false},
		{nil, true},
	}
	for _, c := range cases {
		got := AllPassed(c.results)
		if got != c.want {
			t.Errorf("AllPassed(%v) = %v, want %v", c.results, got, c.want)
		}
	}
}

// ── OverallVerdict ────────────────────────────────────────────────────────────

func TestOverallVerdict(t *testing.T) {
	cases := []struct {
		results []TestResult
		want    Verdict
	}{
		{[]TestResult{{Verdict: VerdictPass}}, VerdictPass},
		{[]TestResult{{Verdict: VerdictSkip}}, VerdictSkip},
		{[]TestResult{{Verdict: VerdictPass}, {Verdict: VerdictFail}}, VerdictFail},
		{[]TestResult{{Verdict: VerdictPass}, {Verdict: VerdictSkip}}, VerdictPass},
		{nil, VerdictSkip},
	}
	for _, c := range cases {
		got := OverallVerdict(c.results)
		if got != c.want {
			t.Errorf("OverallVerdict(%v) = %s, want %s", c.results, got, c.want)
		}
	}
}

// ── RunAll (integration with FakeClient) ─────────────────────────────────────

func TestRunAll_AllPass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SpeedSamples = 1

	fc := &FakeClient{
		// Test 1 (isolation)
		WaitPodReadyPodName: "echo-server",
		PodIPResult:         "10.100.2.5",
		RunInPodResponses: []RunInPodResponse{
			// Test 1: nc blocked
			{Output: "connection refused"},
			// Test 1: DNS blocked
			{Output: "nslookup: can't find echo: NXDOMAIN"},
			// Test 3 (provisioner): cross-tenant delete attempt → not found
			{Output: "Error from server (NotFound): not found"},
		},
		// Test 4 (overhead): valid resource snapshot
		PodResources: []PodResource{
			{Name: "vcluster-0", CPUMilli: 25, MemMB: 128},
		},
		// Test 5 (Cilium): no Cilium → SKIP
		PodsByLabel: nil,
		// Test 6 (recovery)
		WaitPodReadyElapsed: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := RunInput{
		TenantAKubeconfig:           "kc-a.yaml",
		TenantBKubeconfig:           "kc-b.yaml",
		TenantANamespace:            "vcluster-tenant-a",
		TenantBNamespace:            "vcluster-tenant-b",
		TenantAVClusterReadyElapsed: 40 * time.Second,
	}

	results := RunAll(ctx, fc, cfg, input)

	if len(results) != len(allTests) {
		t.Fatalf("expected %d results, got %d", len(allTests), len(results))
	}

	// Cilium test should be SKIPped (no cilium pods)
	for _, r := range results {
		if r.Name == TestCiliumEnforcement && r.Verdict != VerdictSkip {
			t.Errorf("expected Cilium test to be SKIP, got %s", r.Verdict)
		}
		if r.Verdict == "" {
			t.Errorf("test %q has empty verdict", r.Name)
		}
	}
}

func TestRunAll_UnknownTestName(t *testing.T) {
	cfg := DefaultConfig()
	fc := &FakeClient{
		WaitPodReadyPodName: "pod",
		PodIPResult:         "1.2.3.4",
		RunInPodResponses:   []RunInPodResponse{{Output: "connection refused"}, {Output: "NXDOMAIN"}},
		PodsByLabel:         []string{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := RunInput{TenantAKubeconfig: "kc.yaml", TenantBKubeconfig: "kc.yaml"}

	// runOne with unknown name
	r := runOne(ctx, fc, cfg, input, TestName("does_not_exist"))
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL for unknown test name, got %s", r.Verdict)
	}
}

// ── Ensure FakeClient satisfies interface for error path coverage ─────────────

func TestFakeClient_AllMethods(t *testing.T) {
	fc := &FakeClient{
		RunInPodResponses:   []RunInPodResponse{{Output: "ok", Err: nil}},
		WaitPodReadyPodName: "pod-1",
		WaitPodReadyElapsed: time.Second,
		PodResources:        []PodResource{{Name: "p", CPUMilli: 10, MemMB: 50}},
		PodsByLabel:         []string{"pod-1"},
		PodIPResult:         "1.2.3.4",
	}

	ctx := context.Background()

	if _, err := fc.RunInPod(ctx, "", "default", "pod", "", []string{"echo"}); err != nil {
		t.Errorf("RunInPod: %v", err)
	}
	if err := fc.Apply(ctx, "", []byte("yaml")); err != nil {
		t.Errorf("Apply: %v", err)
	}
	if _, _, err := fc.WaitPodReady(ctx, "", "default", "app=x", time.Second); err != nil {
		t.Errorf("WaitPodReady: %v", err)
	}
	if err := fc.DeletePod(ctx, "", "default", "pod-1"); err != nil {
		t.Errorf("DeletePod: %v", err)
	}
	if _, err := fc.GetPodResources(ctx, "", "default", "app=x"); err != nil {
		t.Errorf("GetPodResources: %v", err)
	}
	if _, err := fc.GetPodsByLabel(ctx, "", "default", "app=x"); err != nil {
		t.Errorf("GetPodsByLabel: %v", err)
	}
	if _, err := fc.PodIP(ctx, "", "default", "app=x"); err != nil {
		t.Errorf("PodIP: %v", err)
	}
}

func TestFakeClient_RunInPod_NoResponseConfigured(t *testing.T) {
	fc := &FakeClient{} // no responses configured
	ctx := context.Background()
	_, err := fc.RunInPod(ctx, "", "default", "pod", "", []string{"echo"})
	if err == nil {
		t.Error("expected error when no RunInPod responses configured")
	}
}

func TestFakeClient_RunInPod_ErrorPropagated(t *testing.T) {
	fc := &FakeClient{
		RunInPodResponses: []RunInPodResponse{{Err: errors.New("exec failed")}},
	}
	ctx := context.Background()
	_, err := fc.RunInPod(ctx, "", "default", "pod", "", []string{"cmd"})
	if err == nil {
		t.Error("expected error to be propagated")
	}
}

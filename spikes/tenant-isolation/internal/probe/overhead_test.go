package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── p50Int64 ──────────────────────────────────────────────────────────────────

func TestP50Int64_Odd(t *testing.T) {
	// [1, 2, 3] → median = 2
	got := p50Int64([]int64{3, 1, 2})
	if got != 2 {
		t.Errorf("p50Int64 = %d, want 2", got)
	}
}

func TestP50Int64_Even(t *testing.T) {
	// [10, 20] sorted → upper-median = 20
	got := p50Int64([]int64{20, 10})
	if got != 20 {
		t.Errorf("p50Int64([20,10]) = %d, want 20 (upper-median)", got)
	}
}

func TestP50Int64_Single(t *testing.T) {
	got := p50Int64([]int64{42})
	if got != 42 {
		t.Errorf("p50Int64([42]) = %d, want 42", got)
	}
}

func TestP50Int64_Empty(t *testing.T) {
	got := p50Int64(nil)
	if got != 0 {
		t.Errorf("p50Int64(nil) = %d, want 0", got)
	}
}

func TestP50Int64_Identical(t *testing.T) {
	// Three identical values → p50 is that value.
	got := p50Int64([]int64{100, 100, 100})
	if got != 100 {
		t.Errorf("p50Int64([100,100,100]) = %d, want 100", got)
	}
}

// ── maxInt64 ──────────────────────────────────────────────────────────────────

func TestMaxInt64_Basic(t *testing.T) {
	got := maxInt64([]int64{5, 2, 9, 1})
	if got != 9 {
		t.Errorf("maxInt64 = %d, want 9", got)
	}
}

func TestMaxInt64_Single(t *testing.T) {
	got := maxInt64([]int64{7})
	if got != 7 {
		t.Errorf("maxInt64([7]) = %d, want 7", got)
	}
}

func TestMaxInt64_Empty(t *testing.T) {
	got := maxInt64(nil)
	if got != 0 {
		t.Errorf("maxInt64(nil) = %d, want 0", got)
	}
}

// ── extractOverhead ───────────────────────────────────────────────────────────

func TestExtractOverhead(t *testing.T) {
	samples := []overheadSample{
		{cpuA: 10, memA: 100, cpuB: 20, memB: 200},
		{cpuA: 30, memA: 300, cpuB: 40, memB: 400},
	}
	cpuAs := extractOverhead(samples, func(s overheadSample) int64 { return s.cpuA })
	if len(cpuAs) != 2 || cpuAs[0] != 10 || cpuAs[1] != 30 {
		t.Errorf("extractOverhead(cpuA) = %v, want [10 30]", cpuAs)
	}
}

// ── SizingFormula ─────────────────────────────────────────────────────────────

func TestSizingFormula(t *testing.T) {
	rows := SizingFormula(50, 256, []int{10, 50, 200})
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// 10 tenants: 50m * 10 = 500m CPU, 256 * 10 / 1024 = 2.5 GiB
	if rows[0].Tenants != 10 {
		t.Errorf("row[0].Tenants = %d, want 10", rows[0].Tenants)
	}
	if rows[0].TotalCPUM != 500 {
		t.Errorf("row[0].TotalCPUM = %d, want 500", rows[0].TotalCPUM)
	}
	if rows[0].TotalMemGB < 2.4 || rows[0].TotalMemGB > 2.6 {
		t.Errorf("row[0].TotalMemGB = %.2f, want ≈2.5", rows[0].TotalMemGB)
	}

	// 200 tenants: 256 * 200 / 1024 = 50 GiB
	if rows[2].TotalMemGB < 49.9 || rows[2].TotalMemGB > 50.1 {
		t.Errorf("row[2].TotalMemGB = %.2f, want ≈50.0", rows[2].TotalMemGB)
	}
}

func TestSizingFormula_Empty(t *testing.T) {
	rows := SizingFormula(50, 256, nil)
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for nil tenantCounts, got %d", len(rows))
	}
}

// ── buildOverheadEvidence ─────────────────────────────────────────────────────

func TestBuildOverheadEvidence(t *testing.T) {
	ev := buildOverheadEvidence(25, 128, 30, 140, 27, 134)
	for _, sub := range []string{"25m", "128", "30m", "140", "avg:"} {
		if !hasSubstr(ev, sub) {
			t.Errorf("buildOverheadEvidence %q missing substring %q", ev, sub)
		}
	}
}

// ── RunTestResourceOverhead with FakeClient ───────────────────────────────────

func TestRunTestResourceOverhead_Pass(t *testing.T) {
	fc := &FakeClient{
		// FakeClient returns the same slice for every GetPodResources call.
		// With OverheadSamples=3 and OverheadSampleInterval=0, p50 of three identical
		// values equals that value — result is deterministic and the test is fast.
		PodResources: []PodResource{
			{Name: "vcluster-tenant-a-0", CPUMilli: 25, MemMB: 128},
			{Name: "coredns-abc", CPUMilli: 5, MemMB: 32},
		},
	}

	cfg := DefaultConfig()
	// Stabilization wait and sample interval kept at 0 so the test runs instantly.
	cfg.MaxIdleRAMMB = 512    // 160 MiB total → PASS (well under 512 threshold)
	cfg.MaxIdleCPUMilli = 150 // 30m total → PASS

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestResourceOverhead(ctx, fc, cfg, "vcluster-tenant-a", "vcluster-tenant-b")
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s: %s", r.Verdict, r.Evidence)
	}
	// Verify that samples_count metric is present and reflects OverheadSamples.
	if got := r.MetricOrEmpty("samples_count"); got != "3" {
		t.Errorf("samples_count = %q, want %q", got, "3")
	}
}

func TestRunTestResourceOverhead_FailHighRAM(t *testing.T) {
	fc := &FakeClient{
		PodResources: []PodResource{
			{Name: "vcluster-0", CPUMilli: 10, MemMB: 600}, // 600 MiB > 512 threshold
		},
	}

	cfg := DefaultConfig()
	cfg.MaxIdleRAMMB = 512

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestResourceOverhead(ctx, fc, cfg, "ns-a", "ns-b")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when RAM exceeds threshold, got %s", r.Verdict)
	}
}

func TestRunTestResourceOverhead_FailHighCPU(t *testing.T) {
	fc := &FakeClient{
		// 200m CPU per pod → total per tenant = 200m → avg = 200m > 150m threshold.
		PodResources: []PodResource{
			{Name: "vcluster-0", CPUMilli: 200, MemMB: 100},
		},
	}

	cfg := DefaultConfig() // MaxIdleCPUMilli = 150 by default
	cfg.MaxIdleRAMMB = 512 // keep RAM passing so we isolate the CPU failure

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestResourceOverhead(ctx, fc, cfg, "ns-a", "ns-b")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when CPU exceeds threshold, got %s: %s", r.Verdict, r.Evidence)
	}
	if !hasSubstr(r.Evidence, "p50") {
		t.Errorf("evidence should mention p50, got: %s", r.Evidence)
	}
}

func TestRunTestResourceOverhead_SkipWhenEmpty(t *testing.T) {
	fc := &FakeClient{
		PodResources: nil, // empty → metrics-server not available
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestResourceOverhead(ctx, fc, DefaultConfig(), "ns-a", "ns-b")
	if r.Verdict != VerdictSkip {
		t.Errorf("expected SKIP when no metrics available, got %s", r.Verdict)
	}
}

func TestRunTestResourceOverhead_FailPodResourcesError(t *testing.T) {
	fc := &FakeClient{
		PodResourcesErr: errors.New("metrics-server error"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestResourceOverhead(ctx, fc, DefaultConfig(), "ns-a", "ns-b")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when GetPodResources errors, got %s", r.Verdict)
	}
}

package probe

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 4: Resource overhead per idle vCluster
// ──────────────────────────────────────────────────────────────────────────────
//
// An idle vCluster (API server + etcd + CoreDNS, no tenant services running)
// must consume < 150 m CPU and < 512 MiB RAM on the host cluster.
// This determines the minimum host-cluster sizing formula for N tenants.
//
// Measurement strategy:
//   1. Wait cfg.OverheadStabilizationWait for k3s/etcd startup CPU burst to subside.
//   2. Collect cfg.OverheadSamples snapshots at cfg.OverheadSampleInterval intervals.
//   3. Report p50 of each metric across samples (resilient to transient bursts).
//   4. Also report the peak sample so spikes are visible in the findings.
//
// vCluster control-plane pods reside in the host cluster namespace whose name
// matches the vCluster name (e.g., "vcluster-tenant-a"). The selector
// "app=vcluster" matches the StatefulSet pod.

// vClusterControlPlanePodSelector is the label selector used to find all
// vCluster control-plane pods in a given host namespace.
const vClusterControlPlanePodSelector = "app=vcluster"

// overheadSample holds one resource snapshot for both tenant vClusters.
type overheadSample struct {
	cpuA, memA int64
	cpuB, memB int64
}

// RunTestResourceOverhead measures CPU and RAM consumed by two idle vClusters.
//
// It waits cfg.OverheadStabilizationWait before sampling — this lets the
// embedded k3s/etcd exit their initialization CPU burst and reach steady state.
// It then collects cfg.OverheadSamples snapshots at cfg.OverheadSampleInterval
// intervals and reports the p50 value for each metric.
//
// Both peak (worst-case) and p50 (steady-state) CPU values are emitted as
// metrics so the results table and FINDINGS.md can tell the full story.
//
// If metrics-server is unavailable the test is SKIPped with a clear message.
func RunTestResourceOverhead(
	ctx context.Context,
	c KubectlClient,
	cfg Config,
	tenantANamespace, tenantBNamespace string,
) TestResult {
	start := time.Now()
	metrics := map[string]string{}

	// ── Stabilization wait ────────────────────────────────────────────────────
	// vCluster's embedded k3s and etcd consume high CPU for ~2-3 minutes while
	// initializing etcd WAL, loading k3s state, and starting CoreDNS. Measuring
	// before this burst ends yields inflated CPU numbers that do not represent
	// production idle load.
	if cfg.OverheadStabilizationWait > 0 {
		slog.Info("waiting for vCluster CPU stabilization",
			"wait", cfg.OverheadStabilizationWait,
			"reason", "k3s/etcd startup CPU burst; measuring before it settles inflates numbers")
		timer := time.NewTimer(cfg.OverheadStabilizationWait)
		defer timer.Stop()
		select {
		case <-timer.C:
			slog.Info("stabilization wait complete — beginning overhead sampling")
		case <-ctx.Done():
			return failResult(TestResourceOverhead,
				"context cancelled during stabilization wait", start, metrics)
		}
	}

	// ── Multi-sample collection ───────────────────────────────────────────────
	// Taking multiple snapshots and deriving the p50 makes the measurement
	// resilient to one-off spikes from background reconciliation loops
	// (e.g., k3s garbage collection, etcd compaction).
	n := cfg.OverheadSamples
	if n < 1 {
		n = 1
	}
	samples := make([]overheadSample, 0, n)

outer:
	for i := 0; i < n; i++ {
		// Pause between samples (skip before the first sample).
		if i > 0 && cfg.OverheadSampleInterval > 0 {
			slog.Info("waiting between overhead samples",
				"interval", cfg.OverheadSampleInterval,
				"sample_next", i+1,
				"sample_total", n)
			t := time.NewTimer(cfg.OverheadSampleInterval)
			select {
			case <-t.C:
				t.Stop()
			case <-ctx.Done():
				t.Stop()
				slog.Warn("context cancelled mid-sampling; using samples collected so far",
					"samples_collected", len(samples))
				break outer
			}
		}

		slog.Info("collecting overhead sample", "sample", i+1, "of", n)

		podsA, err := c.GetPodResources(ctx, "" /* host kubeconfig */, tenantANamespace, vClusterControlPlanePodSelector)
		if err != nil {
			return failResult(TestResourceOverhead,
				fmt.Sprintf("get pod resources sample %d (tenant-A namespace=%s): %v", i+1, tenantANamespace, err),
				start, metrics)
		}
		podsB, err := c.GetPodResources(ctx, "", tenantBNamespace, vClusterControlPlanePodSelector)
		if err != nil {
			return failResult(TestResourceOverhead,
				fmt.Sprintf("get pod resources sample %d (tenant-B namespace=%s): %v", i+1, tenantBNamespace, err),
				start, metrics)
		}

		// Empty on the first sample means metrics-server is not installed.
		if len(podsA) == 0 && len(podsB) == 0 {
			if i == 0 {
				return skipResult(TestResourceOverhead,
					"metrics-server not available — install metrics-server to enable this test",
					start)
			}
			slog.Warn("metrics-server returned empty results mid-sampling; using earlier samples",
				"samples_usable", len(samples))
			break outer
		}

		samples = append(samples, overheadSample{
			cpuA: TotalCPUMilli(podsA),
			memA: TotalMemMB(podsA),
			cpuB: TotalCPUMilli(podsB),
			memB: TotalMemMB(podsB),
		})
	}

	if len(samples) == 0 {
		return skipResult(TestResourceOverhead,
			"metrics-server not available — install metrics-server to enable this test",
			start)
	}

	// ── Derive p50 and peak values ────────────────────────────────────────────
	// p50 is the authoritative production-sizing number.
	// Peak is retained for transparency (visible in findings and verbose output).
	cpuA := p50Int64(extractOverhead(samples, func(s overheadSample) int64 { return s.cpuA }))
	memA := p50Int64(extractOverhead(samples, func(s overheadSample) int64 { return s.memA }))
	cpuB := p50Int64(extractOverhead(samples, func(s overheadSample) int64 { return s.cpuB }))
	memB := p50Int64(extractOverhead(samples, func(s overheadSample) int64 { return s.memB }))

	// Peak CPU across both tenants — useful for documenting startup burst magnitude.
	cpuAPeak := maxInt64(extractOverhead(samples, func(s overheadSample) int64 { return s.cpuA }))
	cpuBPeak := maxInt64(extractOverhead(samples, func(s overheadSample) int64 { return s.cpuB }))

	// Average per-vCluster p50 overhead: used for sizing formula and threshold check.
	avgCPU := (cpuA + cpuB) / 2
	avgMem := (memA + memB) / 2

	metrics["samples_count"] = fmt.Sprintf("%d", len(samples))
	metrics["tenant_a_cpu_p50_m"] = fmt.Sprintf("%dm", cpuA)
	metrics["tenant_a_mem_p50_mb"] = fmt.Sprintf("%dMi", memA)
	metrics["tenant_a_cpu_peak_m"] = fmt.Sprintf("%dm", cpuAPeak)
	metrics["tenant_b_cpu_p50_m"] = fmt.Sprintf("%dm", cpuB)
	metrics["tenant_b_mem_p50_mb"] = fmt.Sprintf("%dMi", memB)
	metrics["tenant_b_cpu_peak_m"] = fmt.Sprintf("%dm", cpuBPeak)

	// Legacy keys kept for backward-compat with PrintSizingFormula in main.go.
	metrics["avg_cpu_m"] = fmt.Sprintf("%dm", avgCPU)
	metrics["avg_mem_mb"] = fmt.Sprintf("%dMi", avgMem)

	// Sizing formula: how many GiB of RAM does N × idle vCluster control planes require?
	metrics["sizing_10_tenants_ram_gb"] = fmt.Sprintf("%.1fGi", float64(avgMem*10)/1024)
	metrics["sizing_50_tenants_ram_gb"] = fmt.Sprintf("%.1fGi", float64(avgMem*50)/1024)
	metrics["sizing_200_tenants_ram_gb"] = fmt.Sprintf("%.1fGi", float64(avgMem*200)/1024)

	// ── Threshold evaluation (on p50 values) ─────────────────────────────────
	failures := []string{}
	if avgMem > cfg.MaxIdleRAMMB {
		failures = append(failures,
			fmt.Sprintf("idle RAM p50 %dMi > threshold %dMi", avgMem, cfg.MaxIdleRAMMB))
	}
	if avgCPU > cfg.MaxIdleCPUMilli {
		failures = append(failures,
			fmt.Sprintf("idle CPU p50 %dm > threshold %dm", avgCPU, cfg.MaxIdleCPUMilli))
	}

	evidence := buildOverheadEvidence(cpuA, memA, cpuB, memB, avgCPU, avgMem)
	evidence += fmt.Sprintf(" | samples=%d", len(samples))
	if cpuAPeak > cpuA || cpuBPeak > cpuB {
		// Include peak only when it differs from p50 (highlights any residual burst).
		evidence += fmt.Sprintf(" | cpu_peak_a=%dm cpu_peak_b=%dm", cpuAPeak, cpuBPeak)
	}

	if len(failures) > 0 {
		return failResult(TestResourceOverhead,
			evidence+" | FAIL: "+strings.Join(failures, "; "),
			start, metrics)
	}
	return passResult(TestResourceOverhead, evidence, start, metrics)
}

// buildOverheadEvidence formats the per-tenant and average resource summary.
// Values are p50 across the collected samples.
func buildOverheadEvidence(cpuA, memA, cpuB, memB, avgCPU, avgMem int64) string {
	return fmt.Sprintf(
		"tenant-A: %dm/%dMi | tenant-B: %dm/%dMi | avg: %dm/%dMi",
		cpuA, memA, cpuB, memB, avgCPU, avgMem,
	)
}

// ── Overhead helpers ──────────────────────────────────────────────────────────

// extractOverhead extracts a single int64 field from each sample.
func extractOverhead(samples []overheadSample, fn func(overheadSample) int64) []int64 {
	out := make([]int64, len(samples))
	for i, s := range samples {
		out[i] = fn(s)
	}
	return out
}

// p50Int64 returns the 50th-percentile (median) value from a slice of int64.
// For an even-length slice it returns the upper-median element.
func p50Int64(vals []int64) int64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]int64, len(vals))
	copy(sorted, vals)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

// maxInt64 returns the maximum value from a slice of int64.
func maxInt64(vals []int64) int64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// ── Sizing formula ────────────────────────────────────────────────────────────

// SizingFormula computes RAM and CPU requirements for N idle vClusters.
// Uses the p50 per-vCluster overhead as the per-unit cost.
// This produces the host-cluster sizing table required by §11.6.
func SizingFormula(avgCPUMilli, avgMemMB int64, tenantCounts []int) []SizingRow {
	rows := make([]SizingRow, len(tenantCounts))
	for i, n := range tenantCounts {
		rows[i] = SizingRow{
			Tenants:    n,
			TotalCPUM:  avgCPUMilli * int64(n),
			TotalMemGB: float64(avgMemMB*int64(n)) / 1024,
		}
	}
	return rows
}

// SizingRow holds the resource requirements for a given tenant count.
type SizingRow struct {
	Tenants    int
	TotalCPUM  int64   // total CPU in millicores for all vCluster control planes
	TotalMemGB float64 // total RAM in GiB for all vCluster control planes
}

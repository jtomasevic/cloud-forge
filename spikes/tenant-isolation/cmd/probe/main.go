// Command probe is the CLI entry point for the vCluster tenant isolation spike.
//
// It performs the full validation sequence:
//   1. Checks required tools (kubectl, vcluster, helm, k3d).
//   2. Creates two tenant vClusters (tenant-a and tenant-b) on the existing k3d cluster.
//   3. Runs all six isolation tests defined in §11.3 of docs/3-Introduce-CF-VPC.md.
//   4. Prints a formatted results table to stdout.
//   5. Exits with code 0 if all tests pass/skip, code 1 if any test fails.
//
// Usage:
//
//	probe [flags]
//
// Flags:
//
//	-kubeconfig        Path to host-cluster kubeconfig (default: KUBECONFIG env or ~/.kube/config)
//	-tenant-a          Name for the first vCluster (default: tenant-a)
//	-tenant-b          Name for the second vCluster (default: tenant-b)
//	-kubeconfig-dir    Directory for generated per-vCluster kubeconfigs (default: ./kubeconfigs)
//	-speed-samples     Number of NATS provisioning cycles to measure (default: 3)
//	-overhead-wait     Stabilization wait before overhead measurement (default: 0; production: 2m)
//	-overhead-interval Interval between overhead samples (default: 0; production: 30s)
//	-overhead-samples  Number of overhead CPU/RAM snapshots (default: 3)
//	-skip-create       Skip vCluster creation (use existing clusters)
//	-verbose           Print per-test metrics after the results table
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cloud-forge/spikes/tenant-isolation/internal/cluster"
	"github.com/cloud-forge/spikes/tenant-isolation/internal/probe"
)

func main() {
	os.Exit(run())
}

func run() int {
	// ── Flags ─────────────────────────────────────────────────────────────
	kubeconfigFlag  := flag.String("kubeconfig", os.Getenv("KUBECONFIG"), "host-cluster kubeconfig path")
	tenantA         := flag.String("tenant-a", "tenant-a", "name of the first vCluster (tenant-A)")
	tenantB         := flag.String("tenant-b", "tenant-b", "name of the second vCluster (tenant-B)")
	kcDir           := flag.String("kubeconfig-dir", "kubeconfigs", "directory for per-vCluster kubeconfigs")
	speedSamples    := flag.Int("speed-samples", 3, "number of NATS provisioning cycles")
	overheadWait    := flag.Duration("overhead-wait", 0, "stabilization wait before overhead measurement (e.g. 2m); 0 = disabled")
	overheadInterval := flag.Duration("overhead-interval", 0, "interval between overhead CPU/RAM samples (e.g. 30s); 0 = no pause")
	overheadSamples := flag.Int("overhead-samples", 3, "number of overhead CPU/RAM snapshots to collect")
	skipCreate      := flag.Bool("skip-create", false, "skip vCluster creation (use existing)")
	verbose         := flag.Bool("verbose", false, "print per-test metrics and sizing table after results")
	flag.Parse()

	// ── Logger ────────────────────────────────────────────────────────────
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// ── Prerequisite check ─────────────────────────────────────────────────
	slog.Info("checking prerequisites")
	checkResults, err := cluster.CheckPrerequisites(ctx)
	for _, cr := range checkResults {
		status := "OK"
		if !cr.Found {
			status = "MISSING"
		}
		slog.Info("tool check", "tool", cr.Tool.Binary, "status", status, "version", cr.Version)
	}
	if err != nil {
		slog.Error("prerequisite check failed", "error", err)
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// ── Build config ────────────────────────────────────────────────────────
	cfg := probe.DefaultConfig()
	cfg.KubeconfigPath = *kubeconfigFlag
	cfg.TenantAName = *tenantA
	cfg.TenantBName = *tenantB
	cfg.KubeconfigDir = *kcDir
	cfg.SpeedSamples = *speedSamples

	// Overhead measurement tuning — applied from CLI flags so the Makefile
	// can set production-grade values (2m wait, 30s interval) while unit tests
	// keep the defaults (0/0) and run instantly.
	cfg.OverheadStabilizationWait = *overheadWait
	cfg.OverheadSampleInterval = *overheadInterval
	cfg.OverheadSamples = *overheadSamples

	tenantAKubeconfig := cluster.KubeconfigPath(*kcDir, *tenantA)
	tenantBKubeconfig := cluster.KubeconfigPath(*kcDir, *tenantB)
	tenantANamespace := "vcluster-" + *tenantA
	tenantBNamespace := "vcluster-" + *tenantB

	// ── vCluster creation ────────────────────────────────────────────────────
	var vClusterAElapsed time.Duration
	if !*skipCreate {
		slog.Info("creating vCluster for tenant-A", "name", *tenantA, "namespace", tenantANamespace)
		elapsed, err := cluster.CreateVCluster(ctx, *tenantA, tenantANamespace, tenantAKubeconfig,
			time.Duration(cfg.VClusterReadySeconds*2*float64(time.Second)))
		if err != nil {
			slog.Error("failed to create tenant-A vCluster", "error", err)
			return 1
		}
		vClusterAElapsed = elapsed
		slog.Info("tenant-A vCluster ready", "elapsed", elapsed.Round(time.Second))

		slog.Info("creating vCluster for tenant-B", "name", *tenantB, "namespace", tenantBNamespace)
		if _, err := cluster.CreateVCluster(ctx, *tenantB, tenantBNamespace, tenantBKubeconfig,
			time.Duration(cfg.VClusterReadySeconds*2*float64(time.Second))); err != nil {
			slog.Error("failed to create tenant-B vCluster", "error", err)
			return 1
		}
		slog.Info("tenant-B vCluster ready")
	} else {
		slog.Info("skipping vCluster creation (-skip-create)", "tenant-a", tenantAKubeconfig, "tenant-b", tenantBKubeconfig)
	}

	// ── Run all six tests ───────────────────────────────────────────────────
	slog.Info("starting spike tests", "tests", len(probe.AllTests()))
	client := cluster.NewRealClient()

	input := probe.RunInput{
		TenantAKubeconfig:           tenantAKubeconfig,
		TenantBKubeconfig:           tenantBKubeconfig,
		TenantANamespace:            tenantANamespace,
		TenantBNamespace:            tenantBNamespace,
		TenantAVClusterReadyElapsed: vClusterAElapsed,
	}

	results := probe.RunAll(ctx, client, cfg, input)

	// ── Print results ────────────────────────────────────────────────────────
	probe.PrintResults(os.Stdout, results)

	if *verbose {
		probe.PrintMetrics(os.Stdout, results)

		// Print sizing formula for the overhead test regardless of PASS/FAIL.
		// A FAIL just means the threshold was exceeded, but the measured values
		// are still valid for infrastructure sizing decisions.
		for _, r := range results {
			if r.Name == probe.TestResourceOverhead && r.Verdict != probe.VerdictSkip {
				avgCPU := probe.ParseCPUMillicores(r.MetricOrEmpty("avg_cpu_m"))
				avgMem := probe.ParseMemMB(r.MetricOrEmpty("avg_mem_mb"))
				if avgCPU > 0 || avgMem > 0 {
					probe.PrintSizingFormula(os.Stdout, avgCPU, avgMem)
				}
				break
			}
		}
	}

	// ── Exit code ─────────────────────────────────────────────────────────────
	if !probe.AllPassed(results) {
		return 1
	}
	return 0
}

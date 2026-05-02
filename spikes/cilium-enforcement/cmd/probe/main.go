// Command probe is the CLI entry point for the Cilium enforcement spike.
//
// It performs the full validation sequence:
//  1. Checks required tools (kubectl, cilium CLI, helm, k3d, vcluster).
//  2. Runs all five Cilium enforcement tests defined in §11.3 Test 5.
//  3. Prints a formatted results table to stdout.
//  4. Exits with code 0 if all tests pass/skip, code 1 if any test fails.
//
// Usage:
//
//	probe [flags]
//
// Flags:
//
//	-tenant-a    Tenant-A namespace name (default: cilium-tenant-a)
//	-tenant-b    Tenant-B namespace name (default: cilium-tenant-b)
//	-platform    Platform namespace name (default: cf-system)
//	-vcluster-ns vCluster host namespace (default: vcluster-pilot)
//	-vcluster    vCluster name for Test 5 (default: pilot)
//	-verbose     Print per-test metrics after the results table
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cloud-forge/spikes/cilium-enforcement/internal/cluster"
	"github.com/cloud-forge/spikes/cilium-enforcement/internal/probe"
)

func main() {
	os.Exit(run())
}

func run() int {
	// ── Flags ─────────────────────────────────────────────────────────────
	tenantA := flag.String("tenant-a", "cilium-tenant-a", "name for the first tenant namespace")
	tenantB := flag.String("tenant-b", "cilium-tenant-b", "name for the second tenant namespace")
	platformNs := flag.String("platform", "cf-system", "platform (control-plane) namespace name")
	vclusterNs := flag.String("vcluster-ns", "vcluster-pilot", "host namespace for the vCluster in Test 5")
	vclusterName := flag.String("vcluster", "pilot", "vCluster name for Test 5")
	verbose := flag.Bool("verbose", false, "print per-test metrics after results table")
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
	cfg.TenantANamespace = *tenantA
	cfg.TenantBNamespace = *tenantB
	cfg.PlatformNamespace = *platformNs
	cfg.VClusterNamespace = *vclusterNs
	cfg.VClusterName = *vclusterName

	// ── Run all five tests ──────────────────────────────────────────────────
	slog.Info("starting spike tests", "tests", len(probe.AllTests()))
	c := cluster.NewRealClient()
	results := probe.RunAll(ctx, c, cfg)

	// ── Print results ────────────────────────────────────────────────────────
	probe.PrintResults(os.Stdout, results)

	if *verbose {
		probe.PrintMetrics(os.Stdout, results)
	}

	// ── Exit code ─────────────────────────────────────────────────────────────
	if !probe.AllPassed(results) {
		return 1
	}
	return 0
}

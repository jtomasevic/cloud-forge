// Package main is the entry point for the Spike 0.8 cold-start benchmark tool.
//
// It parses CLI flags, builds the Runner with production dependencies
// (HTTPProber + KubectlPodCounter), executes the benchmark, and prints the
// results table to stdout.
//
// # Usage
//
//	go run ./cmd/measure --service=all --samples=10
//	go run ./cmd/measure --service=minimal --samples=5
//	go run ./cmd/measure --service=heavy --namespace=functions --samples=3
//
// # Exit codes
//
//	0  — all measured variants pass their p95 thresholds
//	1  — one or more variants exceed their p95 threshold (or an error occurred)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/jtomasevic/cloud-forge/spikes/knative-coldstart/internal/measure"
)

// nowUTC returns the current time in UTC; extracted for clarity.
func nowUTC() time.Time { return time.Now().UTC() }

// runtimePlatform returns a human-readable platform string for FINDINGS.md.
func runtimePlatform() string {
	return fmt.Sprintf("%s/%s %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
}

func main() {
	// ── Parse flags ───────────────────────────────────────────────────────────
	serviceFlag := flag.String("service", "all",
		`Which variant(s) to measure.
Values: "all", "minimal", "medium", "heavy"`)

	samplesFlag := flag.Int("samples", 10,
		"Number of cold-start measurements to take per variant (minimum 1).")

	namespaceFlag := flag.String("namespace", "default",
		"Kubernetes namespace where the Knative Services are deployed.")

	baseURLFlag := flag.String("base-url", "",
		`URL pattern for service URLs (optional).
Default: "http://fn-%s.<namespace>.127.0.0.1.sslip.io:9080"
The %s placeholder is replaced with the variant name (minimal|medium|heavy).`)

	knativeVersionFlag := flag.String("knative-version", "",
		"Knative Serving version to display in the results table (e.g. v1.15.0).")

	flag.Parse()

	// ── Build logger ──────────────────────────────────────────────────────────
	// Use JSON output so Knative log collectors can parse progress messages.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// ── Build config ──────────────────────────────────────────────────────────
	cfg := measure.DefaultConfig()
	cfg.Namespace = *namespaceFlag

	if *samplesFlag < 1 {
		logger.Error("--samples must be at least 1")
		os.Exit(1)
	}
	cfg.Samples = *samplesFlag

	if *baseURLFlag != "" {
		cfg.BaseURL = *baseURLFlag
	} else {
		// Incorporate the namespace flag into the default URL pattern.
		cfg.BaseURL = fmt.Sprintf("http://fn-%%s.%s.127.0.0.1.sslip.io:9080", *namespaceFlag)
	}

	// ── Resolve variants ──────────────────────────────────────────────────────
	variants, err := parseServiceFlag(*serviceFlag)
	if err != nil {
		logger.Error("invalid --service value", "error", err)
		flag.Usage()
		os.Exit(1)
	}

	// ── Graceful shutdown on SIGINT / SIGTERM ─────────────────────────────────
	// A Ctrl-C during the benchmark prints partial results for any completed variants.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Run the benchmark ──────────────────────────────────────────────────────
	prober := measure.NewHTTPProber(cfg.RequestTimeout, 0)
	counter := &measure.KubectlPodCounter{}
	runner := measure.NewRunner(prober, counter, cfg, logger)

	logger.Info("benchmark starting",
		"variants", strings.Join(variantNames(variants), ","),
		"samples", cfg.Samples,
		"namespace", cfg.Namespace,
	)

	// Run only the requested variants or all if "all" was specified.
	result := measure.BenchmarkResult{
		Results:        make(map[measure.Variant]measure.Stats, len(variants)),
		KnativeVersion: *knativeVersionFlag,
		Platform:       "", // populated by runner when RunAll is called; set manually here
	}

	if len(variants) == len(measure.AllVariants) {
		// RunAll populates Platform and StartedAt automatically.
		result = runner.RunAll(ctx)
		result.KnativeVersion = *knativeVersionFlag
	} else {
		// Partial run: only measure the requested variants.
		result.StartedAt = nowUTC()
		result.Platform = runtimePlatform()
		for _, v := range variants {
			if ctx.Err() != nil {
				logger.Warn("stopping early due to context cancellation")
				break
			}
			samples := runner.RunVariant(ctx, v)
			result.Results[v] = measure.ComputeStats(v, samples)
		}
	}

	// ── Print results ──────────────────────────────────────────────────────────
	measure.PrintTable(os.Stdout, result)

	if !result.AllPassed() {
		// Non-zero exit so `make ci-measure` can block a PR when thresholds are breached.
		os.Exit(1)
	}
}

// parseServiceFlag converts the --service flag value into a slice of Variants.
//
// Accepts "all" or a comma-separated list of variant names.
func parseServiceFlag(s string) ([]measure.Variant, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "all" {
		return measure.AllVariants, nil
	}

	// Build a lookup set of valid names for quick validation.
	valid := make(map[measure.Variant]bool, len(measure.AllVariants))
	for _, v := range measure.AllVariants {
		valid[v] = true
	}

	var variants []measure.Variant
	for _, part := range strings.Split(s, ",") {
		v := measure.Variant(strings.TrimSpace(part))
		if !valid[v] {
			return nil, fmt.Errorf("unknown variant %q — valid values: minimal, medium, heavy, all", v)
		}
		variants = append(variants, v)
	}

	if len(variants) == 0 {
		return nil, fmt.Errorf("--service must not be empty")
	}

	return variants, nil
}

// variantNames returns the string names of a slice of Variants (for logging).
func variantNames(variants []measure.Variant) []string {
	names := make([]string, len(variants))
	for i, v := range variants {
		names[i] = string(v)
	}
	return names
}

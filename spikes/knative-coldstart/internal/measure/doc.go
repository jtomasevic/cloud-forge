// Package measure contains the cold-start benchmark logic for Spike 0.8.
//
// # Overview
//
// The package is split into small, single-responsibility files so every
// piece of logic can be unit-tested in isolation:
//
//   - types.go    — Variant, Sample, Stats, BenchmarkResult, Config
//   - stats.go    — percentile computation and duration formatting
//   - probe.go    — Prober interface + HTTPProber (TTFB measurement)
//   - poller.go   — PodCounter interface + KubectlPodCounter + WaitForScaleToZero
//   - runner.go   — Runner orchestrates scale-to-zero + probe per variant
//   - table.go    — PrintTable renders the terminal performance table
//
// # Usage
//
//	cfg := measure.DefaultConfig()
//	runner := measure.NewRunner(
//	    measure.NewHTTPProber(cfg.RequestTimeout),
//	    &measure.KubectlPodCounter{},
//	    cfg,
//	    logger,
//	)
//	result := runner.RunAll(ctx)
//	measure.PrintTable(os.Stdout, result)
//	if !result.AllPassed() {
//	    os.Exit(1)
//	}
//
// # Testing
//
// All interfaces (Prober, PodCounter) are designed to be swapped out in tests
// with lightweight mock structs defined in the test files.  No code-generation
// tool is required.
package measure

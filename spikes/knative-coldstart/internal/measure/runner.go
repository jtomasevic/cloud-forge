package measure

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"
)

// Runner orchestrates cold-start benchmark runs for Knative function variants.
//
// It composes a Prober (HTTP measurement) and a PodCounter (scale-to-zero
// detection) so both can be swapped out in tests without a live cluster.
//
// Typical usage:
//
//	runner := NewRunner(
//	    NewHTTPProber(cfg.RequestTimeout),
//	    &KubectlPodCounter{},
//	    cfg,
//	    logger,
//	)
//	result := runner.RunAll(ctx)
type Runner struct {
	// prober measures TTFB for individual HTTP requests.
	prober Prober

	// counter reports the number of ready pods for a given Knative service.
	counter PodCounter

	// cfg controls the benchmark parameters (sample count, timeouts, etc.).
	cfg Config

	// logger receives operational log messages during the run.
	logger *slog.Logger
}

// NewRunner creates a Runner with the given dependencies and configuration.
//
// Pass a slog.New(slog.NewTextHandler(io.Discard, nil)) logger in tests
// to suppress output without losing log-based progress visibility in CI.
func NewRunner(prober Prober, counter PodCounter, cfg Config, logger *slog.Logger) *Runner {
	return &Runner{
		prober:  prober,
		counter: counter,
		cfg:     cfg,
		logger:  logger,
	}
}

// serviceURL constructs the Knative Service URL for a variant.
//
// The pattern from Config.BaseURL is formatted with the variant name, producing
// a URL like:  http://fn-minimal.default.127.0.0.1.sslip.io:9080
func (r *Runner) serviceURL(variant Variant) string {
	return fmt.Sprintf(r.cfg.BaseURL, string(variant))
}

// serviceName returns the Kubernetes service name for a variant.
//
// Knative Services are named "fn-<variant>" (e.g. "fn-minimal") as defined in
// deploy/service-*.yaml.
func (r *Runner) serviceName(variant Variant) string {
	return fmt.Sprintf("fn-%s", string(variant))
}

// RunVariant executes cfg.Samples cold-start measurements for a single variant.
//
// For each sample it:
//  1. Waits for all pods for the variant to disappear (scale-to-zero confirmed).
//  2. Sends a single HTTP GET probe and records the time-to-first-byte.
//  3. Appends the result (TTFB or error) to the returned slice.
//
// The returned slice always has exactly cfg.Samples entries.  Failed attempts
// have Sample.Error set and Sample.TTFB == 0.
//
// The function respects ctx cancellation: if the parent context is cancelled
// mid-run, subsequent samples are recorded as errors and the function returns.
func (r *Runner) RunVariant(ctx context.Context, variant Variant) []Sample {
	url := r.serviceURL(variant)
	svcName := r.serviceName(variant)
	samples := make([]Sample, 0, r.cfg.Samples)

	r.logger.Info("starting variant benchmark",
		"variant", string(variant),
		"url", url,
		"samples", r.cfg.Samples,
	)

	for i := range r.cfg.Samples {
		r.logger.Info("measuring sample",
			"variant", string(variant),
			"sample", fmt.Sprintf("%d/%d", i+1, r.cfg.Samples),
		)

		// Step 1: wait for the function to scale to zero.
		// Use a child context so the scale-down timeout does not bleed into
		// the subsequent HTTP probe timeout.
		scaleCtx, scaleCancel := context.WithTimeout(ctx, r.cfg.ScaleDownTimeout)
		err := WaitForScaleToZero(
			scaleCtx, r.counter, svcName, r.cfg.Namespace, r.cfg.PollInterval, r.logger,
		)
		scaleCancel()

		if err != nil {
			r.logger.Warn("scale-to-zero wait failed",
				"variant", string(variant),
				"sample", i+1,
				"error", err,
			)
			samples = append(samples, Sample{Error: fmt.Errorf("scale-to-zero: %w", err)})
			continue
		}

		// Step 2: send the cold-start probe.
		probeCtx, probeCancel := context.WithTimeout(ctx, r.cfg.RequestTimeout)
		ttfb, err := r.prober.Probe(probeCtx, url)
		probeCancel()

		if err != nil {
			r.logger.Warn("probe failed",
				"variant", string(variant),
				"sample", i+1,
				"error", err,
			)
			samples = append(samples, Sample{Error: fmt.Errorf("probe: %w", err)})
			continue
		}

		r.logger.Info("sample recorded",
			"variant", string(variant),
			"sample", fmt.Sprintf("%d/%d", i+1, r.cfg.Samples),
			"ttfb_ms", ttfb.Milliseconds(),
		)
		samples = append(samples, Sample{TTFB: ttfb})
	}

	return samples
}

// RunAll executes the benchmark for every variant in AllVariants in order
// and returns a consolidated BenchmarkResult.
//
// Variants are run sequentially so the cluster has time to reach a steady state
// between runs.  The function records Go runtime metadata in the result for
// inclusion in FINDINGS.md.
func (r *Runner) RunAll(ctx context.Context) BenchmarkResult {
	result := BenchmarkResult{
		Results:   make(map[Variant]Stats, len(AllVariants)),
		Platform:  fmt.Sprintf("%s/%s %s", runtime.GOOS, runtime.GOARCH, runtime.Version()),
		StartedAt: time.Now(),
	}

	for _, v := range AllVariants {
		// Bail out early if the parent context was cancelled (e.g. Ctrl-C).
		if ctx.Err() != nil {
			r.logger.Warn("aborting remaining variants", "reason", ctx.Err())
			break
		}
		samples := r.RunVariant(ctx, v)
		result.Results[v] = ComputeStats(v, samples)
	}

	return result
}

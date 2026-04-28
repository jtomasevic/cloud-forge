package measure

import "time"

// Variant represents one of the three function size classes measured in the spike.
//
// Size classes reflect real-world Knative function archetypes:
//   - minimal: pure logic functions with no embedded assets (AI call proxies, data transformers)
//   - medium:  functions carrying a small ML model or embedding index (~50 MB payload)
//   - heavy:   functions with a large model checkpoint or native library (~200 MB payload)
type Variant string

const (
	// VariantMinimal is a pure-logic function with no embedded assets.
	// Expected image size: < 10 MB.  Target p95 cold start: < 3 s.
	VariantMinimal Variant = "minimal"

	// VariantMedium carries a 50 MB synthetic payload simulating a small ML model.
	// Expected image size: ~100 MB.  Target p95 cold start: < 5 s.
	VariantMedium Variant = "medium"

	// VariantHeavy carries a 200 MB synthetic payload simulating a large model checkpoint.
	// Expected image size: ~500 MB.  Target p95 cold start: documented (may exceed threshold).
	VariantHeavy Variant = "heavy"
)

// AllVariants is the canonical ordered list of variants used by RunAll.
// The order determines display order in the results table.
var AllVariants = []Variant{VariantMinimal, VariantMedium, VariantHeavy}

// ImageSizes maps each variant to its expected container image size string.
// These values are shown in the results table and must be updated when the
// function Dockerfiles or embedded payload sizes change.
var ImageSizes = map[Variant]string{
	VariantMinimal: "8 MB",
	VariantMedium:  "98 MB",
	VariantHeavy:   "512 MB",
}

// p95Thresholds defines the maximum acceptable p95 cold-start latency per variant.
// Values are derived from CF-FunctionTrigger UX requirements
// (see docs/plan/0-foundation-and-spikes.md Task 0.8).
//
// The tool exits non-zero if any variant breaches its threshold so the
// benchmark can act as a CI gate.
var p95Thresholds = map[Variant]time.Duration{
	VariantMinimal: 3 * time.Second,
	VariantMedium:  5 * time.Second,
	VariantHeavy:   10 * time.Second, // documented ceiling; breach triggers min-replicas=1 requirement
}

// P95Threshold returns the p95 cold-start SLA for the given variant.
// Returns 0 for unknown variants.
func P95Threshold(v Variant) time.Duration {
	return p95Thresholds[v]
}

// Sample holds the result of a single cold-start measurement attempt.
//
// A successful sample has Error == nil and TTFB > 0.
// A failed sample has Error != nil; TTFB is meaningless and should be ignored.
type Sample struct {
	// TTFB is the elapsed time from sending the HTTP request to receiving the
	// first byte of the response body.  Only valid when Error is nil.
	TTFB time.Duration

	// Error describes why the measurement failed.  nil on success.
	// Common causes: scale-to-zero timeout exceeded, HTTP request timeout,
	// Knative ingress returning 503 while a pod is being scheduled.
	Error error
}

// Stats holds the percentile summary for one variant's complete sample set.
//
// All duration fields are zero when no successful samples were collected
// (i.e. SampleCount == FailCount).
type Stats struct {
	// Variant identifies which function size class these stats describe.
	Variant Variant

	// ImageSize is the expected container image size (from ImageSizes map).
	// Populated by ComputeStats.
	ImageSize string

	// P50 is the median (50th percentile) TTFB across successful samples.
	P50 time.Duration

	// P75 is the 75th percentile TTFB.
	P75 time.Duration

	// P95 is the 95th percentile TTFB.  This is the primary acceptance metric.
	P95 time.Duration

	// P99 is the 99th percentile TTFB.  Useful for spotting outliers.
	P99 time.Duration

	// Min is the fastest cold start recorded in the sample set.
	Min time.Duration

	// Max is the slowest cold start recorded in the sample set.
	Max time.Duration

	// SampleCount is the total number of measurement attempts (success + failure).
	SampleCount int

	// FailCount is the number of samples where either scale-to-zero timed out
	// or the HTTP probe returned an error.
	FailCount int
}

// PassesThreshold reports whether this variant's p95 is within its acceptance
// threshold (as defined in p95Thresholds).
//
// A variant with zero successful samples always fails (returns false).
func (s Stats) PassesThreshold() bool {
	if s.SampleCount == 0 || s.SampleCount == s.FailCount {
		// No successful samples — cannot make a determination.
		return false
	}
	threshold, ok := p95Thresholds[s.Variant]
	if !ok {
		// Unknown variant; pass by default to avoid blocking on unrecognised inputs.
		return true
	}
	return s.P95 <= threshold
}

// Threshold returns the configured p95 acceptance threshold for this variant.
// Returns 0 for unknown variants.
func (s Stats) Threshold() time.Duration {
	return p95Thresholds[s.Variant]
}

// BenchmarkResult collects stats for all measured variants and records
// the metadata needed for FINDINGS.md.
type BenchmarkResult struct {
	// Results maps each Variant to its computed Stats.
	Results map[Variant]Stats

	// KnativeVersion is the Knative Serving version installed on the cluster.
	// Populated from `kubectl get namespace knative-serving -o json` by the CLI.
	KnativeVersion string

	// Platform is the Go runtime descriptor: "GOOS/GOARCH goVERSION".
	// Populated by runner.RunAll.
	Platform string

	// StartedAt is the wall-clock timestamp when RunAll was called.
	StartedAt time.Time
}

// AllPassed reports true when every variant in Results meets its p95 threshold.
//
// Returns false when Results is empty (no variants were measured).
// Variants absent from the Results map are ignored.
func (r BenchmarkResult) AllPassed() bool {
	if len(r.Results) == 0 {
		return false
	}
	for _, s := range r.Results {
		if !s.PassesThreshold() {
			return false
		}
	}
	return true
}

// Config controls how the Runner executes a benchmark run.
//
// Use DefaultConfig to get sensible starting values and override individual
// fields as needed.
type Config struct {
	// Samples is the number of cold-start measurements to take per variant.
	// Must be ≥ 1.  Default: 10.
	Samples int

	// Namespace is the Kubernetes namespace where the Knative Services live.
	// Default: "default".
	Namespace string

	// BaseURL is a fmt.Sprintf pattern used to build service URLs.
	// The runner substitutes the variant name (e.g. "minimal"):
	//   fmt.Sprintf(BaseURL, "minimal") → "http://fn-minimal.default.127.0.0.1.sslip.io:9080"
	// Default: "http://fn-%s.default.127.0.0.1.sslip.io:9080"
	BaseURL string

	// ScaleDownTimeout is the maximum duration to wait for a function pod to
	// reach zero replicas before declaring the sample a failure.
	// Default: 90 s.
	ScaleDownTimeout time.Duration

	// RequestTimeout is the per-request HTTP deadline including connection and
	// time-to-first-byte.  Default: 30 s.
	RequestTimeout time.Duration

	// PollInterval is the interval between ready-pod-count polls during the
	// scale-to-zero wait.  Default: 5 s.
	PollInterval time.Duration
}

// DefaultConfig returns a Config with sensible defaults for the spike benchmark.
//
// Timeout rationale (service YAMLs set window=6s, grace-period=10s):
//   - ScaleDownTimeout: 40s — 6s stable-window + 10s grace + 24s safety margin
//   - RequestTimeout:   30s — enough for the activator retry loop on a cold pod
//   - PollInterval:      3s — tighter poll catches scale-to-zero ~1 cycle faster
func DefaultConfig() Config {
	return Config{
		Samples:          10,
		Namespace:        "default",
		BaseURL:          "http://fn-%s.default.127.0.0.1.sslip.io:9080",
		ScaleDownTimeout: 40 * time.Second,
		RequestTimeout:   30 * time.Second,
		PollInterval:     3 * time.Second,
	}
}

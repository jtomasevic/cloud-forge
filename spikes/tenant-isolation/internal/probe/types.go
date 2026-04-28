package probe

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test identifiers
// ──────────────────────────────────────────────────────────────────────────────

// TestName is a stable string identifier for each of the six spike tests.
type TestName string

const (
	TestNetworkIsolation   TestName = "network_isolation"
	TestProvisioningSpeed  TestName = "provisioning_speed"
	TestProvisionerComm    TestName = "provisioner_communication"
	TestResourceOverhead   TestName = "resource_overhead"
	TestCiliumEnforcement  TestName = "cilium_enforcement"
	TestFailureRecovery    TestName = "failure_recovery"
)

// AllTests returns the canonical ordered list of test names.
// Primarily used by cmd/probe/main.go to display the test count.
func AllTests() []TestName { return allTests }

// allTests is the canonical ordered list of tests used by the runner.
// Access via AllTests() from external packages.
var allTests = []TestName{
	TestNetworkIsolation,
	TestProvisioningSpeed,
	TestProvisionerComm,
	TestResourceOverhead,
	TestCiliumEnforcement,
	TestFailureRecovery,
}

// ──────────────────────────────────────────────────────────────────────────────
// Pass / fail verdict
// ──────────────────────────────────────────────────────────────────────────────

// Verdict indicates whether a test passed, failed, or was skipped.
type Verdict string

const (
	VerdictPass Verdict = "PASS"
	VerdictFail Verdict = "FAIL"
	VerdictSkip Verdict = "SKIP" // used when prerequisites are absent (e.g. Cilium not installed)
)

// ──────────────────────────────────────────────────────────────────────────────
// TestResult
// ──────────────────────────────────────────────────────────────────────────────

// TestResult captures the outcome of a single spike test.
type TestResult struct {
	Name     TestName
	Verdict  Verdict
	Evidence string            // human-readable explanation or key observation
	Metrics  map[string]string // optional key→value measurements (latency, memory, etc.)
	Duration time.Duration     // wall-clock time for the test itself
}

// Pass returns true if the test passed.
func (r TestResult) Pass() bool { return r.Verdict == VerdictPass }

// String returns a one-line summary suitable for logging.
func (r TestResult) String() string {
	return fmt.Sprintf("[%s] %s (%s)", r.Verdict, r.Name, r.Duration.Round(time.Millisecond))
}

// MetricOrEmpty returns the metric value or "-" if not set.
func (r TestResult) MetricOrEmpty(key string) string {
	if v, ok := r.Metrics[key]; ok {
		return v
	}
	return "-"
}

// ──────────────────────────────────────────────────────────────────────────────
// PodResource — resource snapshot for a single pod
// ──────────────────────────────────────────────────────────────────────────────

// PodResource holds the resource usage for a single pod as reported by
// `kubectl top pods`. Fields are parsed from the human-readable kubectl output.
type PodResource struct {
	Name     string
	CPUMilli int64 // millicores (e.g. "250m" → 250)
	MemMB    int64 // mebibytes  (e.g. "128Mi" → 128)
}

// TotalCPUMilli sums CPU usage across a slice of pods.
func TotalCPUMilli(pods []PodResource) int64 {
	var sum int64
	for _, p := range pods {
		sum += p.CPUMilli
	}
	return sum
}

// TotalMemMB sums memory usage across a slice of pods.
func TotalMemMB(pods []PodResource) int64 {
	var sum int64
	for _, p := range pods {
		sum += p.MemMB
	}
	return sum
}

// ParseCPUMillicores converts a kubectl CPU string to millicores.
//   - "250m" → 250
//   - "2"    → 2000  (2 full cores)
//   - ""     → 0
func ParseCPUMillicores(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "<unknown>" {
		return 0
	}
	if strings.HasSuffix(s, "m") {
		var v int64
		fmt.Sscanf(strings.TrimSuffix(s, "m"), "%d", &v)
		return v
	}
	// Whole cores
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v * 1000
}

// ParseMemMB converts a kubectl memory string to integer mebibytes.
//   - "128Mi" → 128
//   - "1Gi"   → 1024
//   - "512Ki" → 0  (rounds down to 0 MiB for very small values)
//   - "500M"  → 500 (decimal megabytes, treated as MiB for simplicity)
func ParseMemMB(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "<unknown>" {
		return 0
	}
	switch {
	case strings.HasSuffix(s, "Gi"):
		var v int64
		fmt.Sscanf(strings.TrimSuffix(s, "Gi"), "%d", &v)
		return v * 1024
	case strings.HasSuffix(s, "Mi"):
		var v int64
		fmt.Sscanf(strings.TrimSuffix(s, "Mi"), "%d", &v)
		return v
	case strings.HasSuffix(s, "Ki"):
		var v int64
		fmt.Sscanf(strings.TrimSuffix(s, "Ki"), "%d", &v)
		return v / 1024
	case strings.HasSuffix(s, "G"):
		var v int64
		fmt.Sscanf(strings.TrimSuffix(s, "G"), "%d", &v)
		return v * 1024
	case strings.HasSuffix(s, "M"):
		var v int64
		fmt.Sscanf(strings.TrimSuffix(s, "M"), "%d", &v)
		return v
	case strings.HasSuffix(s, "k"):
		var v int64
		fmt.Sscanf(strings.TrimSuffix(s, "k"), "%d", &v)
		return v / 1024
	}
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v / (1024 * 1024)
}

// ──────────────────────────────────────────────────────────────────────────────
// Config — spike runtime configuration and pass/fail thresholds
// ──────────────────────────────────────────────────────────────────────────────

// Config holds all tunable parameters for the spike.
// Threshold fields correspond directly to the pass/fail table in §11.5.
type Config struct {
	// Kubernetes context / cluster name to target.
	KubeconfigPath string

	// Names used for the two tenant virtual clusters created during the spike.
	TenantAName string
	TenantBName string

	// Directory where generated per-vCluster kubeconfigs are written.
	KubeconfigDir string

	// Number of provisioning cycles to run for Test 2 (speed).
	SpeedSamples int

	// ── Thresholds ──────────────────────────────────────────────────────────

	// Maximum seconds from "vcluster create" to API server being healthy.
	// Pass: < 90 s.  Fail: > 180 s.
	VClusterReadySeconds float64

	// Maximum seconds from API ready to NATS JetStream being healthy inside vCluster.
	// Pass: < 180 s.  Fail: > 300 s.
	NATSReadySeconds float64

	// Maximum combined RAM for all idle vCluster control-plane pods (MiB).
	// Revised after spike run: vCluster v0.33.2 with k3s control plane uses ~431 MiB.
	// Pass: < 512.  Fail: > 768.
	MaxIdleRAMMB int64

	// Maximum combined CPU for all idle vCluster control-plane pods (millicores).
	// Measured steady-state (after k3s/etcd warm-up): ~102 m.
	// Pass: < 150.  Fail: > 300.  (150 m = 47% headroom above observed steady-state.)
	MaxIdleCPUMilli int64

	// Maximum seconds for vCluster API server to become Ready after pod deletion.
	// Pass: < 60 s.  Fail: > 120 s.
	RecoverySeconds float64

	// ── Overhead measurement tuning ─────────────────────────────────────────

	// OverheadStabilizationWait is the time to wait after vCluster creation before
	// beginning resource measurements. vCluster's embedded k3s and etcd run at high
	// CPU for 2-3 minutes during initial startup; measuring without this wait
	// produces misleading (inflated) CPU numbers.
	//
	// Set to 0 to disable (useful in unit tests). Production default: 2 * time.Minute.
	// Configure via the -overhead-wait CLI flag.
	OverheadStabilizationWait time.Duration

	// OverheadSamples is the number of CPU/RAM snapshots taken at
	// OverheadSampleInterval intervals. The p50 across samples is used as the
	// authoritative idle overhead value, making the measurement resilient to
	// transient bursts from background reconciliation loops.
	//
	// Default: 3. Configure via the -overhead-samples CLI flag.
	OverheadSamples int

	// OverheadSampleInterval is the pause between consecutive overhead samples.
	// Set to 0 to disable (useful in unit tests). Production default: 30s.
	// Configure via the -overhead-interval CLI flag.
	OverheadSampleInterval time.Duration

	// Timeout for any single kubectl/vcluster CLI invocation.
	ExecTimeout time.Duration
}

// DefaultConfig returns sensible defaults matching the thresholds from §11.5.
//
// For production runs, override OverheadStabilizationWait and
// OverheadSampleInterval via the -overhead-wait and -overhead-interval flags
// (the Makefile sets 120s and 30s respectively).
func DefaultConfig() Config {
	return Config{
		TenantAName:          "tenant-a",
		TenantBName:          "tenant-b",
		KubeconfigDir:        "kubeconfigs",
		SpeedSamples:         3,
		VClusterReadySeconds: 90,
		NATSReadySeconds:     180,
		MaxIdleRAMMB:         512, // vCluster v0.33.2 (k3s control plane) uses ~431 MiB measured
		MaxIdleCPUMilli:      150, // steady-state ~102 m; 150 m = 47% headroom for production sizing
		RecoverySeconds:      60,

		// Overhead measurement: stabilization and multi-sample defaults.
		// OverheadStabilizationWait is intentionally 0 so unit tests run fast.
		// Real runs set it via -overhead-wait 120s (see Makefile OVERHEAD_WAIT).
		OverheadStabilizationWait: 0,
		OverheadSamples:           3,
		OverheadSampleInterval:    0, // 0 = no pause; set to 30s via -overhead-interval in production

		ExecTimeout: 5 * time.Minute,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// KubectlClient — injectable interface for all cluster operations
// ──────────────────────────────────────────────────────────────────────────────

// KubectlClient abstracts every cluster operation used by the six spike tests.
// The real implementation shells out to kubectl/vcluster; unit tests use FakeClient.
type KubectlClient interface {
	// RunInPod executes cmd inside a running container and returns stdout+stderr.
	// kubeconfigPath may be "" to use the ambient KUBECONFIG / in-cluster config.
	RunInPod(ctx context.Context, kubeconfigPath, namespace, pod, container string, cmd []string) (output string, err error)

	// Apply applies the YAML content to the cluster addressed by kubeconfigPath.
	Apply(ctx context.Context, kubeconfigPath string, yamlContent []byte) error

	// WaitPodReady blocks until at least one pod matching selector is Running/Ready,
	// or the timeout expires. Returns the pod name and elapsed wall-clock duration.
	WaitPodReady(ctx context.Context, kubeconfigPath, namespace, selector string, timeout time.Duration) (podName string, elapsed time.Duration, err error)

	// DeletePod deletes a pod by name (e.g., to simulate a crash).
	DeletePod(ctx context.Context, kubeconfigPath, namespace, pod string) error

	// GetPodResources returns current CPU/memory usage for pods matched by selector.
	// Requires metrics-server in the cluster; returns an empty slice if unavailable.
	GetPodResources(ctx context.Context, kubeconfigPath, namespace, selector string) ([]PodResource, error)

	// GetPodsByLabel lists pod names matching the label selector.
	GetPodsByLabel(ctx context.Context, kubeconfigPath, namespace, selector string) ([]string, error)

	// PodIP returns the first IP of a pod matched by selector.
	PodIP(ctx context.Context, kubeconfigPath, namespace, selector string) (string, error)
}

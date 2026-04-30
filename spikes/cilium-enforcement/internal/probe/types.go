package probe

import (
	"context"
	"fmt"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test identifiers
// ──────────────────────────────────────────────────────────────────────────────

// TestName is a stable string identifier for each of the five spike tests.
type TestName string

const (
	// TestCrossNamespaceDeny verifies that a CiliumNetworkPolicy denies TCP
	// traffic from tenant-B's namespace to tenant-A's namespace.
	TestCrossNamespaceDeny TestName = "cross_namespace_deny"

	// TestIntraNamespaceAllow verifies that the same CiliumNetworkPolicy
	// permits intra-namespace traffic within tenant-A.
	TestIntraNamespaceAllow TestName = "intra_namespace_allow"

	// TestPlatformIsolation verifies that a tenant namespace cannot reach
	// the cf-system platform namespace.
	TestPlatformIsolation TestName = "platform_isolation"

	// TestPolicyTrace verifies that `cilium policy trace` reports DENIED
	// for the cross-namespace connection, confirming the eBPF kernel decision.
	TestPolicyTrace TestName = "policy_trace"

	// TestVClusterCoexistence verifies that Cilium CNP enforcement holds
	// correctly when a vCluster StatefulSet is running in the host namespace.
	TestVClusterCoexistence TestName = "vcluster_coexistence"
)

// AllTests returns the canonical ordered list of test names.
func AllTests() []TestName { return allTests }

var allTests = []TestName{
	TestCrossNamespaceDeny,
	TestIntraNamespaceAllow,
	TestPlatformIsolation,
	TestPolicyTrace,
	TestVClusterCoexistence,
}

// ──────────────────────────────────────────────────────────────────────────────
// Pass / fail verdict
// ──────────────────────────────────────────────────────────────────────────────

// Verdict indicates whether a test passed, failed, or was skipped.
type Verdict string

const (
	VerdictPass Verdict = "PASS"
	VerdictFail Verdict = "FAIL"
	VerdictSkip Verdict = "SKIP" // used when prerequisites are absent (e.g. vcluster not installed)
)

// ──────────────────────────────────────────────────────────────────────────────
// TestResult
// ──────────────────────────────────────────────────────────────────────────────

// TestResult captures the outcome of a single spike test.
type TestResult struct {
	Name     TestName
	Verdict  Verdict
	Evidence string            // human-readable explanation or key observation
	Metrics  map[string]string // optional key→value measurements
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
// Config — spike runtime configuration
// ──────────────────────────────────────────────────────────────────────────────

// Config holds all tunable parameters for the spike.
type Config struct {
	// TenantANamespace is the namespace for the first simulated tenant.
	TenantANamespace string
	// TenantBNamespace is the namespace for the second simulated tenant (the "attacker").
	TenantBNamespace string
	// PlatformNamespace is the namespace simulating the CF control-plane / cf-system.
	PlatformNamespace string
	// VClusterNamespace is the host namespace where the vCluster pod runs in Test 5.
	VClusterNamespace string
	// VClusterName is the vCluster name created in Test 5.
	VClusterName string

	// PodReadyTimeout is the maximum time to wait for a test pod to become Running/Ready.
	PodReadyTimeout time.Duration
	// ExecTimeout is the maximum time for any single kubectl exec / policy trace call.
	ExecTimeout time.Duration
	// ConnectTimeoutSeconds is the -w/-connect-timeout value passed to curl.
	// Cilium DROP causes a timeout; REJECT causes an immediate refused.
	ConnectTimeoutSeconds int
}

// DefaultConfig returns sensible defaults for a local k3d cluster.
func DefaultConfig() Config {
	return Config{
		TenantANamespace:      "cilium-tenant-a",
		TenantBNamespace:      "cilium-tenant-b",
		PlatformNamespace:     "cf-system",
		VClusterNamespace:     "vcluster-pilot",
		VClusterName:          "pilot",
		PodReadyTimeout:       120 * time.Second,
		ExecTimeout:           30 * time.Second,
		ConnectTimeoutSeconds: 5,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// KubectlClient — injectable interface for all cluster operations
// ──────────────────────────────────────────────────────────────────────────────

// KubectlClient abstracts every cluster operation used by the five spike tests.
// The real implementation shells out to kubectl; unit tests use FakeClient.
type KubectlClient interface {
	// Apply applies YAML content to the cluster. kubeconfigPath may be "" for
	// the ambient kubeconfig.
	Apply(ctx context.Context, kubeconfigPath string, yamlContent []byte) error

	// WaitPodReady blocks until at least one pod matching selector is Running/Ready,
	// or the timeout expires. Returns the pod name and elapsed wall-clock duration.
	WaitPodReady(ctx context.Context, kubeconfigPath, namespace, selector string, timeout time.Duration) (podName string, elapsed time.Duration, err error)

	// RunInPod executes cmd inside a running container and returns stdout+stderr.
	RunInPod(ctx context.Context, kubeconfigPath, namespace, pod, container string, cmd []string) (output string, err error)

	// GetPodsByLabel lists pod names matching the label selector.
	GetPodsByLabel(ctx context.Context, kubeconfigPath, namespace, selector string) ([]string, error)

	// PodIP returns the first IP of a pod matched by selector.
	PodIP(ctx context.Context, kubeconfigPath, namespace, selector string) (string, error)
}

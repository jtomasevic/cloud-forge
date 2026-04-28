package probe

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Runner — orchestrates all six spike tests
// ──────────────────────────────────────────────────────────────────────────────

// RunInput carries all infrastructure state needed to execute the six tests.
// It is populated by main.go after vClusters are created.
type RunInput struct {
	// TenantAKubeconfig is the path to the kubeconfig for tenant-A's vCluster.
	TenantAKubeconfig string
	// TenantBKubeconfig is the path to the kubeconfig for tenant-B's vCluster.
	TenantBKubeconfig string
	// TenantANamespace is the host-cluster namespace that contains the vCluster pods for tenant-A.
	TenantANamespace string
	// TenantBNamespace is the host-cluster namespace that contains the vCluster pods for tenant-B.
	TenantBNamespace string
	// TenantAVClusterReadyElapsed is the measured vCluster creation time (used in Test 2).
	TenantAVClusterReadyElapsed time.Duration
}

// RunAll executes all six spike tests sequentially and returns their results.
//
// It logs each test start and finish to slog at INFO level so progress is
// visible in the terminal during a long run.
func RunAll(ctx context.Context, c KubectlClient, cfg Config, input RunInput) []TestResult {
	results := make([]TestResult, 0, len(allTests))

	for _, name := range allTests {
		slog.Info("starting test", "test", name)
		r := runOne(ctx, c, cfg, input, name)
		slog.Info("test complete", "test", name, "verdict", r.Verdict, "duration", r.Duration.Round(time.Millisecond))
		results = append(results, r)
	}

	return results
}

// runOne dispatches a single test by name.
func runOne(ctx context.Context, c KubectlClient, cfg Config, input RunInput, name TestName) TestResult {
	switch name {
	case TestNetworkIsolation:
		return RunTestNetworkIsolation(ctx, c, cfg, input.TenantAKubeconfig, input.TenantBKubeconfig)

	case TestProvisioningSpeed:
		return RunTestProvisioningSpeed(ctx, c, cfg,
			input.TenantAKubeconfig,
			input.TenantAVClusterReadyElapsed,
		)

	case TestProvisionerComm:
		return RunTestProvisionerComm(ctx, c, cfg, input.TenantAKubeconfig, input.TenantBKubeconfig)

	case TestResourceOverhead:
		return RunTestResourceOverhead(ctx, c, cfg, input.TenantANamespace, input.TenantBNamespace)

	case TestCiliumEnforcement:
		return RunTestCiliumEnforcement(ctx, c, cfg, input.TenantANamespace, input.TenantBNamespace)

	case TestFailureRecovery:
		return RunTestFailureRecovery(ctx, c, cfg, input.TenantANamespace)

	default:
		return TestResult{
			Name:     name,
			Verdict:  VerdictFail,
			Evidence: fmt.Sprintf("unknown test name: %q", name),
			Metrics:  map[string]string{},
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Summary helpers
// ──────────────────────────────────────────────────────────────────────────────

// CountByVerdict returns the number of results for each verdict.
func CountByVerdict(results []TestResult) map[Verdict]int {
	counts := map[Verdict]int{VerdictPass: 0, VerdictFail: 0, VerdictSkip: 0}
	for _, r := range results {
		counts[r.Verdict]++
	}
	return counts
}

// AllPassed returns true if every result has VerdictPass or VerdictSkip.
// Failed results return false; SKIP results are not counted as failures.
func AllPassed(results []TestResult) bool {
	for _, r := range results {
		if r.Verdict == VerdictFail {
			return false
		}
	}
	return true
}

// OverallVerdict returns a single verdict for the full run.
func OverallVerdict(results []TestResult) Verdict {
	if AllPassed(results) {
		counts := CountByVerdict(results)
		if counts[VerdictPass] > 0 {
			return VerdictPass
		}
		return VerdictSkip
	}
	return VerdictFail
}

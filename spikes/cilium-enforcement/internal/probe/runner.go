package probe

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Runner — orchestrates all five spike tests
// ──────────────────────────────────────────────────────────────────────────────

// RunAll executes all five Cilium enforcement tests sequentially and returns
// their results. Each test sets up its own pods and policies (Apply is idempotent),
// so tests can be re-run safely without cleaning up.
func RunAll(ctx context.Context, c KubectlClient, cfg Config) []TestResult {
	results := make([]TestResult, 0, len(allTests))
	for _, name := range allTests {
		slog.Info("starting test", "test", name)
		r := runOne(ctx, c, cfg, name)
		slog.Info("test complete",
			"test", name,
			"verdict", r.Verdict,
			"duration", r.Duration.Round(time.Millisecond))
		results = append(results, r)
	}
	return results
}

// runOne dispatches a single test by name.
func runOne(ctx context.Context, c KubectlClient, cfg Config, name TestName) TestResult {
	switch name {
	case TestCrossNamespaceDeny:
		return RunTestCrossNamespaceDeny(ctx, c, cfg)
	case TestIntraNamespaceAllow:
		return RunTestIntraNamespaceAllow(ctx, c, cfg)
	case TestPlatformIsolation:
		return RunTestPlatformIsolation(ctx, c, cfg)
	case TestPolicyTrace:
		return RunTestPolicyTrace(ctx, c, cfg)
	case TestVClusterCoexistence:
		return RunTestVClusterCoexistence(ctx, c, cfg)
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

// AllPassed returns true if every result is PASS or SKIP (no FAILs).
func AllPassed(results []TestResult) bool {
	for _, r := range results {
		if r.Verdict == VerdictFail {
			return false
		}
	}
	return true
}

// OverallVerdict returns a single verdict summarising the full run.
func OverallVerdict(results []TestResult) Verdict {
	if AllPassed(results) {
		if CountByVerdict(results)[VerdictPass] > 0 {
			return VerdictPass
		}
		return VerdictSkip
	}
	return VerdictFail
}

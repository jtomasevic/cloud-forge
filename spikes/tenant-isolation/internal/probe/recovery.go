package probe

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 6: Failure recovery
// ──────────────────────────────────────────────────────────────────────────────
//
// The vCluster API server pod is forcefully deleted (simulating a crash) and
// the test measures the time from deletion to the pod being Running/Ready again.
// Services running inside the vCluster (NATS) should continue operating during
// the outage because their pods remain running in the host namespace regardless
// of the API server state.
//
// Expected outcome: API server pod re-created by the StatefulSet controller and
// Ready within cfg.RecoverySeconds (pass threshold = 60 s).

// RunTestFailureRecovery kills the vCluster API server pod and measures the
// time for Kubernetes to reschedule and restore it.
//
// It also verifies that the NATS pod (deployed in a previous test) remained
// running during the outage by checking its status after recovery.
func RunTestFailureRecovery(
	ctx context.Context,
	c KubectlClient,
	cfg Config,
	tenantNamespace string,
) TestResult {
	start := time.Now()
	metrics := map[string]string{}
	recoveryThreshold := time.Duration(cfg.RecoverySeconds * float64(time.Second))

	// ── Step 1: Find the vCluster API server pod ──────────────────────────
	pods, err := c.GetPodsByLabel(ctx, "", tenantNamespace, "app=vcluster")
	if err != nil || len(pods) == 0 {
		return failResult(TestFailureRecovery,
			fmt.Sprintf("cannot find vCluster pod in namespace %s: %v", tenantNamespace, err),
			start, metrics)
	}
	vclusterPod := pods[0]
	metrics["target_pod"] = vclusterPod

	// ── Step 2: Delete the pod (simulate crash) ───────────────────────────
	deleteStart := time.Now()
	if err := c.DeletePod(ctx, "", tenantNamespace, vclusterPod); err != nil {
		return failResult(TestFailureRecovery,
			fmt.Sprintf("delete vCluster pod %q: %v", vclusterPod, err),
			start, metrics)
	}
	metrics["deleted_at"] = deleteStart.Format(time.RFC3339)

	// ── Step 3: Wait for the replacement pod to be Ready ─────────────────
	recoveryCtx, cancel := context.WithTimeout(ctx, recoveryThreshold+30*time.Second)
	defer cancel()

	newPod, elapsed, err := c.WaitPodReady(recoveryCtx, "", tenantNamespace, "app=vcluster", recoveryThreshold)
	metrics["recovery_elapsed"] = elapsed.Round(time.Millisecond).String()
	metrics["new_pod"] = newPod

	if err != nil {
		return failResult(TestFailureRecovery,
			fmt.Sprintf("vCluster API server did not recover within %s: %v", recoveryThreshold, err),
			start, metrics)
	}

	// ── Step 4: Verify NATS is still running in the tenant namespace ──────
	natsPods, natsErr := c.GetPodsByLabel(ctx, "", tenantNamespace, "app=nats")
	natsStillRunning := natsErr == nil && len(natsPods) > 0
	metrics["nats_still_running"] = fmt.Sprintf("%v", natsStillRunning)

	evidence := buildRecoveryEvidence(vclusterPod, elapsed, recoveryThreshold, natsStillRunning)

	if elapsed > recoveryThreshold {
		return failResult(TestFailureRecovery,
			evidence+" | FAIL: recovery time exceeded threshold",
			start, metrics)
	}
	return passResult(TestFailureRecovery, evidence, start, metrics)
}

// buildRecoveryEvidence formats a summary of the recovery test outcome.
func buildRecoveryEvidence(deletedPod string, elapsed, threshold time.Duration, natsOK bool) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("deleted=%s | ", deletedPod))
	sb.WriteString(fmt.Sprintf("recovery=%s (<=%s) | ", elapsed.Round(time.Second), threshold))
	if natsOK {
		sb.WriteString("NATS=running ✓")
	} else {
		sb.WriteString("NATS=not-detected (may not be deployed)")
	}
	return sb.String()
}

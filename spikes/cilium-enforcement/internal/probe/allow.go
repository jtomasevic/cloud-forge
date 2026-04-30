package probe

import (
	"context"
	"fmt"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 2: Intra-namespace allow
// ──────────────────────────────────────────────────────────────────────────────
//
// The same CNP applied in Test 1 (deny cross-namespace ingress, allow
// same-namespace) must permit traffic between pods within tenant-A.
//
// This test deploys a second probe pod inside tenant-A and verifies that it
// can reach the echo server — confirming the CNP is not overly restrictive.
//
// PASS: curl from allow-probe (tenant-A) to echo (tenant-A) succeeds.
// FAIL: curl fails (CNP too restrictive or echo not reachable).

// allowProbePodTemplate creates a probe pod within tenant-A for intra-namespace testing.
const allowProbePodTemplate = `apiVersion: v1
kind: Pod
metadata:
  name: allow-probe
  namespace: %s
  labels:
    app: allow-probe
spec:
  containers:
  - name: probe
    image: nicolaka/netshoot:latest
    command: ["sleep", "infinity"]
  terminationGracePeriodSeconds: 1
`

// RunTestIntraNamespaceAllow deploys an allow-probe inside tenant-A and verifies
// that it can successfully reach the echo server in the same namespace.
//
// The CNP from Test 1 allows same-namespace ingress; if Test 1 passed,
// this test confirms the allow side of the same policy.
//
// PASS: curl succeeds — intra-namespace traffic is permitted.
// FAIL: curl fails  — CNP is overly restrictive (bug in policy).
func RunTestIntraNamespaceAllow(ctx context.Context, c KubectlClient, cfg Config) TestResult {
	start := time.Now()
	metrics := map[string]string{}

	// ── Deploy allow-probe in tenant-A ────────────────────────────────────
	probeYAML := fmt.Sprintf(allowProbePodTemplate, cfg.TenantANamespace)
	if err := c.Apply(ctx, "", []byte(probeYAML)); err != nil {
		return failResult(TestIntraNamespaceAllow,
			fmt.Sprintf("apply allow-probe in %s: %v", cfg.TenantANamespace, err), start, metrics)
	}

	// ── Wait for allow-probe ready ────────────────────────────────────────
	if _, _, err := c.WaitPodReady(ctx, "", cfg.TenantANamespace, "app=allow-probe", cfg.PodReadyTimeout); err != nil {
		return failResult(TestIntraNamespaceAllow,
			fmt.Sprintf("allow-probe not ready in %s: %v", cfg.TenantANamespace, err), start, metrics)
	}

	// ── Get echo pod IP (may already exist from Test 1) ───────────────────
	echoIP, err := c.PodIP(ctx, "", cfg.TenantANamespace, "app=echo")
	if err != nil {
		return failResult(TestIntraNamespaceAllow,
			fmt.Sprintf("get echo pod IP in %s: %v (ensure Test 1 ran first or echo pod exists)", cfg.TenantANamespace, err),
			start, metrics)
	}
	metrics["echo_pod_ip"] = echoIP
	metrics["probe_namespace"] = cfg.TenantANamespace

	// ── Attempt intra-namespace connection ────────────────────────────────
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSeconds+5)*time.Second)
	defer cancel()

	target := fmt.Sprintf("http://%s:8080", echoIP)
	out, execErr := c.RunInPod(probeCtx, "",
		cfg.TenantANamespace, "allow-probe", "",
		[]string{"curl", "-s",
			"--connect-timeout", fmt.Sprintf("%d", cfg.ConnectTimeoutSeconds),
			"--max-time", fmt.Sprintf("%d", cfg.ConnectTimeoutSeconds+2),
			target},
	)
	metrics["curl_target"] = target
	metrics["curl_output"] = truncate(out, 60)

	if isAllowed(out, execErr) {
		return passResult(TestIntraNamespaceAllow,
			fmt.Sprintf("curl success within %s: %s", cfg.TenantANamespace, truncate(out, 40)),
			start, metrics)
	}

	return failResult(TestIntraNamespaceAllow,
		fmt.Sprintf("intra-namespace curl FAILED in %s — CNP too restrictive? %s",
			cfg.TenantANamespace, formatCurlError(out)),
		start, metrics)
}

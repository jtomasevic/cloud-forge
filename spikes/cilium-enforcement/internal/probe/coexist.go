package probe

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 5: vCluster coexistence
// ──────────────────────────────────────────────────────────────────────────────
//
// When a vCluster StatefulSet pod runs in the host namespace, Cilium must still
// correctly enforce CiliumNetworkPolicies for that namespace.
//
// This test:
//   1. Creates a new namespace (vcluster-pilot) and provisions a minimal vCluster.
//   2. Applies a deny-cross-namespace CNP to that namespace.
//   3. Deploys an echo server in the same namespace.
//   4. Verifies that tenant-B still cannot reach the echo server (CNP enforcement
//      is not disrupted by the vCluster pod co-existing in the same namespace).
//
// PASS: vCluster pod is Running + cross-namespace deny is enforced.
// FAIL: curl succeeds (CNP broken by vCluster presence) or vCluster fails to start.
// SKIP: vcluster CLI is not installed.

// RunTestVClusterCoexistence creates a vCluster in the vcluster-pilot namespace,
// applies a CNP, and verifies that cross-namespace traffic is still blocked.
func RunTestVClusterCoexistence(ctx context.Context, c KubectlClient, cfg Config) TestResult {
	start := time.Now()
	metrics := map[string]string{}

	// ── Check if vcluster CLI is available ────────────────────────────────
	if !vclusterInstalled() {
		return skipResult(TestVClusterCoexistence,
			"vcluster CLI not installed — install via: brew install loft-sh/tap/vcluster",
			start)
	}

	// ── Create the vCluster namespace ─────────────────────────────────────
	nsYAML := fmt.Sprintf(namespacePodTemplate, cfg.VClusterNamespace)
	if err := c.Apply(ctx, "", []byte(nsYAML)); err != nil {
		return failResult(TestVClusterCoexistence,
			fmt.Sprintf("create namespace %s: %v", cfg.VClusterNamespace, err), start, metrics)
	}

	// ── Apply deny CNP to the vCluster namespace ──────────────────────────
	cnp := fmt.Sprintf(nsDenyPolicyTemplate, cfg.VClusterNamespace, cfg.VClusterNamespace)
	if err := c.Apply(ctx, "", []byte(cnp)); err != nil {
		return failResult(TestVClusterCoexistence,
			fmt.Sprintf("apply CNP to %s: %v", cfg.VClusterNamespace, err), start, metrics)
	}

	// ── Deploy echo pod in vCluster namespace ─────────────────────────────
	// This is the target that tenant-B will try to reach (should be blocked).
	echoYAML := fmt.Sprintf(echoPodTemplate, cfg.VClusterNamespace)
	if err := c.Apply(ctx, "", []byte(echoYAML)); err != nil {
		return failResult(TestVClusterCoexistence,
			fmt.Sprintf("apply echo pod in %s: %v", cfg.VClusterNamespace, err), start, metrics)
	}

	// ── Create vCluster ────────────────────────────────────────────────────
	// vcluster create <name> -n <namespace> --connect=false
	// This provisions the vCluster StatefulSet in the host namespace.
	vclusterCreateCtx, vcancel := context.WithTimeout(ctx, 5*time.Minute)
	defer vcancel()

	if err := runVClusterCreate(vclusterCreateCtx, cfg.VClusterName, cfg.VClusterNamespace); err != nil {
		return failResult(TestVClusterCoexistence,
			fmt.Sprintf("vcluster create %s: %v", cfg.VClusterName, err), start, metrics)
	}
	metrics["vcluster_name"] = cfg.VClusterName
	metrics["vcluster_namespace"] = cfg.VClusterNamespace

	// ── Wait for vCluster StatefulSet pod to be Running ────────────────────
	// The vCluster pod label is "app=vcluster" and name is "<vcluster-name>-0".
	vclusterSel := fmt.Sprintf("app=vcluster,release=%s", cfg.VClusterName)
	podName, _, err := c.WaitPodReady(ctx, "", cfg.VClusterNamespace, vclusterSel, 3*time.Minute)
	if err != nil {
		// Fallback selector: just app=vcluster
		podName, _, err = c.WaitPodReady(ctx, "", cfg.VClusterNamespace, "app=vcluster", 3*time.Minute)
		if err != nil {
			return failResult(TestVClusterCoexistence,
				fmt.Sprintf("vCluster pod not ready in %s: %v", cfg.VClusterNamespace, err), start, metrics)
		}
	}
	metrics["vcluster_pod"] = podName

	// ── Wait for echo pod ready ───────────────────────────────────────────
	if _, _, err := c.WaitPodReady(ctx, "", cfg.VClusterNamespace, "app=echo", cfg.PodReadyTimeout); err != nil {
		return failResult(TestVClusterCoexistence,
			fmt.Sprintf("echo pod not ready in %s: %v", cfg.VClusterNamespace, err), start, metrics)
	}

	// ── Get echo pod IP ───────────────────────────────────────────────────
	echoIP, err := c.PodIP(ctx, "", cfg.VClusterNamespace, "app=echo")
	if err != nil {
		return failResult(TestVClusterCoexistence,
			fmt.Sprintf("get echo pod IP in %s: %v", cfg.VClusterNamespace, err), start, metrics)
	}
	metrics["echo_pod_ip"] = echoIP

	// ── Ensure probe pod exists in tenant-B ──────────────────────────────
	probeYAML := fmt.Sprintf(netprobePodTemplate, cfg.TenantBNamespace)
	if err := c.Apply(ctx, "", []byte(probeYAML)); err != nil {
		return failResult(TestVClusterCoexistence,
			fmt.Sprintf("ensure netprobe in %s: %v", cfg.TenantBNamespace, err), start, metrics)
	}
	if _, _, err := c.WaitPodReady(ctx, "", cfg.TenantBNamespace, "app=netprobe", cfg.PodReadyTimeout); err != nil {
		return failResult(TestVClusterCoexistence,
			fmt.Sprintf("netprobe not ready in %s: %v", cfg.TenantBNamespace, err), start, metrics)
	}

	// ── Attempt cross-namespace connection ────────────────────────────────
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSeconds+5)*time.Second)
	defer cancel()

	target := fmt.Sprintf("http://%s:8080", echoIP)
	out, execErr := c.RunInPod(probeCtx, "",
		cfg.TenantBNamespace, "netprobe", "",
		[]string{"curl", "-s",
			"--connect-timeout", fmt.Sprintf("%d", cfg.ConnectTimeoutSeconds),
			"--max-time", fmt.Sprintf("%d", cfg.ConnectTimeoutSeconds+2),
			target},
	)
	metrics["curl_target"] = target
	metrics["curl_output"] = truncate(strings.TrimSpace(out), 120)

	if isBlocked(execErr) {
		evidence := fmt.Sprintf(
			"vCluster pod=%s Running + CNP blocks %s→%s:8080 | %s",
			podName, cfg.TenantBNamespace, cfg.VClusterNamespace, formatCurlError(out))
		return passResult(TestVClusterCoexistence, evidence, start, metrics)
	}

	return failResult(TestVClusterCoexistence,
		fmt.Sprintf("curl SUCCEEDED from %s to %s — CNP enforcement broken by vCluster presence",
			cfg.TenantBNamespace, cfg.VClusterNamespace),
		start, metrics)
}

// vclusterInstalled reports whether the vcluster binary is on the PATH.
func vclusterInstalled() bool {
	_, err := exec.LookPath("vcluster")
	return err == nil
}

// runVClusterCreate shells out to `vcluster create <name> -n <namespace> --connect=false`.
// It blocks until the command exits (vcluster create is synchronous in recent CLI versions).
func runVClusterCreate(ctx context.Context, name, namespace string) error {
	cmd := exec.CommandContext(ctx, "vcluster", "create", name,
		"-n", namespace,
		"--connect=false",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// "already exists" is acceptable — treat as success.
		if strings.Contains(string(out), "already exists") ||
			strings.Contains(string(out), "already present") {
			return nil
		}
		return fmt.Errorf("%w\noutput: %s", err, string(out))
	}
	return nil
}

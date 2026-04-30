package probe

import (
	"context"
	"fmt"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 3: Platform namespace isolation
// ──────────────────────────────────────────────────────────────────────────────
//
// The cf-system namespace represents the CloudForge control plane: CF-API,
// ScyllaDB, Keycloak, OpenBao. Tenant namespaces must never be able to initiate
// connections to cf-system services.
//
// This test applies a deny-cross-namespace CNP to cf-system and verifies
// that a pod in tenant-B cannot reach a service pod in cf-system.
//
// PASS: curl from tenant-B to cf-system times out or is refused.
// FAIL: curl succeeds — tenant can reach the platform namespace.

// platformServicePodTemplate deploys a minimal HTTP service in the platform namespace.
const platformServicePodTemplate = `apiVersion: v1
kind: Pod
metadata:
  name: platform-svc
  namespace: %s
  labels:
    app: platform-svc
spec:
  containers:
  - name: svc
    image: hashicorp/http-echo:latest
    args: ["-listen=:8080", "-text=cf-system"]
    ports:
    - containerPort: 8080
  terminationGracePeriodSeconds: 1
`

// RunTestPlatformIsolation deploys a service pod in the platform namespace (cf-system),
// applies a deny-cross-namespace CNP to it, and verifies that the netprobe pod
// in tenant-B cannot reach it.
//
// PASS: curl from tenant-B to cf-system is blocked.
// FAIL: curl succeeds or setup fails.
func RunTestPlatformIsolation(ctx context.Context, c KubectlClient, cfg Config) TestResult {
	start := time.Now()
	metrics := map[string]string{}

	// ── Create platform namespace ─────────────────────────────────────────
	nsYAML := fmt.Sprintf(namespacePodTemplate, cfg.PlatformNamespace)
	if err := c.Apply(ctx, "", []byte(nsYAML)); err != nil {
		return failResult(TestPlatformIsolation,
			fmt.Sprintf("create namespace %s: %v", cfg.PlatformNamespace, err), start, metrics)
	}

	// ── Apply deny CNP to cf-system ────────────────────────────────────────
	cnp := fmt.Sprintf(nsDenyPolicyTemplate, cfg.PlatformNamespace, cfg.PlatformNamespace)
	if err := c.Apply(ctx, "", []byte(cnp)); err != nil {
		return failResult(TestPlatformIsolation,
			fmt.Sprintf("apply CNP to %s: %v", cfg.PlatformNamespace, err), start, metrics)
	}

	// ── Deploy platform service pod ───────────────────────────────────────
	svcYAML := fmt.Sprintf(platformServicePodTemplate, cfg.PlatformNamespace)
	if err := c.Apply(ctx, "", []byte(svcYAML)); err != nil {
		return failResult(TestPlatformIsolation,
			fmt.Sprintf("apply platform-svc pod in %s: %v", cfg.PlatformNamespace, err), start, metrics)
	}

	// ── Wait for platform service pod ─────────────────────────────────────
	if _, _, err := c.WaitPodReady(ctx, "", cfg.PlatformNamespace, "app=platform-svc", cfg.PodReadyTimeout); err != nil {
		return failResult(TestPlatformIsolation,
			fmt.Sprintf("platform-svc not ready in %s: %v", cfg.PlatformNamespace, err), start, metrics)
	}

	// ── Get platform service pod IP ───────────────────────────────────────
	svcIP, err := c.PodIP(ctx, "", cfg.PlatformNamespace, "app=platform-svc")
	if err != nil {
		return failResult(TestPlatformIsolation,
			fmt.Sprintf("get platform-svc IP: %v", err), start, metrics)
	}
	metrics["platform_svc_ip"] = svcIP
	metrics["attacker_namespace"] = cfg.TenantBNamespace
	metrics["target_namespace"] = cfg.PlatformNamespace

	// ── Ensure the probe pod exists in tenant-B ───────────────────────────
	// (may already exist from Test 1; Apply is idempotent)
	probeYAML := fmt.Sprintf(netprobePodTemplate, cfg.TenantBNamespace)
	if err := c.Apply(ctx, "", []byte(probeYAML)); err != nil {
		return failResult(TestPlatformIsolation,
			fmt.Sprintf("ensure netprobe in %s: %v", cfg.TenantBNamespace, err), start, metrics)
	}
	if _, _, err := c.WaitPodReady(ctx, "", cfg.TenantBNamespace, "app=netprobe", cfg.PodReadyTimeout); err != nil {
		return failResult(TestPlatformIsolation,
			fmt.Sprintf("netprobe not ready in %s: %v", cfg.TenantBNamespace, err), start, metrics)
	}

	// ── Attempt cross-namespace connection from tenant-B to cf-system ─────
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSeconds+5)*time.Second)
	defer cancel()

	target := fmt.Sprintf("http://%s:8080", svcIP)
	out, execErr := c.RunInPod(probeCtx, "",
		cfg.TenantBNamespace, "netprobe", "",
		[]string{"curl", "-s",
			"--connect-timeout", fmt.Sprintf("%d", cfg.ConnectTimeoutSeconds),
			"--max-time", fmt.Sprintf("%d", cfg.ConnectTimeoutSeconds+2),
			target},
	)
	metrics["curl_target"] = target
	metrics["curl_output"] = truncate(out, 120)

	if isBlocked(execErr) {
		evidence := fmt.Sprintf("%s → %s blocked (Cilium DROP) | %s",
			cfg.TenantBNamespace, cfg.PlatformNamespace, formatCurlError(out))
		return passResult(TestPlatformIsolation, evidence, start, metrics)
	}

	return failResult(TestPlatformIsolation,
		fmt.Sprintf("curl SUCCEEDED from %s to %s — platform namespace reachable from tenant",
			cfg.TenantBNamespace, cfg.PlatformNamespace),
		start, metrics)
}

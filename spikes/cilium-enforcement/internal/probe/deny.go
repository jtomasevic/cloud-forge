package probe

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 1: Cross-namespace deny
// ──────────────────────────────────────────────────────────────────────────────
//
// A CiliumNetworkPolicy applied to tenant-A's namespace allows only ingress from
// pods within the same namespace. A probe pod in tenant-B attempts to connect
// to the echo server in tenant-A over TCP port 8080.
//
// Expected result: Cilium's eBPF dataplane drops the packet.
// curl exits with code 28 (operation timed out) or 7 (connection refused),
// both non-zero → RunInPod returns a non-nil error → PASS.
//
// Container images used:
//   - Echo server:  hashicorp/http-echo:latest  (-listen=:8080 -text=pong)
//   - Probe pod:    nicolaka/netshoot:latest    (has curl; runs `sleep infinity`)

// nsDenyPolicy is the CiliumNetworkPolicy applied to tenantNs.
// It selects all endpoints in the namespace and allows ingress only from
// pods bearing the same namespace label (same-namespace traffic = whitelist).
// Once any CNP selects an endpoint, Cilium enforces a deny-by-default for
// all unmatched flows — this is Cilium's identity-based model.
const nsDenyPolicyTemplate = `apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: deny-cross-namespace-ingress
  namespace: %s
spec:
  endpointSelector: {}
  ingress:
    - fromEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: %s
`

// echoPodTemplate creates the HTTP echo server in the given namespace.
const echoPodTemplate = `apiVersion: v1
kind: Pod
metadata:
  name: echo
  namespace: %s
  labels:
    app: echo
spec:
  containers:
  - name: echo
    image: hashicorp/http-echo:latest
    args: ["-listen=:8080", "-text=pong"]
    ports:
    - containerPort: 8080
  terminationGracePeriodSeconds: 1
`

// netprobePodTemplate creates a curl-equipped probe pod in the given namespace.
const netprobePodTemplate = `apiVersion: v1
kind: Pod
metadata:
  name: netprobe
  namespace: %s
  labels:
    app: netprobe
spec:
  containers:
  - name: netprobe
    image: nicolaka/netshoot:latest
    command: ["sleep", "infinity"]
  terminationGracePeriodSeconds: 1
`

// namespacePodTemplate creates a plain Kubernetes namespace.
const namespacePodTemplate = `apiVersion: v1
kind: Namespace
metadata:
  name: %s
`

// RunTestCrossNamespaceDeny sets up two namespaces, deploys an echo server in
// tenant-A with a deny-cross-namespace CNP, deploys a netprobe in tenant-B,
// and verifies that the probe cannot reach the echo server.
//
// PASS: curl from tenant-B to tenant-A times out or is refused (Cilium DROP/REJECT).
// FAIL: curl succeeds (CNP not enforced).
// FAIL: any setup step (apply / wait) fails.
func RunTestCrossNamespaceDeny(ctx context.Context, c KubectlClient, cfg Config) TestResult {
	start := time.Now()
	metrics := map[string]string{}

	// ── Namespace setup ───────────────────────────────────────────────────
	for _, ns := range []string{cfg.TenantANamespace, cfg.TenantBNamespace} {
		if err := c.Apply(ctx, "", []byte(fmt.Sprintf(namespacePodTemplate, ns))); err != nil {
			return failResult(TestCrossNamespaceDeny,
				fmt.Sprintf("create namespace %s: %v", ns, err), start, metrics)
		}
	}

	// ── Apply CiliumNetworkPolicy to tenant-A ─────────────────────────────
	cnp := fmt.Sprintf(nsDenyPolicyTemplate, cfg.TenantANamespace, cfg.TenantANamespace)
	if err := c.Apply(ctx, "", []byte(cnp)); err != nil {
		return failResult(TestCrossNamespaceDeny,
			fmt.Sprintf("apply CNP to %s: %v", cfg.TenantANamespace, err), start, metrics)
	}

	// ── Deploy echo server in tenant-A ────────────────────────────────────
	echoYAML := fmt.Sprintf(echoPodTemplate, cfg.TenantANamespace)
	if err := c.Apply(ctx, "", []byte(echoYAML)); err != nil {
		return failResult(TestCrossNamespaceDeny,
			fmt.Sprintf("apply echo pod in %s: %v", cfg.TenantANamespace, err), start, metrics)
	}

	// ── Deploy netprobe in tenant-B ───────────────────────────────────────
	probeYAML := fmt.Sprintf(netprobePodTemplate, cfg.TenantBNamespace)
	if err := c.Apply(ctx, "", []byte(probeYAML)); err != nil {
		return failResult(TestCrossNamespaceDeny,
			fmt.Sprintf("apply netprobe in %s: %v", cfg.TenantBNamespace, err), start, metrics)
	}

	// ── Wait for both pods ────────────────────────────────────────────────
	echoName, _, err := c.WaitPodReady(ctx, "", cfg.TenantANamespace, "app=echo", cfg.PodReadyTimeout)
	if err != nil {
		return failResult(TestCrossNamespaceDeny,
			fmt.Sprintf("echo pod not ready in %s: %v", cfg.TenantANamespace, err), start, metrics)
	}
	if _, _, err := c.WaitPodReady(ctx, "", cfg.TenantBNamespace, "app=netprobe", cfg.PodReadyTimeout); err != nil {
		return failResult(TestCrossNamespaceDeny,
			fmt.Sprintf("netprobe pod not ready in %s: %v", cfg.TenantBNamespace, err), start, metrics)
	}
	_ = echoName

	// ── Get echo pod IP ───────────────────────────────────────────────────
	echoIP, err := c.PodIP(ctx, "", cfg.TenantANamespace, "app=echo")
	if err != nil {
		return failResult(TestCrossNamespaceDeny,
			fmt.Sprintf("get echo pod IP: %v", err), start, metrics)
	}
	metrics["echo_pod_ip"] = echoIP
	metrics["attacker_namespace"] = cfg.TenantBNamespace
	metrics["target_namespace"] = cfg.TenantANamespace

	// ── Attempt cross-namespace connection from tenant-B ─────────────────
	// curl exit 28 = operation timed out (Cilium DROP)
	// curl exit 7  = connection refused (Cilium REJECT)
	// Both are non-zero → RunInPod returns an error → connection is blocked.
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
		evidence := fmt.Sprintf("%s → %s blocked (Cilium DROP) | %s",
			cfg.TenantBNamespace, cfg.TenantANamespace, formatCurlError(out))
		return passResult(TestCrossNamespaceDeny, evidence, start, metrics)
	}

	// curl succeeded — CNP is NOT enforcing the deny rule.
	return failResult(TestCrossNamespaceDeny,
		fmt.Sprintf("curl SUCCEEDED from %s to %s — CNP not enforcing deny",
			cfg.TenantBNamespace, cfg.TenantANamespace),
		start, metrics)
}

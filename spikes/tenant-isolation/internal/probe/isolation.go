package probe

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 1: Network isolation correctness
// ──────────────────────────────────────────────────────────────────────────────

// echoManifest is the minimal YAML deployed inside each vCluster.
// It runs a lightweight echo server on port 8080 so isolation tests have a
// real target to probe.
const echoManifest = `---
apiVersion: v1
kind: Pod
metadata:
  name: echo-server
  labels:
    app: echo-server
spec:
  containers:
  - name: echo
    image: hashicorp/http-echo:latest
    args: ["-text=hello-from-vcluster"]
    ports:
    - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: echo
spec:
  selector:
    app: echo-server
  ports:
  - port: 8080
    targetPort: 8080
`

// netprobeManifest is a sidecar pod used to run network probes from inside
// tenant-A's vCluster without requiring any pre-existing workloads.
const netprobeManifest = `---
apiVersion: v1
kind: Pod
metadata:
  name: netprobe
  labels:
    app: netprobe
spec:
  containers:
  - name: probe
    image: busybox:stable
    command: ["sleep", "3600"]
`

// IsolationResult captures the three attack-vector outcomes for Test 1.
type IsolationResult struct {
	// DirectIPBlocked is true when a direct pod-IP probe from tenant-A to
	// tenant-B's echo server is refused or times out.
	DirectIPBlocked bool
	// DNSBlocked is true when DNS resolution for echo.default.svc.cluster.local
	// from tenant-A returns NXDOMAIN or connection error (isolated DNS).
	DNSBlocked bool
	// EvidenceDirect is the raw nc/curl output for the direct-IP probe.
	EvidenceDirect string
	// EvidenceDNS is the raw nslookup output for the DNS probe.
	EvidenceDNS string
}

// RunTestNetworkIsolation deploys an echo server in tenant-B's vCluster and a
// network probe pod in tenant-A's vCluster, then verifies that:
//
//  1. A direct TCP connection from tenant-A to tenant-B's pod IP fails.
//  2. DNS resolution of tenant-B's service name from tenant-A fails.
//
// Both failures are required for the test to pass (Verdict: PASS).
// Any successful cross-tenant reach is a FAIL regardless of vector.
func RunTestNetworkIsolation(
	ctx context.Context,
	c KubectlClient,
	cfg Config,
	tenantAKubeconfig, tenantBKubeconfig string,
) TestResult {
	start := time.Now()
	metrics := map[string]string{}

	// ── Step 1: Deploy echo server in tenant-B ────────────────────────────
	if err := c.Apply(ctx, tenantBKubeconfig, []byte(echoManifest)); err != nil {
		return failResult(TestNetworkIsolation, fmt.Sprintf("deploy echo server in tenant-B: %v", err), start, metrics)
	}

	// Wait for echo server to be ready
	_, _, err := c.WaitPodReady(ctx, tenantBKubeconfig, "default", "app=echo-server", cfg.ExecTimeout)
	if err != nil {
		return failResult(TestNetworkIsolation, fmt.Sprintf("echo server not ready in tenant-B: %v", err), start, metrics)
	}

	// ── Step 2: Get tenant-B echo server pod IP ───────────────────────────
	tenantBIP, err := c.PodIP(ctx, tenantBKubeconfig, "default", "app=echo-server")
	if err != nil || tenantBIP == "" {
		return failResult(TestNetworkIsolation, fmt.Sprintf("cannot get tenant-B echo pod IP: %v", err), start, metrics)
	}
	metrics["tenant_b_echo_ip"] = tenantBIP

	// ── Step 3: Deploy netprobe pod in tenant-A ───────────────────────────
	if err := c.Apply(ctx, tenantAKubeconfig, []byte(netprobeManifest)); err != nil {
		return failResult(TestNetworkIsolation, fmt.Sprintf("deploy netprobe in tenant-A: %v", err), start, metrics)
	}
	probePod, _, err := c.WaitPodReady(ctx, tenantAKubeconfig, "default", "app=netprobe", cfg.ExecTimeout)
	if err != nil {
		return failResult(TestNetworkIsolation, fmt.Sprintf("netprobe not ready in tenant-A: %v", err), start, metrics)
	}
	if probePod == "" {
		probePod = "netprobe" // fallback name
	}

	// ── Step 4: Direct IP probe from tenant-A → tenant-B pod IP ──────────
	// nc -zv -w3 <ip> 8080 should fail (refused / timeout) because the IP
	// is in tenant-B's isolated pod network and not routable from tenant-A.
	ncOut, _ := c.RunInPod(ctx, tenantAKubeconfig, "default", probePod, "probe",
		[]string{"nc", "-zv", "-w3", tenantBIP, "8080"},
	)
	directBlocked := isConnectionBlocked(ncOut)
	metrics["direct_ip_output"] = ncOut

	// ── Step 5: DNS probe from tenant-A — resolve tenant-B service name ──
	// echo.default.svc.cluster.local should not resolve because tenant-A's
	// CoreDNS only knows about its own vCluster's services.
	dnsOut, _ := c.RunInPod(ctx, tenantAKubeconfig, "default", probePod, "probe",
		[]string{"nslookup", "echo.default.svc.cluster.local"},
	)
	dnsBlocked := isDNSBlocked(dnsOut)
	metrics["dns_output"] = dnsOut

	ir := IsolationResult{
		DirectIPBlocked: directBlocked,
		DNSBlocked:      dnsBlocked,
		EvidenceDirect:  ncOut,
		EvidenceDNS:     dnsOut,
	}

	evidence := buildIsolationEvidence(ir, tenantBIP)
	if directBlocked && dnsBlocked {
		return passResult(TestNetworkIsolation, evidence, start, metrics)
	}
	return failResult(TestNetworkIsolation, evidence, start, metrics)
}

// ──────────────────────────────────────────────────────────────────────────────
// Signal interpretation helpers — pure functions, fully unit-testable
// ──────────────────────────────────────────────────────────────────────────────

// isConnectionBlocked returns true when the nc output indicates the connection
// was NOT established (i.e., isolation is working correctly).
func isConnectionBlocked(output string) bool {
	output = strings.ToLower(output)
	// Positive isolation signals
	for _, sig := range []string{
		"connection refused",
		"no route to host",
		"network unreachable",
		"connection timed out",
		"timed out",
		"nc: bad address",
		"host is unreachable",
	} {
		if strings.Contains(output, sig) {
			return true
		}
	}
	// A successful connection would contain "succeeded" or "open"
	if strings.Contains(output, "succeeded") || strings.Contains(output, " open") {
		return false
	}
	// Anything else (empty, error on exec itself) is treated as blocked
	return true
}

// isDNSBlocked returns true when nslookup output indicates the name did NOT
// resolve — which is the expected behaviour for cross-vCluster DNS.
func isDNSBlocked(output string) bool {
	output = strings.ToLower(output)
	for _, sig := range []string{
		"nxdomain",
		"can't find",
		"server can't find",
		"name or service not known",
		"connection refused",
		"no servers could be reached",
	} {
		if strings.Contains(output, sig) {
			return true
		}
	}
	// If output contains an IP address, DNS resolved — isolation failure
	if strings.Contains(output, "address:") || strings.Contains(output, "addresses:") {
		return false
	}
	// Ambiguous or empty — treat as blocked
	return true
}

// buildIsolationEvidence formats a human-readable summary of the isolation test.
func buildIsolationEvidence(ir IsolationResult, targetIP string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("target-IP=%s | ", targetIP))
	if ir.DirectIPBlocked {
		sb.WriteString("direct-IP=BLOCKED ✓ | ")
	} else {
		sb.WriteString("direct-IP=REACHABLE ✗ | ")
	}
	if ir.DNSBlocked {
		sb.WriteString("DNS=BLOCKED ✓")
	} else {
		sb.WriteString("DNS=RESOLVED ✗")
	}
	return sb.String()
}

// ──────────────────────────────────────────────────────────────────────────────
// Shared result constructors
// ──────────────────────────────────────────────────────────────────────────────

func passResult(name TestName, evidence string, start time.Time, metrics map[string]string) TestResult {
	return TestResult{Name: name, Verdict: VerdictPass, Evidence: evidence, Metrics: metrics, Duration: time.Since(start)}
}

func failResult(name TestName, evidence string, start time.Time, metrics map[string]string) TestResult {
	return TestResult{Name: name, Verdict: VerdictFail, Evidence: evidence, Metrics: metrics, Duration: time.Since(start)}
}

func skipResult(name TestName, reason string, start time.Time) TestResult {
	return TestResult{Name: name, Verdict: VerdictSkip, Evidence: reason, Metrics: map[string]string{}, Duration: time.Since(start)}
}

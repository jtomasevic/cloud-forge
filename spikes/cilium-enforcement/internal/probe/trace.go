package probe

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 4: Policy trace — kernel-level DENY confirmation
// ──────────────────────────────────────────────────────────────────────────────
//
// Strategy 1 — cilium-dbg policy trace (Cilium v1.17+):
//   `cilium policy trace` was removed from the in-pod `cilium` binary in v1.17.
//   The equivalent is now `cilium-dbg policy trace`, which evaluates the policy
//   verdict against the in-kernel BPF policy map without real traffic.
//
//   PASS: "Final verdict: DENIED" appears in the trace output.
//   FAIL: "Final verdict: ALLOWED" appears in the trace output.
//
// Strategy 2 — hubble observe (fallback when cilium-dbg is unavailable):
//   If `cilium-dbg policy trace` is not available, fall back to `hubble observe`
//   to check for DROPPED flows in Hubble's ring buffer. This requires that
//   traffic was recently attempted (e.g. by Test 1) so Hubble has seen the drops.
//
//   PASS: at least one DROPPED flow from tenant-B → tenant-A port 8080.
//   FAIL: only FORWARDED flows or no relevant flows visible.
//   SKIP: Hubble is also unavailable.
//
// SKIP (both strategies): no cilium-agent pod found in kube-system.

// ciliumAgentSelector is the label selector for the cilium DaemonSet pods.
const ciliumAgentSelector = "k8s-app=cilium"

// isCiliumDbgUnavailable returns true when the combined output and error
// message indicate that cilium-dbg itself, or its "policy trace" subcommand,
// is not present. This distinguishes a missing binary/subcommand from a genuine
// policy-trace failure (e.g. ALLOWED verdict).
func isCiliumDbgUnavailable(out string, err error) bool {
	combined := out
	if err != nil {
		combined += " " + err.Error()
	}
	for _, indicator := range []string{
		"executable file not found",
		"no such file or directory",
		"command not found",
		// cilium-dbg prints a help page with "Available Commands:" when a
		// subcommand is not recognised — the observed failure mode in v1.17
		// when running the now-removed `cilium policy trace`.
		"Available Commands:",
		// cilium-dbg surfaces this for unknown subcommands.
		`unknown command "trace"`,
	} {
		if strings.Contains(combined, indicator) {
			return true
		}
	}
	return false
}

// RunTestPolicyTrace verifies the kernel-level deny decision for the
// cross-namespace flow (tenant-B/netprobe → tenant-A/echo port 8080 TCP).
//
// It first attempts cilium-dbg policy trace (v1.17+). If that binary or
// subcommand is unavailable it falls back to hubble observe.
//
// PASS: trace/observe confirms DENIED/DROPPED.
// FAIL: trace/observe confirms ALLOWED.
// SKIP: no cilium-agent pod found, or both probe strategies unavailable.
func RunTestPolicyTrace(ctx context.Context, c KubectlClient, cfg Config) TestResult {
	start := time.Now()
	metrics := map[string]string{}

	// ── Find a cilium-agent pod ───────────────────────────────────────────
	pods, err := c.GetPodsByLabel(ctx, "", "kube-system", ciliumAgentSelector)
	if err != nil || len(pods) == 0 {
		reason := "no cilium-agent pods found in kube-system"
		if err != nil {
			reason = fmt.Sprintf("get cilium pods: %v", err)
		}
		return skipResult(TestPolicyTrace, reason, start)
	}
	ciliumPod := pods[0]
	metrics["cilium_pod"] = ciliumPod

	// ── Strategy 1: cilium-dbg policy trace ──────────────────────────────
	//
	// cilium-dbg ships inside the same cilium-agent container and carries the
	// debug/introspection commands that were removed from the main `cilium`
	// binary in v1.17, including `policy trace`.
	traceArgs := []string{
		"cilium-dbg", "policy", "trace",
		"--src-namespace", cfg.TenantBNamespace,
		"--src-labels", "app=netprobe",
		"--dst-namespace", cfg.TenantANamespace,
		"--dst-labels", "app=echo",
		"--dport", "8080",
		"--protocol", "tcp",
	}
	out, err := c.RunInPod(ctx, "", "kube-system", ciliumPod, "cilium-agent", traceArgs)
	metrics["trace_output"] = truncate(out, 300)

	if err != nil && isCiliumDbgUnavailable(out, err) {
		// cilium-dbg binary or its 'policy trace' subcommand is not present;
		// fall through to the Hubble fallback.
		metrics["trace_strategy"] = "hubble_observe_fallback"
		return runHubbleObserveFallback(ctx, c, cfg, ciliumPod, start, metrics)
	}

	// cilium-dbg ran (err may still be non-nil for policy errors); interpret verdict.
	if strings.Contains(out, "Final verdict: DENIED") {
		return passResult(TestPolicyTrace,
			fmt.Sprintf("cilium-dbg policy trace: Final verdict: DENIED (%s→%s:8080/tcp)",
				cfg.TenantBNamespace, cfg.TenantANamespace),
			start, metrics)
	}
	if strings.Contains(out, "Final verdict: ALLOWED") {
		return failResult(TestPolicyTrace,
			fmt.Sprintf("cilium-dbg policy trace: Final verdict: ALLOWED — CNP not enforcing deny (%s→%s:8080/tcp)",
				cfg.TenantBNamespace, cfg.TenantANamespace),
			start, metrics)
	}
	if err != nil {
		return failResult(TestPolicyTrace,
			fmt.Sprintf("cilium-dbg policy trace failed: %v | output: %s", err, truncate(out, 120)),
			start, metrics)
	}

	// Ran successfully but no recognisable verdict — unexpected output.
	return failResult(TestPolicyTrace,
		fmt.Sprintf("cilium-dbg policy trace ran but 'Final verdict' not found in output: %s",
			truncate(out, 120)),
		start, metrics)
}

// runHubbleObserveFallback checks for DROPPED flows in Hubble's ring buffer as
// a secondary confirmation that the CNP is enforcing the deny rule.
//
// This is a fallback for environments where cilium-dbg policy trace is not
// available. It relies on prior traffic having been attempted (e.g. by Test 1).
func runHubbleObserveFallback(
	ctx context.Context,
	c KubectlClient,
	cfg Config,
	ciliumPod string,
	start time.Time,
	metrics map[string]string,
) TestResult {
	hubbleArgs := []string{
		"hubble", "observe",
		"--from-namespace", cfg.TenantBNamespace,
		"--to-namespace", cfg.TenantANamespace,
		"--verdict", "DROPPED",
		"--port", "8080",
		"--last", "100",
	}
	out, err := c.RunInPod(ctx, "", "kube-system", ciliumPod, "cilium-agent", hubbleArgs)
	metrics["hubble_output"] = truncate(out, 300)

	if err != nil {
		return skipResult(TestPolicyTrace,
			fmt.Sprintf("cilium-dbg policy trace unavailable; hubble observe also failed: %v", err),
			start)
	}
	if strings.Contains(out, "DROPPED") {
		return passResult(TestPolicyTrace,
			fmt.Sprintf("hubble observe: DROPPED flows found from %s→%s:8080/tcp (cilium-dbg fallback)",
				cfg.TenantBNamespace, cfg.TenantANamespace),
			start, metrics)
	}
	return failResult(TestPolicyTrace,
		fmt.Sprintf("hubble observe: no DROPPED flows found from %s→%s:8080/tcp — CNP may not be enforcing: %s",
			cfg.TenantBNamespace, cfg.TenantANamespace, truncate(out, 120)),
		start, metrics)
}

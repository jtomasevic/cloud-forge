// Package probe implements all six validation tests for the vCluster tenant isolation spike.
//
// The spike validates that vCluster-based per-tenant network isolation meets CloudForge's
// requirements before it is adopted as the production isolation primitive. Each test maps
// to a measurable success criterion defined in docs/3-Introduce-CF-VPC.md §11.5.
//
// Test inventory:
//
//   - Test 1 (isolation)    — cross-tenant traffic is topologically blocked
//   - Test 2 (speed)        — vCluster + NATS provisioning meets latency targets
//   - Test 3 (provisioner)  — platform kubeconfig can apply manifests; isolation is correct
//   - Test 4 (overhead)     — idle vCluster RAM < 300 MB, CPU < 100 m
//   - Test 5 (cilium)       — Cilium eBPF denies cross-namespace TCP
//   - Test 6 (recovery)     — vCluster API server restart < 60 s
//
// All tests accept a [KubectlClient] interface so they can be exercised in unit tests
// without a live cluster. The real implementation is [cluster.RealClient].
package probe

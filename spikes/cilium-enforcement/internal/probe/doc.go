// Package probe implements the five Cilium enforcement tests for the
// cilium-enforcement spike.
//
// Tests:
//   - cross_namespace_deny:   CiliumNetworkPolicy blocks TCP from tenant-B to tenant-A.
//   - intra_namespace_allow:  CiliumNetworkPolicy permits traffic within tenant-A.
//   - platform_isolation:     Tenant namespace cannot reach the cf-system platform namespace.
//   - policy_trace:           cilium policy trace confirms the DENY decision in the kernel.
//   - vcluster_coexistence:   CNP enforcement holds when a vCluster pod is in the host namespace.
//
// All five tests use the KubectlClient interface so the real implementation
// (shelling out to kubectl) can be swapped with FakeClient in unit tests.
package probe

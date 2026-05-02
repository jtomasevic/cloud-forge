package provisioner_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/provisioner"
)

// ── TenantIsolationPolicy ─────────────────────────────────────────────────────

func TestTenantIsolationPolicy_RendersValidYAML(t *testing.T) {
	out, err := provisioner.TenantIsolationPolicy("cilium-tenant-a")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	s := string(out)
	assert.Contains(t, s, "apiVersion: cilium.io/v2")
	assert.Contains(t, s, "kind: CiliumNetworkPolicy")
	assert.Contains(t, s, "name: tenant-isolation")
	assert.Contains(t, s, "namespace: cilium-tenant-a")
	assert.Contains(t, s, "endpointSelector: {}")
	// The same namespace must appear in fromEndpoints so only same-ns pods pass.
	assert.Contains(t, s, "io.kubernetes.pod.namespace: cilium-tenant-a")
}

func TestTenantIsolationPolicy_NamespaceAppearsExactlyTwice(t *testing.T) {
	ns := "my-tenant"
	out, err := provisioner.TenantIsolationPolicy(ns)
	require.NoError(t, err)

	// namespace must appear in metadata.namespace AND in fromEndpoints.matchLabels.
	count := strings.Count(string(out), ns)
	assert.Equal(t, 2, count, "namespace %q should appear twice in rendered CNP", ns)
}

func TestTenantIsolationPolicy_PolicyNameIsCorrect(t *testing.T) {
	out, err := provisioner.TenantIsolationPolicy("t1")
	require.NoError(t, err)
	assert.Contains(t, string(out), "name: tenant-isolation")
	assert.NotContains(t, string(out), "platform-isolation")
}

func TestTenantIsolationPolicy_DifferentNamespacesProduceDifferentOutputs(t *testing.T) {
	a, err := provisioner.TenantIsolationPolicy("tenant-a")
	require.NoError(t, err)
	b, err := provisioner.TenantIsolationPolicy("tenant-b")
	require.NoError(t, err)
	assert.NotEqual(t, string(a), string(b))
}

// ── PlatformIsolationPolicy ───────────────────────────────────────────────────

func TestPlatformIsolationPolicy_RendersValidYAML(t *testing.T) {
	out, err := provisioner.PlatformIsolationPolicy("cf-system")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	s := string(out)
	assert.Contains(t, s, "apiVersion: cilium.io/v2")
	assert.Contains(t, s, "kind: CiliumNetworkPolicy")
	assert.Contains(t, s, "name: platform-isolation")
	assert.Contains(t, s, "namespace: cf-system")
	assert.Contains(t, s, "io.kubernetes.pod.namespace: cf-system")
}

func TestPlatformIsolationPolicy_PolicyNameIsCorrect(t *testing.T) {
	out, err := provisioner.PlatformIsolationPolicy("cf-system")
	require.NoError(t, err)
	assert.Contains(t, string(out), "name: platform-isolation")
	assert.NotContains(t, string(out), "tenant-isolation")
}

// ── Namespace validation ──────────────────────────────────────────────────────

func TestIsolationPolicy_EmptyNamespaceErrors(t *testing.T) {
	_, err := provisioner.TenantIsolationPolicy("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	_, err = provisioner.PlatformIsolationPolicy("")
	assert.Error(t, err)
}

func TestIsolationPolicy_InvalidCharactersError(t *testing.T) {
	cases := []struct {
		ns   string
		desc string
	}{
		{"Tenant-A", "uppercase letter"},
		{"tenant_a", "underscore"},
		{"tenant a", "space"},
		{"tenant.a", "dot"},
		{"-tenant", "leading hyphen"},
		{"tenant-", "trailing hyphen"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := provisioner.TenantIsolationPolicy(tc.ns)
			assert.Error(t, err, "expected error for namespace %q (%s)", tc.ns, tc.desc)
		})
	}
}

func TestIsolationPolicy_ValidNamespacesSucceed(t *testing.T) {
	cases := []string{
		"cilium-tenant-a",
		"cf-system",
		"tenant1",
		"t",
		"a1b2c3",
		"my-very-long-but-valid-tenant-namespace-name",
	}
	for _, ns := range cases {
		t.Run(ns, func(t *testing.T) {
			out, err := provisioner.TenantIsolationPolicy(ns)
			require.NoError(t, err, "namespace %q should be valid", ns)
			assert.NotEmpty(t, out)
		})
	}
}

// ── Policy structure invariants ───────────────────────────────────────────────

// TestIsolationPolicy_SameNamespaceInFromEndpoints verifies the key invariant:
// the namespace in fromEndpoints.matchLabels must match metadata.namespace so
// that Cilium only allows intra-namespace traffic.
func TestIsolationPolicy_SameNamespaceInFromEndpoints(t *testing.T) {
	namespaces := []string{"cilium-tenant-a", "cf-system", "vcluster-pilot"}
	for _, ns := range namespaces {
		out, err := provisioner.TenantIsolationPolicy(ns)
		require.NoError(t, err)
		s := string(out)

		// Both occurrences of the namespace must be the same value.
		metaNS := "namespace: " + ns
		labelNS := "io.kubernetes.pod.namespace: " + ns
		assert.Contains(t, s, metaNS, "metadata.namespace must match for %q", ns)
		assert.Contains(t, s, labelNS, "fromEndpoints label must match for %q", ns)
	}
}

// TestIsolationPolicy_NoExplicitDenyRule verifies that we do not emit an
// explicit egress or ingress deny — Cilium's identity model makes this
// unnecessary and adding it could conflict with other policies.
func TestIsolationPolicy_NoExplicitDenyRule(t *testing.T) {
	out, err := provisioner.TenantIsolationPolicy("cilium-tenant-a")
	require.NoError(t, err)
	s := string(out)

	assert.NotContains(t, s, "deny")
	assert.NotContains(t, s, "egress") // baseline policy is ingress-only
}

// ── ProvisionerAccessPolicy ───────────────────────────────────────────────────

// TestProvisionerAccessPolicy_RendersValidYAML verifies that the rendered YAML
// contains all required fields for the provisioner-access CNP.
func TestProvisionerAccessPolicy_RendersValidYAML(t *testing.T) {
	out, err := provisioner.ProvisionerAccessPolicy("tenant-acme-corp")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	s := string(out)
	assert.Contains(t, s, "apiVersion: cilium.io/v2")
	assert.Contains(t, s, "kind: CiliumNetworkPolicy")
	assert.Contains(t, s, "name: provisioner-access")
	assert.Contains(t, s, "namespace: tenant-acme-corp")
	assert.Contains(t, s, "app: vcluster")
	assert.Contains(t, s, "io.kubernetes.pod.namespace: cf-system")
	assert.Contains(t, s, `port: "6443"`)
	assert.Contains(t, s, "protocol: TCP")
}

// TestProvisionerAccessPolicy_NamespaceIsInjected verifies that the namespace
// is injected into the metadata field of the rendered YAML.
func TestProvisionerAccessPolicy_NamespaceIsInjected(t *testing.T) {
	namespaces := []string{"tenant-acme", "tenant-beta-corp", "cf-system"}
	for _, ns := range namespaces {
		t.Run(ns, func(t *testing.T) {
			out, err := provisioner.ProvisionerAccessPolicy(ns)
			require.NoError(t, err)
			assert.Contains(t, string(out), "namespace: "+ns)
		})
	}
}

// TestProvisionerAccessPolicy_EmptyNamespaceErrors verifies that an empty
// namespace is rejected before any template rendering occurs.
func TestProvisionerAccessPolicy_EmptyNamespaceErrors(t *testing.T) {
	_, err := provisioner.ProvisionerAccessPolicy("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// TestProvisionerAccessPolicy_InvalidNamespaceErrors verifies that namespace
// validation is applied consistently for ProvisionerAccessPolicy.
func TestProvisionerAccessPolicy_InvalidNamespaceErrors(t *testing.T) {
	_, err := provisioner.ProvisionerAccessPolicy("UPPERCASE")
	require.Error(t, err)
}

// TestProvisionerAccessPolicy_SelectorTargetsVCluster verifies that the
// endpointSelector targets pods with the `app: vcluster` label, not all pods
// in the namespace. This prevents the policy from accidentally opening port
// 6443 on non-vCluster pods.
func TestProvisionerAccessPolicy_SelectorTargetsVCluster(t *testing.T) {
	out, err := provisioner.ProvisionerAccessPolicy("tenant-acme-corp")
	require.NoError(t, err)
	s := string(out)

	// Must select by app label, not empty selector.
	assert.Contains(t, s, "app: vcluster")
	assert.NotContains(t, s, "endpointSelector: {}")
}

// TestProvisionerAccessPolicy_SourceIsFromCFSystem verifies that the ingress
// source is scoped to cf-system pods — not all pods in the cluster.
// This is the narrowest permission that allows the provisioner to manage
// vClusters while respecting the tenant isolation boundary.
func TestProvisionerAccessPolicy_SourceIsFromCFSystem(t *testing.T) {
	out, err := provisioner.ProvisionerAccessPolicy("tenant-acme-corp")
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "io.kubernetes.pod.namespace: cf-system")
	// Must NOT use a wildcard or empty fromEndpoints.
	assert.NotContains(t, s, "fromEndpoints: []")
}

// TestProvisionerAccessPolicy_DifferentNamespacesProduceDifferentOutput verifies
// that policies for different tenants are not identical.
func TestProvisionerAccessPolicy_DifferentNamespacesProduceDifferentOutput(t *testing.T) {
	a, err := provisioner.ProvisionerAccessPolicy("tenant-alpha")
	require.NoError(t, err)
	b, err := provisioner.ProvisionerAccessPolicy("tenant-beta")
	require.NoError(t, err)
	assert.NotEqual(t, string(a), string(b))
}

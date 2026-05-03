//go:build integration

package provisioner_test

// End-to-end integration tests for the internal/provisioner package.
//
// These tests simulate the actual workflows executed by CF-Provisioner during
// tenant onboarding, operation, and deprovisioning. Each scenario exercises
// multiple provisioner functions together — CNP rendering AND kubeconfig
// storage — to validate the complete data flow rather than individual
// functions in isolation.
//
// # What "end-to-end" means at this level
//
// True end-to-end would include a running Kubernetes cluster, a Cilium agent
// applying the rendered CNP, and a live vCluster API server accepting the
// kubeconfig. That level of testing belongs to the cluster integration tests
// (spikes/cilium-enforcement and spikes/tenant-isolation).
//
// Here "end-to-end" means:
//   - Both package capabilities (CNP rendering + kubeconfig storage) are
//     exercised together in a single test flow.
//   - A real OpenBao server runs inside a Docker container (not a mock).
//   - The test sequence matches the actual provisioner call order.
//
// # Prerequisites
//
//   - Docker must be running on the host machine.
//     Testcontainers pulls and starts the OpenBao image automatically.
//     First run downloads ~50 MB; subsequent runs use the cached image.
//
//   - No Kubernetes cluster is needed.
//     CNP rendering is pure Go (text/template); no kubectl is called.
//
//   - No Cilium installation is needed.
//     The CNP YAML is validated structurally; it is not applied to a cluster.
//
//   - No vCluster installation is needed.
//     The kubeconfig stored in these tests is a static YAML fixture,
//     not a kubeconfig from a real cluster.
//
// # How to run
//
//	make provisioner-test-integration        # all integration tests + unit tests
//	make provisioner-coverage-integration    # same, with coverage report
//
// Or directly:
//
//	go test -tags=integration -v -run TestLifecycle ./internal/provisioner/...
//
// # Expected runtime
//
// Each test starts its own OpenBao container (~1–2 s startup after image is
// cached). Total suite runtime: ~15–30 s depending on Docker overhead.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/provisioner"
	"github.com/jtomasevic/cloud-forge/internal/testutil"
)

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 1: Full tenant lifecycle
//
// Simulates the exact call sequence CF-Provisioner executes when a new tenant
// signs up through the CloudForge UI or CLI:
//
//  1. Render the tenant isolation CNP (pure Go — happens before kubectl apply).
//  2. Store the vCluster kubeconfig in OpenBao (after vCluster is ready).
//  3. Retrieve the kubeconfig (at the start of each subsequent job).
//  4. Deprovision: Revoke the kubeconfig (hard delete from OpenBao).
//  5. Verify: Retrieve after Revoke returns ErrNotFound.
//
// Reference: docs/3-Introduce-CF-VPC.md §5.4 (onboarding flow end-to-end).
// ─────────────────────────────────────────────────────────────────────────────

func TestLifecycle_FullTenantOnboardingAndDeprovisioning(t *testing.T) {
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	const tenantID = "acme-corp"

	// ── Step 1: render the CiliumNetworkPolicy ────────────────────────────────
	// The provisioner renders the CNP YAML before applying it to the cluster.
	// We validate the output is structurally correct and namespace-scoped.
	cnpYAML, err := provisioner.TenantIsolationPolicy(tenantID)
	require.NoError(t, err, "step 1: TenantIsolationPolicy must not fail for a valid tenant ID")

	cnp := string(cnpYAML)
	assert.Contains(t, cnp, "kind: CiliumNetworkPolicy", "rendered YAML must be a CNP")
	assert.Contains(t, cnp, "namespace: "+tenantID, "CNP must be scoped to the tenant namespace")
	assert.Contains(t, cnp, "io.kubernetes.pod.namespace: "+tenantID,
		"CNP fromEndpoints must reference the same namespace (intra-tenant allow)")
	assert.NotContains(t, cnp, "deny", "baseline CNP must not contain an explicit deny rule")

	// ── Step 2: store the vCluster kubeconfig ────────────────────────────────
	// After the vCluster API server becomes ready, the provisioner receives a
	// kubeconfig and stores it in OpenBao for durability.
	const kubeconfigFixture = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://10.0.0.5:6443
  name: acme-corp
contexts:
- context:
    cluster: acme-corp
    user: cf-provisioner
  name: acme-corp
current-context: acme-corp
users:
- name: cf-provisioner
  user:
    token: eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.test
`
	err = provisioner.Store(ctx, client, tenantID, kubeconfigFixture)
	require.NoError(t, err, "step 2: Store must succeed")

	// ── Step 3: retrieve the kubeconfig at the start of a provisioning job ───
	// Every subsequent provisioning job (NATS setup, MinIO config, etc.) calls
	// Retrieve to get the connection details for the tenant's vCluster.
	retrieved, err := provisioner.Retrieve(ctx, client, tenantID)
	require.NoError(t, err, "step 3: Retrieve must succeed after Store")
	assert.Equal(t, kubeconfigFixture, retrieved,
		"step 3: retrieved kubeconfig must be byte-for-byte identical to what was stored")

	// ── Step 4: deprovision — revoke the kubeconfig ───────────────────────────
	// When the tenant is removed, Revoke hard-deletes all versions of the secret.
	err = provisioner.Revoke(ctx, client, tenantID)
	require.NoError(t, err, "step 4: Revoke must succeed")

	// ── Step 5: verify the kubeconfig is gone ────────────────────────────────
	_, err = provisioner.Retrieve(ctx, client, tenantID)
	require.Error(t, err, "step 5: Retrieve after Revoke must return an error")
	assert.True(t, errors.Is(err, provisioner.ErrNotFound),
		"step 5: error must be ErrNotFound, not a connectivity or auth failure; got: %v", err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 2: Platform namespace setup
//
// The platform namespace (cf-system) is isolated from tenants using a different
// CNP policy name ("platform-isolation" vs "tenant-isolation"). It does NOT
// receive a kubeconfig — cf-system is a host namespace managed directly by the
// platform team, not through a vCluster.
//
// This test validates:
//   - PlatformIsolationPolicy produces a correctly named CNP.
//   - The platform CNP and tenant CNPs are structurally distinct.
//   - No kubeconfig is stored for the platform namespace (it would be wrong to).
// ─────────────────────────────────────────────────────────────────────────────

func TestLifecycle_PlatformNamespaceSetup(t *testing.T) {
	// No OpenBao needed for this scenario — pure Go rendering only.
	const platformNS = "cf-system"

	// ── Platform CNP ──────────────────────────────────────────────────────────
	platformCNP, err := provisioner.PlatformIsolationPolicy(platformNS)
	require.NoError(t, err)

	pcnp := string(platformCNP)
	assert.Contains(t, pcnp, "name: platform-isolation",
		"platform CNP must use the platform-isolation policy name")
	assert.Contains(t, pcnp, "namespace: cf-system")
	assert.Contains(t, pcnp, "io.kubernetes.pod.namespace: cf-system")

	// ── Tenant CNP for comparison ─────────────────────────────────────────────
	tenantCNP, err := provisioner.TenantIsolationPolicy("some-tenant")
	require.NoError(t, err)

	tcnp := string(tenantCNP)
	assert.Contains(t, tcnp, "name: tenant-isolation",
		"tenant CNP must use the tenant-isolation policy name")

	// The two CNPs must use different policy names so kubectl can distinguish them.
	assert.NotEqual(t, pcnp, tcnp, "platform and tenant CNPs must differ")

	// ── Verify platform CNP does not contain tenant namespace ─────────────────
	assert.NotContains(t, pcnp, "some-tenant",
		"platform CNP must not reference any tenant namespace")
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 3: Concurrent tenant provisioning
//
// In production, the provisioner processes multiple onboarding requests
// concurrently (driven by ScyllaDB job queue + goroutine pool). This test
// provisions 5 tenants in parallel and verifies:
//
//   - No cross-contamination: each Retrieve returns exactly that tenant's data.
//   - No data races: the race detector (-race flag) must not report anything.
//   - CNP rendering is goroutine-safe (text/template with a pre-parsed template).
//   - OpenBao KV v2 handles concurrent writes to distinct paths correctly.
// ─────────────────────────────────────────────────────────────────────────────

func TestLifecycle_ConcurrentTenantProvisioning(t *testing.T) {
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	tenants := []struct {
		id         string
		kubeconfig string
	}{
		{"startup-alpha", "kubeconfig: alpha-cluster-token"},
		{"startup-beta", "kubeconfig: beta-cluster-token"},
		{"startup-gamma", "kubeconfig: gamma-cluster-token"},
		{"startup-delta", "kubeconfig: delta-cluster-token"},
		{"startup-epsilon", "kubeconfig: epsilon-cluster-token"},
	}

	// ── Phase 1: provision all tenants concurrently ───────────────────────────
	var wg sync.WaitGroup
	errors_ := make([]error, len(tenants))

	for i, tenant := range tenants {
		wg.Add(1)
		go func(idx int, id, kc string) {
			defer wg.Done()

			// Step A: render the CNP (pure Go — no I/O).
			cnpYAML, err := provisioner.TenantIsolationPolicy(id)
			if err != nil {
				errors_[idx] = err
				return
			}
			// Sanity: CNP must reference this tenant's namespace.
			if !strings.Contains(string(cnpYAML), "namespace: "+id) {
				errors_[idx] = fmt.Errorf("CNP namespace mismatch for tenant %q", id)
				return
			}

			// Step B: store the kubeconfig in OpenBao.
			if err := provisioner.Store(ctx, client, id, kc); err != nil {
				errors_[idx] = err
				return
			}
		}(i, tenant.id, tenant.kubeconfig)
	}
	wg.Wait()

	for i, err := range errors_ {
		require.NoError(t, err, "provisioning goroutine %d failed", i)
	}

	// ── Phase 2: retrieve and verify — no cross-contamination ─────────────────
	for _, tenant := range tenants {
		got, err := provisioner.Retrieve(ctx, client, tenant.id)
		require.NoError(t, err, "Retrieve failed for tenant %q", tenant.id)
		assert.Equal(t, tenant.kubeconfig, got,
			"tenant %q: retrieved wrong kubeconfig (cross-contamination?)", tenant.id)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 4: Zero-downtime kubeconfig rotation
//
// When a vCluster is upgraded or its admin service account is rotated, the
// provisioner must update the kubeconfig in OpenBao without causing a window
// where the kubeconfig is unavailable.
//
// OpenBao KV v2 is append-on-write: Store always creates a new version,
// Retrieve always returns the latest. The rotation workflow is:
//
//  1. Store new kubeconfig (version N+1 — available immediately).
//  2. Verify connectivity with the new kubeconfig (outside this package).
//  3. Revoke old service account token inside the vCluster (separate step).
//
// This test validates that:
//   - Step 1 is atomic: Retrieve after Store always returns the new version.
//   - The old value is not returned after a new Store.
//   - Multiple rotations work correctly.
// ─────────────────────────────────────────────────────────────────────────────

func TestLifecycle_ZeroDowntimeKubeconfigRotation(t *testing.T) {
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	const tenantID = "rotating-tenant"

	rotations := []string{
		"kubeconfig-v1-token-abc",
		"kubeconfig-v2-token-def",
		"kubeconfig-v3-token-ghi",
	}

	for version, kc := range rotations {
		// Store the new version.
		err := provisioner.Store(ctx, client, tenantID, kc)
		require.NoError(t, err, "rotation %d: Store must succeed", version+1)

		// Retrieve must immediately return the new version — not an older one.
		got, err := provisioner.Retrieve(ctx, client, tenantID)
		require.NoError(t, err, "rotation %d: Retrieve must succeed", version+1)
		assert.Equal(t, kc, got,
			"rotation %d: Retrieve must return the version just written, not an older one", version+1)

		// Confirm the old version is NOT returned.
		if version > 0 {
			assert.NotEqual(t, rotations[version-1], got,
				"rotation %d: Retrieve must not return the previous version", version+1)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 5: Deprovisioning after partial provisioning failure
//
// Provisioning is a multi-step workflow. Any step can fail (network error,
// vCluster timeout, OpenBao unavailability). When a failure occurs, the
// provisioner runs a cleanup sequence to leave the system in a consistent state.
//
// This test validates that the cleanup sequence is safe to run even when
// provisioning never reached the Store step:
//
//   - Revoke on a tenant that was never stored → no error (idempotent).
//   - After cleanup, TenantIsolationPolicy can be called again for a retry
//     → the CNP template is stateless, always succeeds for a valid namespace.
//   - A successful retry Store followed by Retrieve works correctly.
//
// This models the "retry after failure" pattern in the provisioner's
// distributed job queue (ScyllaDB LWT deduplication).
// ─────────────────────────────────────────────────────────────────────────────

func TestLifecycle_DeprovisioningAfterPartialFailure(t *testing.T) {
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	const tenantID = "failed-onboarding-tenant"

	// ── Simulate: provisioning started but failed before Store was called ─────
	// (In production: vCluster never became ready, kubeconfig was never obtained.)

	// ── Cleanup sequence — must be idempotent ─────────────────────────────────
	// The provisioner's deprovisioning handler always runs Revoke, regardless of
	// whether Store was ever called. This must not fail.
	err := provisioner.Revoke(ctx, client, tenantID)
	assert.NoError(t, err,
		"cleanup: Revoke on a never-provisioned tenant must not return an error")

	// Running Revoke a second time (e.g. duplicate cleanup job) must also be safe.
	err = provisioner.Revoke(ctx, client, tenantID)
	assert.NoError(t, err,
		"cleanup: second Revoke must also be idempotent")

	// ── Retry: re-run provisioning after cleanup ──────────────────────────────
	// The CNP template is pure Go and stateless — it always produces the same
	// output for the same input, regardless of previous failures.
	cnpYAML, err := provisioner.TenantIsolationPolicy(tenantID)
	require.NoError(t, err, "retry: TenantIsolationPolicy must succeed after failed provisioning attempt")
	assert.Contains(t, string(cnpYAML), "namespace: "+tenantID,
		"retry: CNP must still reference the correct namespace")

	// Simulate successful retry: vCluster is now ready, kubeconfig obtained.
	const retryKubeconfig = "kubeconfig: retry-success-token"
	err = provisioner.Store(ctx, client, tenantID, retryKubeconfig)
	require.NoError(t, err, "retry: Store must succeed on second attempt")

	got, err := provisioner.Retrieve(ctx, client, tenantID)
	require.NoError(t, err, "retry: Retrieve must succeed after successful re-provisioning")
	assert.Equal(t, retryKubeconfig, got,
		"retry: retrieved kubeconfig must match what was stored on the second attempt")
}

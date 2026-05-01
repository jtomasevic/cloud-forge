//go:build integration

package provisioner_test

// Integration tests for kubeconfig.go.
//
// These tests require Docker and start a real OpenBao container via
// internal/testutil.StartOpenBao(t). They validate:
//
//   - Store → Retrieve roundtrip (data fidelity)
//   - Retrieve for a missing path returns ErrNotFound
//   - Revoke removes all secret versions (hard delete)
//   - Revoke is idempotent on a non-existent tenant
//   - Store is idempotent (overwrites to a new KV v2 version)
//   - Cross-tenant path isolation (scoped OpenBao policy + token)
//
// Run with:
//
//	make provisioner-test-integration
//
// Coverage for the integration run (includes paths only exercisable with a
// real OpenBao server):
//
//	make provisioner-coverage-integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	openbao "github.com/openbao/openbao/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/provisioner"
	"github.com/jtomasevic/cloud-forge/internal/testutil"
)

// sampleKubeconfig is a minimal but structurally valid kubeconfig YAML used
// across all tests. It does not point to a real cluster.
const sampleKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: tenant-test
contexts:
- context:
    cluster: tenant-test
    user: provisioner
  name: tenant-test
current-context: tenant-test
users:
- name: provisioner
  user:
    token: test-token-abc123
`

// ── Store + Retrieve ──────────────────────────────────────────────────────────

func TestStore_And_Retrieve_Roundtrip(t *testing.T) {
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	const tenantID = "acme-corp"

	require.NoError(t, provisioner.Store(ctx, client, tenantID, sampleKubeconfig))

	got, err := provisioner.Retrieve(ctx, client, tenantID)
	require.NoError(t, err)
	assert.Equal(t, sampleKubeconfig, got, "retrieved kubeconfig must match stored value byte-for-byte")
}

func TestStore_Overwrites_To_New_KV_Version(t *testing.T) {
	// KV v2 is versioned: each Store creates a new version. Retrieve always
	// returns the latest version. This validates the rotation pattern: write
	// the new kubeconfig, verify it is returned, without losing old versions.
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	const tenantID = "rotation-test"
	const v1 = "kubeconfig-version-one"
	const v2 = "kubeconfig-version-two"

	require.NoError(t, provisioner.Store(ctx, client, tenantID, v1))
	require.NoError(t, provisioner.Store(ctx, client, tenantID, v2))

	got, err := provisioner.Retrieve(ctx, client, tenantID)
	require.NoError(t, err)
	assert.Equal(t, v2, got, "Retrieve must return the latest stored version")
}

func TestStore_MultiTenant_Isolation(t *testing.T) {
	// Storing kubeconfigs for multiple tenants must not interfere with each other.
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	tenants := map[string]string{
		"alpha-tenant": "kubeconfig-alpha",
		"beta-tenant":  "kubeconfig-beta",
		"gamma-tenant": "kubeconfig-gamma",
	}

	for id, kc := range tenants {
		require.NoError(t, provisioner.Store(ctx, client, id, kc))
	}

	for id, wantKC := range tenants {
		got, err := provisioner.Retrieve(ctx, client, id)
		require.NoError(t, err, "tenant %q: Retrieve failed", id)
		assert.Equal(t, wantKC, got, "tenant %q: wrong kubeconfig returned", id)
	}
}

// ── Retrieve — not found ──────────────────────────────────────────────────────

func TestRetrieve_ReturnsErrNotFound_For_New_Tenant(t *testing.T) {
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	_, err := provisioner.Retrieve(ctx, client, "never-provisioned")
	require.Error(t, err)
	assert.True(t, errors.Is(err, provisioner.ErrNotFound),
		"expected ErrNotFound, got: %v", err)
}

func TestRetrieve_ReturnsErrNotFound_After_Revoke(t *testing.T) {
	// After Revoke, Retrieve must return ErrNotFound, not the old kubeconfig.
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	const tenantID = "deprovisioned-tenant"

	require.NoError(t, provisioner.Store(ctx, client, tenantID, sampleKubeconfig))
	require.NoError(t, provisioner.Revoke(ctx, client, tenantID))

	_, err := provisioner.Retrieve(ctx, client, tenantID)
	assert.True(t, errors.Is(err, provisioner.ErrNotFound),
		"after Revoke, Retrieve must return ErrNotFound, got: %v", err)
}

// ── Revoke ────────────────────────────────────────────────────────────────────

func TestRevoke_Is_Idempotent(t *testing.T) {
	// Revoking a tenant that was never provisioned must not return an error.
	// This is critical for deprovisioning workflows that call Revoke in cleanup
	// sequences even when provisioning failed before Store was called.
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	err := provisioner.Revoke(ctx, client, "tenant-that-was-never-stored")
	assert.NoError(t, err, "Revoke on non-existent tenant must be idempotent")
}

func TestRevoke_Twice_Is_Idempotent(t *testing.T) {
	// Revoking the same tenant twice must not fail.
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	const tenantID = "double-revoke-tenant"

	require.NoError(t, provisioner.Store(ctx, client, tenantID, sampleKubeconfig))
	require.NoError(t, provisioner.Revoke(ctx, client, tenantID))
	require.NoError(t, provisioner.Revoke(ctx, client, tenantID),
		"second Revoke on already-revoked tenant must not error")
}

// ── Cross-tenant isolation (OpenBao policy scoping) ───────────────────────────

// TestCrossTenant_PolicyIsolation validates the security claim from
// docs/3-Introduce-CF-VPC.md §5.3: the provisioner receives a token scoped
// to a single tenant's path and cannot read any other tenant's kubeconfig.
//
// How this works:
//  1. Root client stores kubeconfigs for two tenants (simulates provisioning).
//  2. Root client creates an OpenBao policy granting access ONLY to tenant-a's
//     secret path (secret/data/cf/tenants/corp-a/*).
//  3. Root client creates a token with only that policy.
//  4. A new client is created with the scoped token.
//  5. The scoped client can Retrieve tenant-a's kubeconfig.
//  6. The scoped client CANNOT Retrieve tenant-b's kubeconfig — permission denied.
//
// This simulates the production model where the provisioner pod authenticates
// to OpenBao via Kubernetes auth and receives a per-tenant scoped token.
func TestCrossTenant_PolicyIsolation(t *testing.T) {
	rootClient, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	const (
		tenantA = "corp-a"
		tenantB = "corp-b"
	)

	// Step 1: store kubeconfigs for both tenants using the root client.
	require.NoError(t, provisioner.Store(ctx, rootClient, tenantA, "kubeconfig-for-corp-a"))
	require.NoError(t, provisioner.Store(ctx, rootClient, tenantB, "kubeconfig-for-corp-b"))

	// Step 2: create an OpenBao policy that restricts access to tenant-a only.
	// The path "secret/data/..." is the internal KV v2 data path (the KVv2 helper
	// prepends "data/" to the logical path automatically).
	policyHCL := fmt.Sprintf(`
path "secret/data/cf/tenants/%s/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "secret/metadata/cf/tenants/%s/*" {
  capabilities = ["read", "delete", "list"]
}`, tenantA, tenantA)

	const policyName = "corp-a-provisioner"
	require.NoError(t, rootClient.Sys().PutPolicy(policyName, policyHCL),
		"failed to create scoped policy")

	// Step 3: create a token with only the tenant-a policy.
	tokenSecret, err := rootClient.Auth().Token().Create(&openbao.TokenCreateRequest{
		Policies:  []string{policyName},
		TTL:       "1h",
		Renewable: boolPtr(false),
	})
	require.NoError(t, err, "failed to create scoped token")
	scopedToken := tokenSecret.Auth.ClientToken

	// Step 4: create a new OpenBao client using the scoped token.
	scopedClient, err := openbao.NewClient(rootClient.CloneConfig())
	require.NoError(t, err)
	scopedClient.SetToken(scopedToken)

	// Step 5: the scoped client can read tenant-a's kubeconfig.
	gotA, err := provisioner.Retrieve(ctx, scopedClient, tenantA)
	require.NoError(t, err, "scoped client must be able to read its own tenant's kubeconfig")
	assert.Equal(t, "kubeconfig-for-corp-a", gotA)

	// Step 6: the scoped client cannot read tenant-b's kubeconfig.
	// OpenBao returns 403 Forbidden — the SDK wraps this as a non-nil error,
	// distinct from ErrNotFound (404). We verify it is an error and specifically
	// NOT ErrNotFound (which would imply we just missed the secret, not that
	// access was denied).
	_, errB := provisioner.Retrieve(ctx, scopedClient, tenantB)
	require.Error(t, errB,
		"scoped client must NOT be able to read another tenant's kubeconfig")
	assert.False(t, errors.Is(errB, provisioner.ErrNotFound),
		"access to tenant-b must fail with permission denied (403), not not-found (404)")
}

// ── Retrieve — malformed secret ───────────────────────────────────────────────

func TestRetrieve_ErrorsWhenKubeconfigFieldMissing(t *testing.T) {
	// If a secret exists at the expected path but lacks the "kubeconfig" field
	// (e.g. written manually with the wrong key), Retrieve must return a clear
	// error rather than an empty string or a panic.
	client, _ := testutil.StartOpenBao(t)
	ctx := context.Background()

	// Write a secret with the correct path but the wrong field name.
	_, err := client.KVv2("secret").Put(ctx, "cf/tenants/malformed-tenant/kubeconfig",
		map[string]interface{}{
			"wrong-field": "some-value", // intentionally wrong — kubeconfig key is absent
		})
	require.NoError(t, err, "pre-condition: write malformed secret")

	_, err = provisioner.Retrieve(ctx, client, "malformed-tenant")
	require.Error(t, err)
	assert.False(t, errors.Is(err, provisioner.ErrNotFound),
		"missing field must not look like ErrNotFound — it is a data integrity problem")
	assert.Contains(t, err.Error(), "missing or empty",
		"error message should describe the missing field")
}

// boolPtr is a helper that returns a pointer to the given bool value.
// The OpenBao token creation API requires pointer fields for optional booleans.
func boolPtr(b bool) *bool { return &b }

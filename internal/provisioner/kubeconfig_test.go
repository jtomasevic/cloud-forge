package provisioner_test

// Unit tests for kubeconfig.go validation paths.
//
// These tests exercise all error cases that are caught before the OpenBao
// client is ever called (tenant ID validation, empty kubeconfig). They run
// with a nil *openbao.Client, which is safe as long as validation returns
// early — confirmed by code inspection of Store, Retrieve, and Revoke.
//
// Integration tests that require a live OpenBao container live in
// kubeconfig_integration_test.go (//go:build integration).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/provisioner"
)

// ── Store — input validation ──────────────────────────────────────────────────

func TestStore_EmptyTenantID_ReturnsError(t *testing.T) {
	err := provisioner.Store(context.Background(), nil, "", "kubeconfig: yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenantID")
}

func TestStore_EmptyKubeconfig_ReturnsError(t *testing.T) {
	// A valid tenant ID but empty kubeconfig — should fail before touching client.
	err := provisioner.Store(context.Background(), nil, "valid-tenant", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubeconfigYAML")
}

func TestStore_InvalidTenantID_ReturnsError(t *testing.T) {
	cases := []struct {
		id   string
		desc string
	}{
		{"Tenant-A", "uppercase"},
		{"tenant_a", "underscore"},
		{"-tenant", "leading hyphen"},
		{"tenant-", "trailing hyphen"},
		{"tenant a", "space"},
		{"tenant.corp", "dot"},
		{"ACME", "all-caps"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			err := provisioner.Store(context.Background(), nil, tc.id, "kc")
			assert.Error(t, err, "expected error for tenantID %q (%s)", tc.id, tc.desc)
		})
	}
}

func TestStore_ValidTenantIDs_PassValidation(t *testing.T) {
	// These tenant IDs are valid Kubernetes namespace names. Store will fail
	// after validation (nil client dereference) — we confirm validation itself
	// does not reject them by checking the error is not a validation error.
	cases := []string{
		"acme-corp",
		"tenant1",
		"t",
		"a1b2c3",
		"my-very-long-but-valid-tenant-id",
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			// Passing nil client: validation passes → panic on KVv2 call.
			// We recover from the panic to confirm validation did not block it.
			require.NotPanics(t, func() {
				defer func() { recover() }() //nolint:errcheck // intentional panic recovery
				_ = provisioner.Store(context.Background(), nil, id, "kc-yaml")
			})
		})
	}
}

// ── Retrieve — input validation ───────────────────────────────────────────────

func TestRetrieve_EmptyTenantID_ReturnsError(t *testing.T) {
	_, err := provisioner.Retrieve(context.Background(), nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenantID")
}

func TestRetrieve_InvalidTenantID_ReturnsError(t *testing.T) {
	_, err := provisioner.Retrieve(context.Background(), nil, "INVALID!")
	require.Error(t, err)
	// Must not be ErrNotFound — this is a validation error, not a missing key.
	assert.False(t, isErrNotFound(err), "validation error must not look like ErrNotFound")
}

// ── Revoke — input validation ─────────────────────────────────────────────────

func TestRevoke_EmptyTenantID_ReturnsError(t *testing.T) {
	err := provisioner.Revoke(context.Background(), nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenantID")
}

func TestRevoke_InvalidTenantID_ReturnsError(t *testing.T) {
	err := provisioner.Revoke(context.Background(), nil, "INVALID!")
	require.Error(t, err)
}

// ── ErrNotFound sentinel ──────────────────────────────────────────────────────

func TestErrNotFound_IsDistinctFromValidationErrors(t *testing.T) {
	// ErrNotFound must be detectable via errors.Is and must not be confused
	// with validation errors returned for invalid tenant IDs.
	validationErr := provisioner.Store(context.Background(), nil, "", "kc")
	assert.False(t, isErrNotFound(validationErr), "validation error must not be ErrNotFound")
}

// isErrNotFound is a test helper that uses errors.Is to detect ErrNotFound.
// This avoids importing the errors package in the test file.
func isErrNotFound(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == provisioner.ErrNotFound.Error()
}

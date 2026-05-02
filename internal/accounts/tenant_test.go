package accounts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTenantStatusConstants verifies that TenantStatus constants have the
// expected string values. These values are stored in ScyllaDB and must match
// the CQL schema comment exactly (changing them would require a data migration).
func TestTenantStatusConstants(t *testing.T) {
	assert.Equal(t, TenantStatus("PROVISIONING"), TenantStatusProvisioning)
	assert.Equal(t, TenantStatus("ACTIVE"), TenantStatusActive)
	assert.Equal(t, TenantStatus("SUSPENDED"), TenantStatusSuspended)
	assert.Equal(t, TenantStatus("DELETED"), TenantStatusDeleted)
}

// TestErrTenantNotFound_IsDistinctFromErrTenantAlreadyExists verifies that
// the two sentinel errors are not equal so that callers can distinguish them
// using errors.Is.
func TestErrTenantNotFound_IsDistinctFromErrTenantAlreadyExists(t *testing.T) {
	assert.NotEqual(t, ErrTenantNotFound, ErrTenantAlreadyExists)
	assert.NotNil(t, ErrTenantNotFound)
	assert.NotNil(t, ErrTenantAlreadyExists)
}

// TestErrTenantNotFound_ErrorMessage verifies that the sentinel errors have
// human-readable messages that include the package prefix for discoverability.
func TestErrTenantNotFound_ErrorMessage(t *testing.T) {
	assert.Contains(t, ErrTenantNotFound.Error(), "accounts")
	assert.Contains(t, ErrTenantAlreadyExists.Error(), "accounts")
}

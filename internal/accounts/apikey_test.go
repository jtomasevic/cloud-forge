package accounts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAPIKeyStatusConstants verifies that the APIKeyStatus constants have the
// expected string values that are stored in ScyllaDB.
func TestAPIKeyStatusConstants(t *testing.T) {
	assert.Equal(t, APIKeyStatus("ACTIVE"), APIKeyStatusActive)
	assert.Equal(t, APIKeyStatus("ROTATING"), APIKeyStatusRotating)
	assert.Equal(t, APIKeyStatus("REVOKED"), APIKeyStatusRevoked)
}

// TestErrAPIKeyNotFound_HasMessage verifies the sentinel error message contains
// the package prefix so that log messages are easily discoverable.
func TestErrAPIKeyNotFound_HasMessage(t *testing.T) {
	assert.Contains(t, ErrAPIKeyNotFound.Error(), "accounts")
	assert.Contains(t, ErrAPIKeyNotFound.Error(), "not found")
}

// TestErrAPIKeyNotFound_IsDistinctFromTenantErrors verifies that API key errors
// and tenant errors are not the same sentinel, preventing accidental confusion
// in error handling paths.
func TestErrAPIKeyNotFound_IsDistinctFromTenantErrors(t *testing.T) {
	assert.NotEqual(t, ErrAPIKeyNotFound, ErrTenantNotFound)
	assert.NotEqual(t, ErrAPIKeyNotFound, ErrTenantAlreadyExists)
}

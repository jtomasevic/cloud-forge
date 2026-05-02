package accounts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestJobStatusConstants verifies that JobStatus constants match the CQL
// schema values. Changing these strings would require a ScyllaDB migration.
func TestJobStatusConstants(t *testing.T) {
	assert.Equal(t, JobStatus("QUEUED"), JobStatusQueued)
	assert.Equal(t, JobStatus("PROVISIONING"), JobStatusProvisioning)
	assert.Equal(t, JobStatus("READY"), JobStatusReady)
	assert.Equal(t, JobStatus("FAILED"), JobStatusFailed)
}

// TestJobOperationConstants verifies that JobOperation constants match the
// expected CQL-stored values.
func TestJobOperationConstants(t *testing.T) {
	assert.Equal(t, JobOperation("PROVISION_VPC"), JobOperationProvisionVPC)
	assert.Equal(t, JobOperation("DEPROVISION_VPC"), JobOperationDeprovisionVPC)
}

// TestErrJobNotFound_HasMessage verifies the sentinel error carries the
// expected package prefix and "not found" keywords.
func TestErrJobNotFound_HasMessage(t *testing.T) {
	assert.Contains(t, ErrJobNotFound.Error(), "accounts")
	assert.Contains(t, ErrJobNotFound.Error(), "not found")
}

// TestErrJobNotFound_IsDistinctFromOtherErrors verifies that job errors are
// not confused with tenant or API key errors at the call site.
func TestErrJobNotFound_IsDistinctFromOtherErrors(t *testing.T) {
	assert.NotEqual(t, ErrJobNotFound, ErrTenantNotFound)
	assert.NotEqual(t, ErrJobNotFound, ErrAPIKeyNotFound)
}

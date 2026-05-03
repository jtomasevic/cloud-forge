package provisioner_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/jtomasevic/cloud-forge/services/provisioner"
	svc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// ── ToServiceProvisionParams ──────────────────────────────────────────────────

func TestToServiceProvisionParams_MapsFields(t *testing.T) {
	req := ProvisionRequest{
		TenantID:    "acme-corp",
		DisplayName: "Acme Corporation",
		Plan:        "starter",
	}

	params := req.ToServiceProvisionParams()

	// TenantID from the REST layer maps to TenantSlug in the service layer.
	// DisplayName and Plan are ignored — the workflow fetches them from the DB.
	assert.Equal(t, "acme-corp", params.TenantSlug)
}

// ── ToGeneratedJobResponse ────────────────────────────────────────────────────

func TestToGeneratedJobResponse_QueuedJob_NoVPCResult(t *testing.T) {
	jobID := uuid.New()
	result := svc.JobResult{
		JobID:  jobID,
		Status: "QUEUED",
	}

	resp := ToGeneratedJobResponse(result)

	assert.Equal(t, jobID, resp.JobId)
	assert.Nil(t, resp.ApiKey)
	assert.Nil(t, resp.VpcInfo)
}

func TestToGeneratedJobResponse_WithErrorMessage(t *testing.T) {
	jobID := uuid.New()
	errMsg := "step 3: vcluster timed out"
	result := svc.JobResult{
		JobID:        jobID,
		Status:       "FAILED",
		ErrorMessage: errMsg,
	}

	resp := ToGeneratedJobResponse(result)

	require.NotNil(t, resp.ErrorMessage)
	assert.Equal(t, errMsg, *resp.ErrorMessage)
}

func TestToGeneratedJobResponse_ReadyJob_WithVPCResult(t *testing.T) {
	jobID := uuid.New()
	apiKeyID := uuid.New()
	result := svc.JobResult{
		JobID:  jobID,
		Status: "READY",
		VPCResult: &svc.VPCProvisionResult{
			PodCIDR:  "10.100.1.0/24",
			SvcCIDR:  "10.200.1.0/24",
			APIKey:   "cf_live_secret",
			APIKeyID: apiKeyID,
		},
	}

	resp := ToGeneratedJobResponse(result)

	require.NotNil(t, resp.ApiKey)
	require.NotNil(t, resp.ApiKeyId)
	require.NotNil(t, resp.VpcInfo)
	assert.Equal(t, "cf_live_secret", *resp.ApiKey)
	assert.Equal(t, apiKeyID, *resp.ApiKeyId)
	assert.Equal(t, "10.100.1.0/24", resp.VpcInfo.PodCidr)
	assert.Equal(t, "10.200.1.0/24", resp.VpcInfo.ServiceCidr)
	assert.True(t, resp.VpcInfo.VclusterReady)
}

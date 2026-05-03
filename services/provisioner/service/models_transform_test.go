package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// ── ToJobResultFromAccountsJob ────────────────────────────────────────────────

func TestToJobResultFromAccountsJob_Queued(t *testing.T) {
	jobID := uuid.New()
	job := &accounts.ProvisioningJob{
		JobID:  jobID,
		Status: accounts.JobStatusQueued,
	}

	result := ToJobResultFromAccountsJob(job)

	assert.Equal(t, jobID, result.JobID)
	assert.Equal(t, string(accounts.JobStatusQueued), result.Status)
	assert.Nil(t, result.VPCResult)
}

func TestToJobResultFromAccountsJob_FailedWithMessage(t *testing.T) {
	job := &accounts.ProvisioningJob{
		JobID:        uuid.New(),
		Status:       accounts.JobStatusFailed,
		ErrorMessage: "step 4: create vCluster: timeout",
	}

	result := ToJobResultFromAccountsJob(job)

	assert.Equal(t, string(accounts.JobStatusFailed), result.Status)
	assert.Equal(t, "step 4: create vCluster: timeout", result.ErrorMessage)
	assert.Nil(t, result.VPCResult)
}

func TestToJobResultFromAccountsJob_Ready_ParsesVPCResult(t *testing.T) {
	apiKeyID := uuid.New()
	blob, err := json.Marshal(map[string]any{
		"api_key":    "cf_live_secret123",
		"api_key_id": apiKeyID.String(),
		"vpc_info": map[string]any{
			"pod_cidr":     "10.100.2.0/24",
			"service_cidr": "10.200.2.0/24",
		},
	})
	require.NoError(t, err)

	job := &accounts.ProvisioningJob{
		JobID:  uuid.New(),
		Status: accounts.JobStatusReady,
		Result: string(blob),
	}

	result := ToJobResultFromAccountsJob(job)

	require.NotNil(t, result.VPCResult)
	assert.Equal(t, "cf_live_secret123", result.VPCResult.APIKey)
	assert.Equal(t, apiKeyID, result.VPCResult.APIKeyID)
	assert.Equal(t, "10.100.2.0/24", result.VPCResult.PodCIDR)
	assert.Equal(t, "10.200.2.0/24", result.VPCResult.SvcCIDR)
}

func TestToJobResultFromAccountsJob_Ready_EmptyResult_NilVPCResult(t *testing.T) {
	job := &accounts.ProvisioningJob{
		JobID:  uuid.New(),
		Status: accounts.JobStatusReady,
		Result: "", // empty blob → no VPCResult
	}

	result := ToJobResultFromAccountsJob(job)

	assert.Nil(t, result.VPCResult, "empty result blob must yield nil VPCResult")
}

func TestToJobResultFromAccountsJob_Ready_InvalidJSON_NilVPCResult(t *testing.T) {
	job := &accounts.ProvisioningJob{
		JobID:  uuid.New(),
		Status: accounts.JobStatusReady,
		Result: "not-json",
	}

	result := ToJobResultFromAccountsJob(job)

	assert.Nil(t, result.VPCResult, "invalid JSON must yield nil VPCResult")
}

// ── ToAccountsJobOperation ────────────────────────────────────────────────────

func TestToAccountsJobOperation_ProvisionVPC(t *testing.T) {
	op := ToAccountsJobOperation("PROVISION_VPC")
	assert.Equal(t, accounts.JobOperationProvisionVPC, op)
}

func TestToAccountsJobOperation_DeprovisionVPC(t *testing.T) {
	op := ToAccountsJobOperation("DEPROVISION_VPC")
	assert.Equal(t, accounts.JobOperationDeprovisionVPC, op)
}

func TestToAccountsJobOperation_UnknownDefaultsToProvision(t *testing.T) {
	op := ToAccountsJobOperation("UNKNOWN_OP")
	assert.Equal(t, accounts.JobOperationProvisionVPC, op)
}

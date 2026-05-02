package service

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// ToJobResultFromAccountsJob converts an accounts.ProvisioningJob (DB layer
// model) into the service-level JobResult.
//
// When the job is READY the result JSON blob is parsed to reconstruct
// VPCProvisionResult. If parsing fails the VPCResult is left nil — callers
// should treat a nil VPCResult on a READY job as a non-fatal data issue
// rather than a hard error.
func ToJobResultFromAccountsJob(j *accounts.ProvisioningJob) JobResult {
	r := JobResult{
		JobID:        j.JobID,
		Status:       string(j.Status),
		ErrorMessage: j.ErrorMessage,
	}
	if j.Status == accounts.JobStatusReady && j.Result != "" {
		r.VPCResult = parseVPCResult(j.Result)
	}
	return r
}

// parseVPCResult unmarshals the JSON blob stored in cf.provisioning_jobs.result
// into a VPCProvisionResult. Returns nil on any parse error.
func parseVPCResult(blob string) *VPCProvisionResult {
	var raw struct {
		APIKey   string    `json:"api_key"`
		APIKeyID uuid.UUID `json:"api_key_id"`
		VPCInfo  struct {
			PodCIDR string `json:"pod_cidr"`
			SvcCIDR string `json:"service_cidr"`
		} `json:"vpc_info"`
	}
	if err := json.Unmarshal([]byte(blob), &raw); err != nil {
		return nil
	}
	return &VPCProvisionResult{
		PodCIDR:  raw.VPCInfo.PodCIDR,
		SvcCIDR:  raw.VPCInfo.SvcCIDR,
		APIKey:   raw.APIKey,
		APIKeyID: raw.APIKeyID,
	}
}

// ToAccountsJobOperation maps the service-level operation string to the
// accounts layer constant. Used when enqueueing a new job.
func ToAccountsJobOperation(op string) accounts.JobOperation {
	switch op {
	case "PROVISION_VPC":
		return accounts.JobOperationProvisionVPC
	case "DEPROVISION_VPC":
		return accounts.JobOperationDeprovisionVPC
	default:
		return accounts.JobOperationProvisionVPC
	}
}

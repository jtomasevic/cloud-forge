package provisioner

import (
	"github.com/jtomasevic/cloud-forge/services/provisioner/generated"
	svc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// ToServiceProvisionParams converts the REST ProvisionRequest into the
// service-layer ProvisionParams. No REST types cross into the service layer.
func (r ProvisionRequest) ToServiceProvisionParams() svc.ProvisionParams {
	return svc.ProvisionParams{
		TenantID:    r.TenantID,
		DisplayName: r.DisplayName,
		Plan:        svc.Plan(r.Plan),
	}
}

// ToGeneratedJobResponse converts a service-layer JobResult into the
// generated JobResponse type understood by oapi-codegen's strict handler.
func ToGeneratedJobResponse(j svc.JobResult) generated.JobResponse {
	status := generated.JobStatus(j.Status)
	resp := generated.JobResponse{
		JobId:  j.JobID, // openapi_types.UUID is a type alias for uuid.UUID
		Status: status,
	}
	if j.ErrorMessage != "" {
		resp.ErrorMessage = &j.ErrorMessage
	}
	if j.VPCResult != nil {
		apiKey := j.VPCResult.APIKey
		apiKeyID := j.VPCResult.APIKeyID // openapi_types.UUID = uuid.UUID
		resp.ApiKey = &apiKey
		resp.ApiKeyId = &apiKeyID
		resp.VpcInfo = &generated.VPCInfo{
			PodCidr:       j.VPCResult.PodCIDR,
			ServiceCidr:   j.VPCResult.SvcCIDR,
			VclusterReady: true,
		}
	}
	return resp
}

package service

import "github.com/google/uuid"

// Plan is the billing tier requested at provisioning time.
type Plan string

const (
	PlanStarter    Plan = "starter"
	PlanPro        Plan = "pro"
	PlanEnterprise Plan = "enterprise"
)

// ProvisionParams carries the validated, transport-free inputs for the
// 10-step VPC provisioning workflow.
// It must never reference any generated REST types.
type ProvisionParams struct {
	TenantID    string
	DisplayName string
	Plan        Plan
}

// VPCProvisionResult carries the network topology and the initial API key
// produced once provisioning reaches READY status.
// The RawAPIKey is generated once and never stored — it must be consumed
// by the caller and returned to the tenant on the first successful job poll.
type VPCProvisionResult struct {
	PodCIDR   string
	SvcCIDR   string
	APIKey    string // raw key: cf_live_... — present once, then lost
	APIKeyID  uuid.UUID
}

// JobResult is the service-level representation of a provisioning job's
// current state and terminal output.
// The REST layer maps this into the generated JobResponse type.
type JobResult struct {
	JobID        uuid.UUID
	Status       string
	ErrorMessage string
	// VPCResult is non-nil only when Status is "READY" for a provision job.
	VPCResult *VPCProvisionResult
}

// DeprovisionParams carries the validated inputs for the teardown workflow.
type DeprovisionParams struct {
	TenantSlug string
}

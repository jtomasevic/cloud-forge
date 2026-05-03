package service

import (
	"time"

	"github.com/google/uuid"
)

// Plan mirrors the billing tier enum from the OpenAPI spec.
// Defined here so the REST layer never leaks generated types into the service.
type Plan string

// Available billing plan tiers.
const (
	PlanStarter    Plan = "starter"
	PlanPro        Plan = "pro"
	PlanEnterprise Plan = "enterprise"
)

// ProvisionNetworkParams carries the inputs for starting VPC provisioning on
// an existing tenant account. The tenant must have been created via Register.
type ProvisionNetworkParams struct {
	TenantSlug string
}

// AccountResult is the service-layer view of a tenant account record.
// The REST layer maps this into the generated AccountResponse type.
type AccountResult struct {
	CreatedAt   time.Time
	Slug        string
	DisplayName string
	Status      string
	Plan        string
	PodCIDR     string
	ServiceCIDR string
	TenantID    uuid.UUID
}

// ProvisionNetworkResult is returned by ProvisionNetwork: the tenant slug plus
// the provisioning job ID the caller should poll for progress.
type ProvisionNetworkResult struct {
	Slug     string
	Status   string
	TenantID uuid.UUID
	JobID    uuid.UUID
}

// DeleteAccountResult carries the deprovisioning job ID so callers can poll
// the provisioner API for teardown progress.
type DeleteAccountResult struct {
	Slug  string
	JobID uuid.UUID
}

// IssueKeyParams carries the validated inputs for issuing a new API key.
type IssueKeyParams struct {
	TenantSlug  string
	DisplayName string
	Scopes      string // comma-separated, e.g. "provision:write,provision:read"
}

// KeyResult is the service-layer representation of a newly-issued API key.
// RawKey is present only immediately after generation — it is never stored.
type KeyResult struct {
	CreatedAt   time.Time
	RawKey      string
	DisplayName string
	Scopes      string
	Status      string
	KeyID       uuid.UUID
}

// RegisterParams carries the validated, transport-free inputs for self-service
// user registration. The password arrives here as plain text; the service
// layer bcrypt-hashes it before calling UserStorer.Create.
type RegisterParams struct {
	Email       string
	Password    string // plain text — hashed by Register() before storage
	Slug        string
	DisplayName string
	Plan        Plan
}

// RegisterResult is returned by Register: user ID, tenant ID, the tenant slug,
// and the initial API key (raw, shown once).
// VPC provisioning is NOT started here; the caller must follow up with
// ProvisionNetwork to kick off the background workflow.
type RegisterResult struct {
	InitialAPIKey string // raw bearer token — shown once, never stored
	Slug          string
	UserID        uuid.UUID
	TenantID      uuid.UUID
}

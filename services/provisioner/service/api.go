package service

import (
	"context"

	"github.com/google/uuid"
	openbao "github.com/openbao/openbao/api/v2"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// ProvisionerService is the interface the REST handler depends on.
// Keeping the handler coupled only to this interface means it can be tested
// without a real ScyllaDB or OpenBao connection.
type ProvisionerService interface {
	// Provision starts the 10-step VPC provisioning workflow asynchronously.
	// It enqueues a job and returns its ID immediately. The caller should poll
	// GetJob until the job reaches a terminal state.
	// Returns ErrTenantAlreadyExists if the tenant_id is taken.
	Provision(ctx context.Context, p ProvisionParams) (uuid.UUID, error)

	// GetJob returns the current state of a provisioning or deprovisioning job.
	// Returns ErrJobNotFound if no job exists for the given ID.
	GetJob(ctx context.Context, jobID uuid.UUID) (JobResult, error)

	// Deprovision enqueues the teardown workflow for an existing tenant.
	// Returns ErrTenantNotFound if the tenant_id does not exist.
	Deprovision(ctx context.Context, p DeprovisionParams) (uuid.UUID, error)
}

// Deps holds the external dependencies that the service implementation
// requires. They are injected from cmd/cf-provisioner/main.go.
type Deps struct {
	Tenants *accounts.TenantStore
	Keys    *accounts.APIKeyStore
	Jobs    *accounts.JobStore
	Bao     *openbao.Client
}

// New returns a CFProvisionerService as a ProvisionerService interface.
// Callers always interact through the interface; the concrete type is
// never exposed outside this package.
func New(d Deps) ProvisionerService {
	return &CFProvisionerService{
		tenants: d.Tenants,
		keys:    d.Keys,
		jobs:    d.Jobs,
		bao:     d.Bao,
	}
}

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

// tenantManager abstracts the tenant-store operations used by this service.
// *accounts.TenantStore satisfies this interface.
type tenantManager interface {
	GetBySlug(ctx context.Context, slug string) (*accounts.Tenant, error)
	SetCIDRs(ctx context.Context, tenantID uuid.UUID, podCIDR, svcCIDR string) error
	UpdateStatus(ctx context.Context, tenantID uuid.UUID, status accounts.TenantStatus) error
}

// jobQueuer abstracts the job-store operations used by this service.
// *accounts.JobStore satisfies this interface.
type jobQueuer interface {
	Enqueue(ctx context.Context, tenantID uuid.UUID, idemKey string, op accounts.JobOperation) (uuid.UUID, error)
	Get(ctx context.Context, tenantID, jobID uuid.UUID) (*accounts.ProvisioningJob, error)
	Claim(ctx context.Context, tenantID, jobID uuid.UUID) (bool, error)
	Complete(ctx context.Context, tenantID, jobID uuid.UUID, result string) error
	Fail(ctx context.Context, tenantID, jobID uuid.UUID, errMsg string) error
}

// apiKeyManager combines key creation and revocation used during provisioning
// and deprovisioning. *accounts.APIKeyStore satisfies this interface.
type apiKeyManager interface {
	Store(ctx context.Context, k *accounts.APIKey) error
	RevokeByHash(ctx context.Context, keyHash string) error
}

// Deps holds the external dependencies that the service implementation
// requires. They are injected from cmd/cf-provisioner/main.go.
// Concrete types are accepted here; the service stores them as interfaces
// for testability.
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

// newWithIfaces is used in tests to inject mock implementations of the
// internal interfaces without needing real ScyllaDB / OpenBao connections.
// The bao client is always nil in unit tests because OpenBao calls are
// replaced by the storeKubeconfigFn / revokeKubeconfigFn seam variables.
func newWithIfaces(tenants tenantManager, jobs jobQueuer, keys apiKeyManager) ProvisionerService {
	return &CFProvisionerService{
		tenants: tenants,
		keys:    keys,
		jobs:    jobs,
	}
}

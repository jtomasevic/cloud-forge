package service_test

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	"github.com/jtomasevic/cloud-forge/internal/provisioner"
	provisionersvc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// ── fakeTenantStore ──────────────────────────────────────────────────────────

type fakeTenantStore struct {
	tenant    *accounts.Tenant
	err       error
	createErr error
}

func (f *fakeTenantStore) Create(_ context.Context, slug, _, _ string) (*accounts.Tenant, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	t := &accounts.Tenant{
		TenantID: uuid.New(),
		Slug:     slug,
		Status:   accounts.TenantStatusProvisioning,
	}
	f.tenant = t
	return t, nil
}

func (f *fakeTenantStore) GetBySlug(_ context.Context, _ string) (*accounts.Tenant, error) {
	return f.tenant, f.err
}

// ── fakeAPIKeyStore ──────────────────────────────────────────────────────────

type fakeAPIKeyStore struct {
	storeErr  error
	revokeErr error
	// storeRecorded is the last key passed to Store, for assertion.
	storeRecorded *accounts.APIKey
}

func (f *fakeAPIKeyStore) Store(_ context.Context, k *accounts.APIKey) error {
	f.storeRecorded = k
	return f.storeErr
}

func (f *fakeAPIKeyStore) RevokeByID(_ context.Context, _ uuid.UUID) error {
	return f.revokeErr
}

// fakeAPIKeyStore also satisfies provisioner.APIKeyStorer (used as KeyGenerator).
var _ provisioner.APIKeyStorer = (*fakeAPIKeyStore)(nil)

// ── fakeUserStore ─────────────────────────────────────────────────────────────

type fakeUserStore struct {
	createUser    *accounts.User
	createErr     error
	getByEmailRes *accounts.User
	getByEmailErr error
}

func (f *fakeUserStore) Create(_ context.Context, _, _ string, _ uuid.UUID) (*accounts.User, error) {
	return f.createUser, f.createErr
}

func (f *fakeUserStore) GetByEmail(_ context.Context, _ string) (*accounts.User, error) {
	return f.getByEmailRes, f.getByEmailErr
}

// ── fakeProvisionerService ────────────────────────────────────────────────────

type fakeProvisionerService struct {
	provisionErr     error
	deprovisionErr   error
	provisionJobID   uuid.UUID
	deprovisionJobID uuid.UUID
}

func (f *fakeProvisionerService) Provision(_ context.Context, _ provisionersvc.ProvisionParams) (uuid.UUID, error) {
	return f.provisionJobID, f.provisionErr
}

func (f *fakeProvisionerService) GetJob(_ context.Context, _ uuid.UUID) (provisionersvc.JobResult, error) {
	return provisionersvc.JobResult{}, errors.New("not implemented in fake")
}

func (f *fakeProvisionerService) Deprovision(_ context.Context, _ provisionersvc.DeprovisionParams) (uuid.UUID, error) {
	return f.deprovisionJobID, f.deprovisionErr
}

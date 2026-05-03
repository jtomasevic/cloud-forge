// Package service implements the CloudForge Accounts service layer.
//
// # Responsibility
//
// This package owns the business logic for:
//   - Self-service user registration (creates user + tenant record, issues initial key)
//   - Provisioning the private network for an existing tenant (via the provisioner library)
//   - Querying account status
//   - Issuing and revoking tenant API keys
//
// # Flow
//
//  1. Register (public) — creates user + tenant (PROVISIONING), returns initial_api_key.
//  2. ProvisionNetwork (authenticated) — starts VPC workflow, returns job_id.
//  3. GetAccount — poll for status=ACTIVE.
//
// # Dependencies
//
// The service layer depends on:
//   - [TenantStorer] — creates and reads tenant records in ScyllaDB
//   - [UserStorer] — creates and looks up user records (email + bcrypt hash)
//   - [APIKeyStorer] — revokes API keys in ScyllaDB
//   - [APIKeyGenerator] — generates new API keys (BLAKE2b hash + raw value)
//   - [ProvisionerService] — the VPC provisioner, used as a Go library (same process)
//
// All dependencies are injected as interfaces to enable unit testing without
// a live database or provisioner.
//
// # Wire-up
//
// In cmd/cf-accounts/wire.go, call New(Deps{...}) and pass the returned
// AccountsService to the REST handler:
//
//	svc := service.New(service.Deps{...})
//	handler := accounts.NewHandler(svc)
//	router  := accounts.NewRouter(handler, logger, reg, "accounts_svc")
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	"github.com/jtomasevic/cloud-forge/internal/provisioner"
	provisionersvc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// ── Store interfaces ──────────────────────────────────────────────────────────
//
// These interfaces define the subset of each store that CFAccountsService uses.
// The concrete *accounts.TenantStore and *accounts.APIKeyStore satisfy them.
// Using interfaces here makes unit tests possible without a real ScyllaDB.

// TenantStorer is the store interface required by CFAccountsService.
type TenantStorer interface {
	// Create inserts a new tenant record and returns it.
	// Returns accounts.ErrTenantAlreadyExists if the slug is already taken.
	Create(ctx context.Context, slug, displayName, planID string) (*accounts.Tenant, error)
	// GetBySlug looks up a tenant by its URL-safe slug.
	// Returns accounts.ErrTenantNotFound if the slug is unknown.
	GetBySlug(ctx context.Context, slug string) (*accounts.Tenant, error)
}

// UserStorer is the store interface for human user records.
// Used only by the Register flow. Concrete type: *accounts.UserStore.
type UserStorer interface {
	// Create inserts a new user record. Returns ErrEmailAlreadyRegistered if
	// a user with the same email already exists.
	Create(ctx context.Context, email, passwordHash string, tenantID uuid.UUID) (*accounts.User, error)
	// GetByEmail resolves an email to a user record.
	// Returns accounts.ErrUserNotFound if the email is unknown.
	GetByEmail(ctx context.Context, email string) (*accounts.User, error)
}

// APIKeyStorer is the store interface required for API key revocation.
// provisioner.APIKeyStorer (from internal/provisioner/apikey.go) is used for
// generation — it has a compatible Store method.
type APIKeyStorer interface {
	// Store persists a new API key record (used for generation).
	Store(ctx context.Context, k *accounts.APIKey) error
	// RevokeByID marks the key identified by keyID as REVOKED.
	RevokeByID(ctx context.Context, keyID uuid.UUID) error
}

// AccountsService is the interface the REST handler depends on.
// It is the only boundary between the REST layer and the service layer.
type AccountsService interface {
	// ProvisionNetwork starts the VPC provisioning workflow for an existing
	// tenant account (created via Register). Returns a job ID immediately;
	// the actual provisioning runs asynchronously.
	// Returns ErrAccountNotFound if no tenant with that slug exists.
	ProvisionNetwork(ctx context.Context, tenantSlug string) (ProvisionNetworkResult, error)

	// GetAccount returns the current state of a tenant account by slug.
	// Returns ErrAccountNotFound if the slug does not exist.
	GetAccount(ctx context.Context, tenantSlug string) (AccountResult, error)

	// DeleteAccount initiates the deprovisioning workflow for the given slug.
	// Returns DeleteAccountResult immediately; teardown runs asynchronously.
	// Returns ErrAccountNotFound if the slug does not exist.
	DeleteAccount(ctx context.Context, tenantSlug string) (DeleteAccountResult, error)

	// IssueKey generates a new API key for the tenant identified by tenantSlug.
	// The raw key in KeyResult.RawKey is present only in this response.
	// Returns ErrAccountNotFound if the tenant does not exist.
	// Returns ErrAccountNotActive if the tenant is not in ACTIVE status.
	IssueKey(ctx context.Context, p IssueKeyParams) (KeyResult, error)

	// RevokeKey permanently revokes the API key identified by keyID.
	// Returns ErrKeyNotFound if no key with that ID exists.
	// Revocation is idempotent: revoking an already-revoked key succeeds.
	RevokeKey(ctx context.Context, tenantSlug string, keyID uuid.UUID) error

	// Register creates a user record and a tenant account (PROVISIONING status),
	// then issues an initial API key. VPC provisioning is NOT started here —
	// call ProvisionNetwork after registration.
	// Returns ErrEmailAlreadyRegistered if the email is already in use.
	// Returns ErrAccountAlreadyExists if the slug is already taken.
	Register(ctx context.Context, p RegisterParams) (RegisterResult, error)
}

// Deps holds the external dependencies injected into CFAccountsService.
// All fields are interfaces to allow unit testing without real infrastructure.
type Deps struct {
	Tenants     TenantStorer
	Users       UserStorer
	Keys        APIKeyStorer
	Provisioner provisionersvc.ProvisionerService

	// KeyGenerator is used to call provisioner.GenerateAPIKey with a
	// provisioner.APIKeyStorer. Pass the same *accounts.APIKeyStore as Keys.
	// Separated from Keys so each dependency has its own interface.
	KeyGenerator provisioner.APIKeyStorer
}

// New returns a CFAccountsService as an AccountsService interface.
// The concrete type is never exported; callers always use the interface.
//
// Following the naming rule from docs/general/webappsec.md:
// concrete type is CF<InterfaceName> = CFAccountsService.
func New(d Deps) AccountsService { //nolint:gocritic // hugeParam: Deps is only passed once at startup; pointer would leak the struct to callers
	return &CFAccountsService{
		tenants:      d.Tenants,
		users:        d.Users,
		keys:         d.Keys,
		provisioner:  d.Provisioner,
		keyGenerator: d.KeyGenerator,
	}
}

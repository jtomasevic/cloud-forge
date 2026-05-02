package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	"github.com/jtomasevic/cloud-forge/internal/provisioner"
	provisionersvc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// CFAccountsService is the concrete implementation of AccountsService.
// It is unexported and only accessible through the AccountsService interface
// returned by New().
//
// It orchestrates between:
//   - CFProvisionerService (VPC lifecycle)
//   - TenantStorer (account data in ScyllaDB)
//   - UserStorer (human user records in ScyllaDB)
//   - APIKeyStorer (API key management in ScyllaDB)
type CFAccountsService struct {
	tenants      TenantStorer
	users        UserStorer
	keys         APIKeyStorer
	provisioner  provisionersvc.ProvisionerService
	keyGenerator provisioner.APIKeyStorer
}

// ── AccountsService implementation ───────────────────────────────────────────

// ProvisionNetwork starts the VPC provisioning workflow for a tenant account
// that was created via Register. It delegates to the provisioner service library
// and returns the job ID for the caller to poll.
//
// The tenant must exist (created by Register). The provisioner's background
// workflow looks up the tenant record and proceeds with the 10-step VPC setup.
func (s *CFAccountsService) ProvisionNetwork(ctx context.Context, tenantSlug string) (ProvisionNetworkResult, error) {
	// Verify the tenant exists before kicking off the workflow.
	tenant, err := s.tenants.GetBySlug(ctx, tenantSlug)
	if errors.Is(err, accounts.ErrTenantNotFound) {
		return ProvisionNetworkResult{}, ErrAccountNotFound
	}
	if err != nil {
		return ProvisionNetworkResult{}, fmt.Errorf("accounts: provision network, lookup tenant: %w", err)
	}

	jobID, err := s.provisioner.Provision(ctx, provisionersvc.ProvisionParams{
		TenantSlug: tenantSlug,
	})
	if errors.Is(err, provisionersvc.ErrTenantAlreadyExists) {
		return ProvisionNetworkResult{}, ErrAccountAlreadyExists
	}
	if err != nil {
		return ProvisionNetworkResult{}, fmt.Errorf("accounts: provision network: %w", err)
	}

	return ProvisionNetworkResult{
		TenantID: tenant.TenantID,
		Slug:     tenantSlug,
		Status:   string(accounts.TenantStatusProvisioning),
		JobID:    jobID,
	}, nil
}

// GetAccount returns the current state of a tenant account by looking up its
// record in ScyllaDB via the TenantStorer.
func (s *CFAccountsService) GetAccount(ctx context.Context, tenantSlug string) (AccountResult, error) {
	tenant, err := s.tenants.GetBySlug(ctx, tenantSlug)
	if errors.Is(err, accounts.ErrTenantNotFound) {
		return AccountResult{}, ErrAccountNotFound
	}
	if err != nil {
		return AccountResult{}, fmt.Errorf("accounts: get account: %w", err)
	}
	return ToAccountResultFromTenant(tenant), nil
}

// DeleteAccount initiates the VPC deprovisioning workflow for the given tenant
// slug. The provisioner service handles the full teardown asynchronously.
func (s *CFAccountsService) DeleteAccount(ctx context.Context, tenantSlug string) (DeleteAccountResult, error) {
	jobID, err := s.provisioner.Deprovision(ctx, provisionersvc.DeprovisionParams{
		TenantSlug: tenantSlug,
	})
	if errors.Is(err, provisionersvc.ErrTenantNotFound) {
		return DeleteAccountResult{}, ErrAccountNotFound
	}
	if err != nil {
		return DeleteAccountResult{}, fmt.Errorf("accounts: deprovision: %w", err)
	}
	return DeleteAccountResult{
		Slug:  tenantSlug,
		JobID: jobID,
	}, nil
}

// IssueKey generates a new API key for the tenant identified by tenantSlug.
// The tenant must be in ACTIVE status. The raw key is returned once; only its
// BLAKE2b-256 hash is persisted.
func (s *CFAccountsService) IssueKey(ctx context.Context, p IssueKeyParams) (KeyResult, error) {
	tenant, err := s.tenants.GetBySlug(ctx, p.TenantSlug)
	if errors.Is(err, accounts.ErrTenantNotFound) {
		return KeyResult{}, ErrAccountNotFound
	}
	if err != nil {
		return KeyResult{}, fmt.Errorf("accounts: issue key, lookup tenant: %w", err)
	}

	if tenant.Status != accounts.TenantStatusActive {
		return KeyResult{}, ErrAccountNotActive
	}

	// Resolve default scopes when the caller omits the field.
	scopes := p.Scopes
	if scopes == "" {
		scopes = "provision:read"
	}

	generated, err := provisioner.GenerateAPIKey(ctx, s.keyGenerator, tenant.TenantID, p.DisplayName, scopes)
	if err != nil {
		return KeyResult{}, fmt.Errorf("accounts: generate api key: %w", err)
	}

	return KeyResult{
		KeyID:       generated.Record.KeyID,
		RawKey:      generated.RawKey,
		DisplayName: generated.Record.DisplayName,
		Scopes:      generated.Record.Scopes,
		Status:      string(generated.Record.Status),
		CreatedAt:   generated.Record.CreatedAt,
	}, nil
}

// Register creates a user record (email + bcrypt-hashed password) and a
// tenant account (status: PROVISIONING), then issues an initial API key.
//
// VPC network provisioning is deliberately NOT started here. The caller must
// follow up with POST /accounts/{slug}/provision (authenticated) to kick off
// the background workflow once registered.
//
// Error precedence:
//  1. Email uniqueness check (ErrEmailAlreadyRegistered).
//  2. Tenant slug uniqueness (ErrAccountAlreadyExists).
//  3. bcrypt hashing failure (unlikely; treated as 500).
//  4. User record creation failure (propagated as 500).
//  5. Initial API key issuance failure (propagated as 500).
//
// The initial API key bypasses the ACTIVE status check because the tenant is
// still PROVISIONING at this point. The key lets the user authenticate the
// follow-up ProvisionNetwork call.
func (s *CFAccountsService) Register(ctx context.Context, p RegisterParams) (RegisterResult, error) { //nolint:gocritic // hugeParam: RegisterParams passed by value intentionally to match interface signature
	// 1. Check email uniqueness via the MV before attempting any writes.
	//    A 404 here means the email is available.
	_, err := s.users.GetByEmail(ctx, p.Email)
	if err == nil {
		return RegisterResult{}, ErrEmailAlreadyRegistered
	}
	if !errors.Is(err, accounts.ErrUserNotFound) {
		return RegisterResult{}, fmt.Errorf("accounts: register, email lookup: %w", err)
	}

	// 2. Create the tenant record directly (status: PROVISIONING).
	//    The LWT on cf.tenant_slugs enforces slug uniqueness.
	tenant, err := s.tenants.Create(ctx, p.Slug, p.DisplayName, string(p.Plan))
	if errors.Is(err, accounts.ErrTenantAlreadyExists) {
		return RegisterResult{}, ErrAccountAlreadyExists
	}
	if err != nil {
		return RegisterResult{}, fmt.Errorf("accounts: register, create tenant: %w", err)
	}

	// 3. Hash the password with bcrypt (cost 12).
	//    The raw password is never stored or logged beyond this point.
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), 12) //nolint:gosec // bcrypt cost 12 is intentional
	if err != nil {
		return RegisterResult{}, fmt.Errorf("accounts: register, hash password: %w", err)
	}

	// 4. Persist the user record (LWT guards against email races).
	user, err := s.users.Create(ctx, p.Email, string(hash), tenant.TenantID)
	if errors.Is(err, accounts.ErrEmailAlreadyRegistered) {
		return RegisterResult{}, ErrEmailAlreadyRegistered
	}
	if err != nil {
		return RegisterResult{}, fmt.Errorf("accounts: register, create user: %w", err)
	}

	// 5. Issue the bootstrapped API key.
	//    We bypass IssueKey()'s ACTIVE status check because the tenant is
	//    still PROVISIONING. The user must be able to call ProvisionNetwork.
	key, err := provisioner.GenerateAPIKey(ctx, s.keyGenerator, tenant.TenantID, "initial", "provision:read")
	if err != nil {
		return RegisterResult{}, fmt.Errorf("accounts: register, issue initial key: %w", err)
	}

	return RegisterResult{
		UserID:        user.UserID,
		TenantID:      tenant.TenantID,
		Slug:          p.Slug,
		InitialAPIKey: key.RawKey,
	}, nil
}

// RevokeKey permanently revokes the API key identified by keyID.
// Revocation is idempotent at the store level (no error if already revoked).
func (s *CFAccountsService) RevokeKey(ctx context.Context, tenantSlug string, keyID uuid.UUID) error {
	// The tenantSlug is validated by the REST layer. We do a quick existence
	// check to provide a 404 if the tenant does not exist before attempting
	// key revocation. This avoids silently ignoring invalid tenant slugs.
	_, err := s.tenants.GetBySlug(ctx, tenantSlug)
	if errors.Is(err, accounts.ErrTenantNotFound) {
		return ErrAccountNotFound
	}
	if err != nil {
		return fmt.Errorf("accounts: revoke key, lookup tenant: %w", err)
	}

	if err := s.keys.RevokeByID(ctx, keyID); err != nil {
		return fmt.Errorf("accounts: revoke key %s: %w", keyID, err)
	}
	return nil
}

package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	provisionersvc "github.com/jtomasevic/cloud-forge/services/provisioner/service"

	. "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func activeTenant(slug string) *accounts.Tenant {
	return &accounts.Tenant{
		TenantID:    uuid.New(),
		Slug:        slug,
		DisplayName: "Test Tenant",
		Status:      accounts.TenantStatusActive,
		PlanID:      "starter",
		PodCIDR:     "10.100.1.0/24",
		SvcCIDR:     "10.200.1.0/24",
		CreatedAt:   time.Now(),
	}
}

func provisioningTenant(slug string) *accounts.Tenant {
	t := activeTenant(slug)
	t.Status = accounts.TenantStatusProvisioning
	return t
}

func defaultDeps(ts *fakeTenantStore, ks *fakeAPIKeyStore, ps *fakeProvisionerService) Deps {
	return Deps{
		Tenants:      ts,
		Users:        &fakeUserStore{},
		Keys:         ks,
		Provisioner:  ps,
		KeyGenerator: ks,
	}
}

func depsWithUsers(ts *fakeTenantStore, us *fakeUserStore, ks *fakeAPIKeyStore, ps *fakeProvisionerService) Deps {
	return Deps{
		Tenants:      ts,
		Users:        us,
		Keys:         ks,
		Provisioner:  ps,
		KeyGenerator: ks,
	}
}

// ── ProvisionNetwork ──────────────────────────────────────────────────────────

func TestProvisionNetwork_Success(t *testing.T) {
	jobID := uuid.New()
	tenant := provisioningTenant("acme")
	ps := &fakeProvisionerService{provisionJobID: jobID}
	ts := &fakeTenantStore{tenant: tenant}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, ps))

	result, err := svc.ProvisionNetwork(context.Background(), "acme")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JobID != jobID {
		t.Errorf("job ID: got %s, want %s", result.JobID, jobID)
	}
	if result.Slug != "acme" {
		t.Errorf("slug: got %q, want %q", result.Slug, "acme")
	}
}

func TestProvisionNetwork_TenantNotFound(t *testing.T) {
	ps := &fakeProvisionerService{}
	ts := &fakeTenantStore{err: accounts.ErrTenantNotFound}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, ps))

	_, err := svc.ProvisionNetwork(context.Background(), "missing")

	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestProvisionNetwork_AlreadyProvisioned(t *testing.T) {
	tenant := provisioningTenant("dup")
	ps := &fakeProvisionerService{
		provisionErr: provisionersvc.ErrTenantAlreadyExists,
	}
	ts := &fakeTenantStore{tenant: tenant}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, ps))

	_, err := svc.ProvisionNetwork(context.Background(), "dup")

	if !errors.Is(err, ErrAccountAlreadyExists) {
		t.Errorf("expected ErrAccountAlreadyExists, got %v", err)
	}
}

func TestProvisionNetwork_ProvisionerError_Wrapped(t *testing.T) {
	tenant := provisioningTenant("fail")
	ps := &fakeProvisionerService{
		provisionErr: errors.New("some infrastructure failure"),
	}
	ts := &fakeTenantStore{tenant: tenant}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, ps))

	_, err := svc.ProvisionNetwork(context.Background(), "fail")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── GetAccount ────────────────────────────────────────────────────────────────

func TestGetAccount_Success(t *testing.T) {
	tenant := activeTenant("acme")
	ts := &fakeTenantStore{tenant: tenant}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	result, err := svc.GetAccount(context.Background(), "acme")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Slug != "acme" {
		t.Errorf("slug: got %q, want %q", result.Slug, "acme")
	}
	if result.Status != string(accounts.TenantStatusActive) {
		t.Errorf("status: got %q, want ACTIVE", result.Status)
	}
	if result.PodCIDR != "10.100.1.0/24" {
		t.Errorf("pod cidr: got %q", result.PodCIDR)
	}
}

func TestGetAccount_NotFound(t *testing.T) {
	ts := &fakeTenantStore{err: accounts.ErrTenantNotFound}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	_, err := svc.GetAccount(context.Background(), "missing")

	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestGetAccount_StoreError(t *testing.T) {
	ts := &fakeTenantStore{err: errors.New("db offline")}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	_, err := svc.GetAccount(context.Background(), "broken")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── DeleteAccount ─────────────────────────────────────────────────────────────

func TestDeleteAccount_Success(t *testing.T) {
	jobID := uuid.New()
	ps := &fakeProvisionerService{deprovisionJobID: jobID}
	svc := New(defaultDeps(&fakeTenantStore{}, &fakeAPIKeyStore{}, ps))

	result, err := svc.DeleteAccount(context.Background(), "acme")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JobID != jobID {
		t.Errorf("job ID: got %s, want %s", result.JobID, jobID)
	}
	if result.Slug != "acme" {
		t.Errorf("slug: got %q, want %q", result.Slug, "acme")
	}
}

func TestDeleteAccount_NotFound(t *testing.T) {
	ps := &fakeProvisionerService{deprovisionErr: provisionersvc.ErrTenantNotFound}
	svc := New(defaultDeps(&fakeTenantStore{}, &fakeAPIKeyStore{}, ps))

	_, err := svc.DeleteAccount(context.Background(), "ghost")

	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestDeleteAccount_ProvisionerError(t *testing.T) {
	ps := &fakeProvisionerService{deprovisionErr: errors.New("teardown failed")}
	svc := New(defaultDeps(&fakeTenantStore{}, &fakeAPIKeyStore{}, ps))

	_, err := svc.DeleteAccount(context.Background(), "broken")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── IssueKey ─────────────────────────────────────────────────────────────────

func TestIssueKey_Success(t *testing.T) {
	tenant := activeTenant("acme")
	ts := &fakeTenantStore{tenant: tenant}
	ks := &fakeAPIKeyStore{}
	svc := New(defaultDeps(ts, ks, &fakeProvisionerService{}))

	result, err := svc.IssueKey(context.Background(), IssueKeyParams{
		TenantSlug:  "acme",
		DisplayName: "CI/CD pipeline",
		Scopes:      "provision:read",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RawKey == "" {
		t.Error("expected non-empty raw key")
	}
	if result.DisplayName != "CI/CD pipeline" {
		t.Errorf("display name: got %q, want %q", result.DisplayName, "CI/CD pipeline")
	}
}

func TestIssueKey_DefaultScopes(t *testing.T) {
	tenant := activeTenant("acme")
	ts := &fakeTenantStore{tenant: tenant}
	ks := &fakeAPIKeyStore{}
	svc := New(defaultDeps(ts, ks, &fakeProvisionerService{}))

	// Pass empty scopes — service should default to "provision:read"
	result, err := svc.IssueKey(context.Background(), IssueKeyParams{
		TenantSlug: "acme", DisplayName: "key",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scopes != "provision:read" {
		t.Errorf("scopes: got %q, want %q", result.Scopes, "provision:read")
	}
}

func TestIssueKey_TenantNotFound(t *testing.T) {
	ts := &fakeTenantStore{err: accounts.ErrTenantNotFound}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	_, err := svc.IssueKey(context.Background(), IssueKeyParams{TenantSlug: "ghost"})

	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestIssueKey_TenantNotActive(t *testing.T) {
	tenant := provisioningTenant("acme")
	ts := &fakeTenantStore{tenant: tenant}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	_, err := svc.IssueKey(context.Background(), IssueKeyParams{
		TenantSlug: "acme", DisplayName: "key",
	})

	if !errors.Is(err, ErrAccountNotActive) {
		t.Errorf("expected ErrAccountNotActive, got %v", err)
	}
}

func TestIssueKey_StoreError(t *testing.T) {
	tenant := activeTenant("acme")
	ts := &fakeTenantStore{tenant: tenant}
	ks := &fakeAPIKeyStore{storeErr: errors.New("disk full")}
	svc := New(defaultDeps(ts, ks, &fakeProvisionerService{}))

	_, err := svc.IssueKey(context.Background(), IssueKeyParams{
		TenantSlug: "acme", DisplayName: "key",
	})
	if err == nil {
		t.Fatal("expected error from store, got nil")
	}
}

func TestIssueKey_LookupError(t *testing.T) {
	ts := &fakeTenantStore{err: errors.New("connection refused")}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	_, err := svc.IssueKey(context.Background(), IssueKeyParams{
		TenantSlug: "acme", DisplayName: "key",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── RevokeKey ─────────────────────────────────────────────────────────────────

func TestRevokeKey_Success(t *testing.T) {
	tenant := activeTenant("acme")
	ts := &fakeTenantStore{tenant: tenant}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	err := svc.RevokeKey(context.Background(), "acme", uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRevokeKey_TenantNotFound(t *testing.T) {
	ts := &fakeTenantStore{err: accounts.ErrTenantNotFound}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	err := svc.RevokeKey(context.Background(), "ghost", uuid.New())

	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestRevokeKey_StoreError(t *testing.T) {
	tenant := activeTenant("acme")
	ts := &fakeTenantStore{tenant: tenant}
	ks := &fakeAPIKeyStore{revokeErr: errors.New("revoke failed")}
	svc := New(defaultDeps(ts, ks, &fakeProvisionerService{}))

	err := svc.RevokeKey(context.Background(), "acme", uuid.New())
	if err == nil {
		t.Fatal("expected error from revoke, got nil")
	}
}

func TestRevokeKey_TenantLookupError(t *testing.T) {
	ts := &fakeTenantStore{err: errors.New("timeout")}
	svc := New(defaultDeps(ts, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	err := svc.RevokeKey(context.Background(), "broken", uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	// GetByEmail returns "not found" → email is available
	us := &fakeUserStore{
		getByEmailErr: accounts.ErrUserNotFound,
		createUser: &accounts.User{
			UserID:   uuid.New(),
			TenantID: uuid.New(),
			Email:    "alice@acme.com",
		},
	}
	// fakeTenantStore.Create will produce a new tenant
	ts := &fakeTenantStore{}
	ps := &fakeProvisionerService{}
	ks := &fakeAPIKeyStore{}

	s := New(depsWithUsers(ts, us, ks, ps))
	result, err := s.Register(context.Background(), RegisterParams{
		Email:       "alice@acme.com",
		Password:    "s3cur3pass!",
		Slug:        "acme-corp",
		DisplayName: "Acme Corporation",
		Plan:        PlanStarter,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Slug != "acme-corp" {
		t.Errorf("slug: got %q", result.Slug)
	}
	if result.InitialAPIKey == "" {
		t.Error("expected non-empty initial_api_key")
	}
	// VPC provisioning is NOT started by Register; no job_id in result.
	if result.TenantID == uuid.Nil {
		t.Error("expected non-nil tenant_id")
	}
}

func TestRegister_EmailAlreadyExists(t *testing.T) {
	// GetByEmail returns an existing user → email taken
	us := &fakeUserStore{
		getByEmailRes: &accounts.User{Email: "alice@acme.com"},
	}
	ts := &fakeTenantStore{}
	ps := &fakeProvisionerService{}

	s := New(depsWithUsers(ts, us, &fakeAPIKeyStore{}, ps))
	_, err := s.Register(context.Background(), RegisterParams{
		Email: "alice@acme.com", Password: "s3cur3pass!", Slug: "acme",
	})

	if !errors.Is(err, ErrEmailAlreadyRegistered) {
		t.Errorf("expected ErrEmailAlreadyRegistered, got %v", err)
	}
}

func TestRegister_EmailLookupError(t *testing.T) {
	us := &fakeUserStore{getByEmailErr: errors.New("db offline")}
	s := New(depsWithUsers(&fakeTenantStore{}, us, &fakeAPIKeyStore{}, &fakeProvisionerService{}))

	_, err := s.Register(context.Background(), RegisterParams{
		Email: "alice@acme.com", Password: "s3cur3pass!", Slug: "acme",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRegister_SlugAlreadyExists(t *testing.T) {
	us := &fakeUserStore{getByEmailErr: accounts.ErrUserNotFound}
	ts := &fakeTenantStore{createErr: accounts.ErrTenantAlreadyExists}

	s := New(depsWithUsers(ts, us, &fakeAPIKeyStore{}, &fakeProvisionerService{}))
	_, err := s.Register(context.Background(), RegisterParams{
		Email: "alice@acme.com", Password: "s3cur3pass!", Slug: "taken",
	})

	if !errors.Is(err, ErrAccountAlreadyExists) {
		t.Errorf("expected ErrAccountAlreadyExists, got %v", err)
	}
}

func TestRegister_TenantCreateFails(t *testing.T) {
	us := &fakeUserStore{getByEmailErr: accounts.ErrUserNotFound}
	ts := &fakeTenantStore{createErr: errors.New("db offline")}

	s := New(depsWithUsers(ts, us, &fakeAPIKeyStore{}, &fakeProvisionerService{}))
	_, err := s.Register(context.Background(), RegisterParams{
		Email: "alice@acme.com", Password: "s3cur3pass!", Slug: "acme",
	})
	if err == nil {
		t.Fatal("expected error from tenant create, got nil")
	}
}

func TestRegister_UserCreateFails(t *testing.T) {
	us := &fakeUserStore{
		getByEmailErr: accounts.ErrUserNotFound,
		createErr:     errors.New("disk full"),
	}
	ts := &fakeTenantStore{}

	s := New(depsWithUsers(ts, us, &fakeAPIKeyStore{}, &fakeProvisionerService{}))
	_, err := s.Register(context.Background(), RegisterParams{
		Email: "alice@acme.com", Password: "s3cur3pass!", Slug: "acme",
	})
	if err == nil {
		t.Fatal("expected error from user create, got nil")
	}
}

func TestRegister_UserCreateEmailRace(t *testing.T) {
	// Create returns ErrEmailAlreadyRegistered → LWT lost the race
	us := &fakeUserStore{
		getByEmailErr: accounts.ErrUserNotFound,
		createErr:     accounts.ErrEmailAlreadyRegistered,
	}
	ts := &fakeTenantStore{}

	s := New(depsWithUsers(ts, us, &fakeAPIKeyStore{}, &fakeProvisionerService{}))
	_, err := s.Register(context.Background(), RegisterParams{
		Email: "alice@acme.com", Password: "s3cur3pass!", Slug: "acme",
	})

	if !errors.Is(err, ErrEmailAlreadyRegistered) {
		t.Errorf("expected ErrEmailAlreadyRegistered, got %v", err)
	}
}

func TestRegister_KeyStoreFails(t *testing.T) {
	us := &fakeUserStore{
		getByEmailErr: accounts.ErrUserNotFound,
		createUser:    &accounts.User{UserID: uuid.New(), TenantID: uuid.New()},
	}
	ts := &fakeTenantStore{}
	ks := &fakeAPIKeyStore{storeErr: errors.New("write failed")}

	s := New(depsWithUsers(ts, us, ks, &fakeProvisionerService{}))
	_, err := s.Register(context.Background(), RegisterParams{
		Email: "alice@acme.com", Password: "s3cur3pass!", Slug: "acme",
	})
	if err == nil {
		t.Fatal("expected error from key store, got nil")
	}
}

package accounts_test

import (
	"context"
	"time"

	"github.com/google/uuid"

	svc "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

// fakeAccountsService is a hand-rolled mock for svc.AccountsService.
// Each method records its call and returns pre-programmed results.
type fakeAccountsService struct { //nolint:govet // field order optimised for readability, not GC bitmap
	// ProvisionNetwork controls
	provisionResult svc.ProvisionNetworkResult
	provisionErr    error

	// GetAccount controls
	getResult svc.AccountResult
	getErr    error

	// DeleteAccount controls
	deleteResult svc.DeleteAccountResult
	deleteErr    error

	// IssueKey controls
	issueResult svc.KeyResult
	issueErr    error

	// RevokeKey controls
	revokeErr error

	// Register controls
	registerResult svc.RegisterResult
	registerErr    error
}

func (f *fakeAccountsService) ProvisionNetwork(_ context.Context, _ string) (svc.ProvisionNetworkResult, error) {
	return f.provisionResult, f.provisionErr
}

func (f *fakeAccountsService) GetAccount(_ context.Context, _ string) (svc.AccountResult, error) {
	return f.getResult, f.getErr
}

func (f *fakeAccountsService) DeleteAccount(_ context.Context, _ string) (svc.DeleteAccountResult, error) {
	return f.deleteResult, f.deleteErr
}

func (f *fakeAccountsService) IssueKey(_ context.Context, _ svc.IssueKeyParams) (svc.KeyResult, error) {
	return f.issueResult, f.issueErr
}

func (f *fakeAccountsService) RevokeKey(_ context.Context, _ string, _ uuid.UUID) error {
	return f.revokeErr
}

func (f *fakeAccountsService) Register(_ context.Context, _ svc.RegisterParams) (svc.RegisterResult, error) { //nolint:gocritic // hugeParam: test mock must match the interface signature
	return f.registerResult, f.registerErr
}

// ── Fixture helpers ───────────────────────────────────────────────────────────

func activeAccountResult(slug string) svc.AccountResult {
	return svc.AccountResult{
		TenantID:    uuid.New(),
		Slug:        slug,
		DisplayName: "Test Tenant",
		Status:      "ACTIVE",
		Plan:        "starter",
		PodCIDR:     "10.100.1.0/24",
		ServiceCIDR: "10.200.1.0/24",
		CreatedAt:   time.Now().UTC(),
	}
}

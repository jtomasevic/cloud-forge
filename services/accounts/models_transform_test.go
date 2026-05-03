package accounts_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	. "github.com/jtomasevic/cloud-forge/services/accounts"
	"github.com/jtomasevic/cloud-forge/services/accounts/generated"
	svc "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

func TestToGeneratedAccountResponse_Active(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()

	ar := svc.AccountResult{
		TenantID:    id,
		Slug:        "acme",
		DisplayName: "Acme Corp",
		Status:      "ACTIVE",
		Plan:        "pro",
		PodCIDR:     "10.100.1.0/24",
		ServiceCIDR: "10.200.1.0/24",
		CreatedAt:   now,
	}

	resp := ToGeneratedAccountResponse(&ar)

	if resp.TenantId != id {
		t.Errorf("tenant_id: got %s, want %s", resp.TenantId, id)
	}
	if resp.Slug != "acme" {
		t.Errorf("slug: got %q", resp.Slug)
	}
	if resp.Status != generated.AccountStatusACTIVE {
		t.Errorf("status: got %q, want ACTIVE", resp.Status)
	}
	if resp.PodCidr == nil || *resp.PodCidr != "10.100.1.0/24" {
		t.Errorf("pod_cidr: got %v", resp.PodCidr)
	}
	if resp.ServiceCidr == nil || *resp.ServiceCidr != "10.200.1.0/24" {
		t.Errorf("service_cidr: got %v", resp.ServiceCidr)
	}
}

func TestToGeneratedAccountResponse_EmptyCIDRs(t *testing.T) {
	ar := svc.AccountResult{
		TenantID: uuid.New(),
		Slug:     "new",
		Status:   "PROVISIONING",
		Plan:     "starter",
	}

	resp := ToGeneratedAccountResponse(&ar)

	// CIDRs must be nil (omitempty) when the tenant is still provisioning.
	if resp.PodCidr != nil {
		t.Errorf("pod_cidr should be nil during PROVISIONING, got %q", *resp.PodCidr)
	}
	if resp.ServiceCidr != nil {
		t.Errorf("service_cidr should be nil during PROVISIONING, got %q", *resp.ServiceCidr)
	}
}

// ── RegisterRequest.ToServiceRegisterParams ───────────────────────────────────

func TestRegisterRequest_ToServiceRegisterParams_OK(t *testing.T) {
	req := RegisterRequest(generated.RegisterRequest{
		Email:       openapi_types.Email("alice@acme.com"),
		Password:    "s3cur3pass!",
		Slug:        "acme-corp",
		DisplayName: "Acme Corporation",
		Plan:        generated.Starter,
	})

	params, err := req.ToServiceRegisterParams()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Email != "alice@acme.com" {
		t.Errorf("email: got %q", params.Email)
	}
	if params.Password != "s3cur3pass!" {
		t.Errorf("password not preserved")
	}
	if params.Slug != "acme-corp" {
		t.Errorf("slug: got %q", params.Slug)
	}
	if params.Plan != svc.PlanStarter {
		t.Errorf("plan: got %q", params.Plan)
	}
}

func TestRegisterRequest_ToServiceRegisterParams_InvalidEmail(t *testing.T) {
	req := RegisterRequest(generated.RegisterRequest{
		Email:    openapi_types.Email("not-an-email"),
		Password: "s3cur3pass!",
		Slug:     "acme",
		Plan:     generated.Starter,
	})
	_, err := req.ToServiceRegisterParams()
	if err == nil {
		t.Fatal("expected error for invalid email, got nil")
	}
}

func TestRegisterRequest_ToServiceRegisterParams_ShortPassword(t *testing.T) {
	req := RegisterRequest(generated.RegisterRequest{
		Email:    openapi_types.Email("alice@acme.com"),
		Password: "short",
		Slug:     "acme",
		Plan:     generated.Starter,
	})
	_, err := req.ToServiceRegisterParams()
	if err == nil {
		t.Fatal("expected error for short password, got nil")
	}
}

// ── ToGeneratedRegisterResponse ───────────────────────────────────────────────

func TestToGeneratedRegisterResponse(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	r := svc.RegisterResult{
		UserID:        userID,
		TenantID:      tenantID,
		Slug:          "acme-corp",
		InitialAPIKey: "cf_live_testkey",
	}

	resp := ToGeneratedRegisterResponse(&r)

	if resp.UserId != userID {
		t.Errorf("user_id: got %s, want %s", resp.UserId, userID)
	}
	if resp.TenantId != tenantID {
		t.Errorf("tenant_id: got %s, want %s", resp.TenantId, tenantID)
	}
	if resp.Slug != "acme-corp" {
		t.Errorf("slug: got %q", resp.Slug)
	}
	if resp.InitialApiKey != "cf_live_testkey" {
		t.Errorf("initial_api_key: got %q", resp.InitialApiKey)
	}
}

func TestToGeneratedIssueKeyResponse(t *testing.T) {
	keyID := uuid.New()
	now := time.Now().UTC()
	kr := svc.KeyResult{
		KeyID:       keyID,
		RawKey:      "cf_live_abc",
		DisplayName: "test key",
		Scopes:      "provision:read",
		Status:      "ACTIVE",
		CreatedAt:   now,
	}

	resp := ToGeneratedIssueKeyResponse(&kr)

	if resp.KeyId != keyID {
		t.Errorf("key_id: got %s, want %s", resp.KeyId, keyID)
	}
	if resp.RawKey != "cf_live_abc" {
		t.Errorf("raw_key: got %q", resp.RawKey)
	}
	if resp.Status != generated.IssueKeyResponseStatusACTIVE {
		t.Errorf("status: got %q, want ACTIVE", resp.Status)
	}
	if resp.Scopes != "provision:read" {
		t.Errorf("scopes: got %q", resp.Scopes)
	}
}

package accounts

import (
	"fmt"
	"net/mail"

	"github.com/jtomasevic/cloud-forge/services/accounts/generated"
	svc "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

// ToGeneratedAccountResponse converts a service-layer AccountResult into the
// generated AccountResponse understood by the strict handler.
func ToGeneratedAccountResponse(a *svc.AccountResult) generated.AccountResponse {
	resp := generated.AccountResponse{
		TenantId:    a.TenantID,
		Slug:        a.Slug,
		DisplayName: a.DisplayName,
		Status:      generated.AccountStatus(a.Status),
		Plan:        a.Plan,
		CreatedAt:   a.CreatedAt,
	}
	if a.PodCIDR != "" {
		resp.PodCidr = &a.PodCIDR
	}
	if a.ServiceCIDR != "" {
		resp.ServiceCidr = &a.ServiceCIDR
	}
	return resp
}

// ToGeneratedIssueKeyResponse converts a service-layer KeyResult into the
// generated IssueKeyResponse type.
func ToGeneratedIssueKeyResponse(k *svc.KeyResult) generated.IssueKeyResponse {
	return generated.IssueKeyResponse{
		KeyId:       k.KeyID,
		RawKey:      k.RawKey,
		DisplayName: k.DisplayName,
		Scopes:      k.Scopes,
		Status:      generated.IssueKeyResponseStatusACTIVE,
		CreatedAt:   k.CreatedAt,
	}
}

// ToServiceRegisterParams converts the REST RegisterRequest into the
// service-layer RegisterParams, applying lightweight validation.
//
// Email format is validated here so the handler can return 422 before
// calling the service layer. Password length is enforced by OpenAPI
// (minLength: 8) but re-checked here for defence-in-depth.
func (r RegisterRequest) ToServiceRegisterParams() (svc.RegisterParams, error) { //nolint:gocritic // hugeParam: RegisterRequest wraps the generated type; pointer receiver would prevent value conversion from generated type
	emailStr := string(r.Email)
	if _, err := mail.ParseAddress(emailStr); err != nil {
		return svc.RegisterParams{}, fmt.Errorf("invalid email address: %w", err)
	}
	if len(r.Password) < 8 {
		return svc.RegisterParams{}, fmt.Errorf("password must be at least 8 characters")
	}
	return svc.RegisterParams{
		Email:       emailStr,
		Password:    r.Password,
		Slug:        r.Slug,
		DisplayName: r.DisplayName,
		Plan:        svc.Plan(r.Plan),
	}, nil
}

// ToGeneratedRegisterResponse converts a service-layer RegisterResult into the
// generated RegisterResponse type. VPC provisioning is not started at
// registration — no job_id is included.
func ToGeneratedRegisterResponse(r *svc.RegisterResult) generated.RegisterResponse {
	return generated.RegisterResponse{
		UserId:        r.UserID,
		TenantId:      r.TenantID,
		Slug:          r.Slug,
		InitialApiKey: r.InitialAPIKey,
	}
}

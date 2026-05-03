// Package accounts implements the REST layer for the CloudForge Accounts API.
//
// It maps HTTP requests and responses to/from the service layer (service.AccountsService)
// using the generated types from services/accounts/generated (oapi-codegen output).
//
// Architecture (from docs/general/webappsec.md):
//
//	Request → handler.go (REST, generated types)
//	       → service/service.go (business logic, service types)
//	       → internal/accounts (ScyllaDB store)
//	       → services/provisioner/service (VPC provisioner library)
package accounts

import (
	"context"
	"log/slog"

	"github.com/jtomasevic/cloud-forge/services/accounts/generated"
	svc "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

// Handler implements generated.StrictServerInterface. It translates between
// the generated HTTP request/response types and the service-layer API.
// It must never contain business logic — only translation and error mapping.
type Handler struct {
	svc svc.AccountsService
	log *slog.Logger
}

// NewHandler returns a Handler wired to the given AccountsService.
func NewHandler(service svc.AccountsService, log *slog.Logger) *Handler {
	return &Handler{svc: service, log: log}
}

// ── ProvisionNetwork: POST /accounts/{tenant_slug}/provision ─────────────────

// ProvisionNetwork handles requests to start the VPC provisioning workflow for
// an existing tenant account. The account must have been created via Register.
// Returns 202 Accepted immediately — provisioning is async.
func (h *Handler) ProvisionNetwork(ctx context.Context, req generated.ProvisionNetworkRequestObject) (generated.ProvisionNetworkResponseObject, error) {
	result, err := h.svc.ProvisionNetwork(ctx, req.TenantSlug)
	if err != nil {
		code, body := HTTPStatusAndBody(err)
		switch code {
		case 404:
			return generated.ProvisionNetwork404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(body)}, nil
		case 409:
			return generated.ProvisionNetwork409JSONResponse{ConflictJSONResponse: generated.ConflictJSONResponse(body)}, nil
		default:
			return generated.ProvisionNetwork500JSONResponse{InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body)}, nil
		}
	}

	return generated.ProvisionNetwork202JSONResponse(generated.CreateAccountAccepted{
		TenantId: result.TenantID,
		Slug:     result.Slug,
		Status:   generated.AccountStatus(result.Status),
		JobId:    result.JobID,
	}), nil
}

// ── GetAccount: GET /accounts/{tenant_slug} ───────────────────────────────────

// GetAccount retrieves the current state of a tenant account.
func (h *Handler) GetAccount(ctx context.Context, req generated.GetAccountRequestObject) (generated.GetAccountResponseObject, error) {
	result, err := h.svc.GetAccount(ctx, req.TenantSlug)
	if err != nil {
		code, body := HTTPStatusAndBody(err)
		switch code {
		case 404:
			return generated.GetAccount404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(body)}, nil
		default:
			return generated.GetAccount500JSONResponse{InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body)}, nil
		}
	}

	return generated.GetAccount200JSONResponse(ToGeneratedAccountResponse(&result)), nil
}

// ── DeleteAccount: DELETE /accounts/{tenant_slug} ────────────────────────────

// DeleteAccount initiates account deprovisioning. Returns 202 Accepted
// immediately; the teardown workflow runs asynchronously.
func (h *Handler) DeleteAccount(ctx context.Context, req generated.DeleteAccountRequestObject) (generated.DeleteAccountResponseObject, error) {
	result, err := h.svc.DeleteAccount(ctx, req.TenantSlug)
	if err != nil {
		code, body := HTTPStatusAndBody(err)
		switch code {
		case 404:
			return generated.DeleteAccount404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(body)}, nil
		default:
			return generated.DeleteAccount500JSONResponse{InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body)}, nil
		}
	}

	return generated.DeleteAccount202JSONResponse(generated.DeleteAccountAccepted{
		Slug:   result.Slug,
		JobId:  result.JobID,
		Status: generated.QUEUED,
	}), nil
}

// ── IssueKey: POST /accounts/{tenant_slug}/keys ───────────────────────────────

// IssueKey generates a new API key for the tenant. The raw key in the response
// is present only once — it is never stored and cannot be retrieved again.
func (h *Handler) IssueKey(ctx context.Context, req generated.IssueKeyRequestObject) (generated.IssueKeyResponseObject, error) {
	if req.Body == nil {
		return generated.IssueKey400JSONResponse{BadRequestJSONResponse: generated.BadRequestJSONResponse(BadRequestBody("request body required"))}, nil
	}

	scopes := ""
	if req.Body.Scopes != nil {
		scopes = *req.Body.Scopes
	}

	result, err := h.svc.IssueKey(ctx, svc.IssueKeyParams{
		TenantSlug:  req.TenantSlug,
		DisplayName: req.Body.DisplayName,
		Scopes:      scopes,
	})
	if err != nil {
		code, body := HTTPStatusAndBody(err)
		switch code {
		case 404:
			return generated.IssueKey404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(body)}, nil
		case 422:
			return generated.IssueKey422JSONResponse{UnprocessableEntityJSONResponse: generated.UnprocessableEntityJSONResponse(body)}, nil
		default:
			return generated.IssueKey500JSONResponse{InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body)}, nil
		}
	}

	return generated.IssueKey201JSONResponse(ToGeneratedIssueKeyResponse(&result)), nil
}

// ── Register: POST /register ──────────────────────────────────────────────────

// Register handles self-service user registration.
//
// This is a public endpoint (no Bearer key required). It validates the request,
// delegates to the service layer, and returns 201 Created with the initial API
// key the user must persist immediately.
func (h *Handler) Register(ctx context.Context, req generated.RegisterRequestObject) (generated.RegisterResponseObject, error) {
	if req.Body == nil {
		return generated.Register400JSONResponse{BadRequestJSONResponse: generated.BadRequestJSONResponse(BadRequestBody("request body required"))}, nil
	}

	params, err := RegisterRequest(*req.Body).ToServiceRegisterParams()
	if err != nil {
		return generated.Register422JSONResponse{UnprocessableEntityJSONResponse: generated.UnprocessableEntityJSONResponse(ValidationBody(err.Error()))}, nil
	}

	result, svcErr := h.svc.Register(ctx, params)
	if svcErr != nil {
		code, body := HTTPStatusAndBody(svcErr)
		switch code {
		case 409:
			return generated.Register409JSONResponse(body), nil
		case 422:
			return generated.Register422JSONResponse{UnprocessableEntityJSONResponse: generated.UnprocessableEntityJSONResponse(body)}, nil
		default:
			return generated.Register500JSONResponse{InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body)}, nil
		}
	}

	return generated.Register201JSONResponse(ToGeneratedRegisterResponse(&result)), nil
}

// ── RevokeKey: DELETE /accounts/{tenant_slug}/keys/{key_id} ──────────────────

// RevokeKey permanently revokes the identified API key. Revocation is
// idempotent — revoking an already-revoked key also returns 204.
func (h *Handler) RevokeKey(ctx context.Context, req generated.RevokeKeyRequestObject) (generated.RevokeKeyResponseObject, error) {
	err := h.svc.RevokeKey(ctx, req.TenantSlug, req.KeyId)
	if err != nil {
		code, body := HTTPStatusAndBody(err)
		switch code {
		case 404:
			return generated.RevokeKey404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(body)}, nil
		default:
			return generated.RevokeKey500JSONResponse{InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body)}, nil
		}
	}

	return generated.RevokeKey204Response{}, nil
}

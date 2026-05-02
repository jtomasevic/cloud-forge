package provisioner

import (
	"context"
	"errors"
	"regexp"

	"github.com/google/uuid"

	"github.com/jtomasevic/cloud-forge/services/provisioner/generated"
	svc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// Handler implements generated.StrictServerInterface.
// Every method's responsibility is limited to:
//  1. Decode the incoming request (done by oapi-codegen before we are called)
//  2. Validate REST-layer constraints (format, allowed values)
//  3. Transform to service-layer params using models_transform.go
//  4. Call the service
//  5. Transform the result back to a generated response type
//  6. Map service errors to HTTP status codes using errors.go
//
// No business logic belongs here.
type Handler struct {
	svc svc.ProvisionerService
}

// NewHandler returns a Handler wired to the given service implementation.
func NewHandler(s svc.ProvisionerService) *Handler {
	return &Handler{svc: s}
}

// tenantIDPattern enforces the DNS label format required by both the OpenAPI
// spec (pattern constraint) and the Kubernetes namespace naming rules.
var tenantIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)

// ── StrictServerInterface implementation ────────────────────────────────────

// ProvisionVPC handles POST /vpc/provision.
func (h *Handler) ProvisionVPC(
	ctx context.Context,
	req generated.ProvisionVPCRequestObject,
) (generated.ProvisionVPCResponseObject, error) {
	if req.Body == nil {
		return generated.ProvisionVPC400JSONResponse{
			BadRequestJSONResponse: generated.BadRequestJSONResponse(BadRequestBody("request body is required")),
		}, nil
	}

	// REST-layer validation: enforce DNS label format beyond what the OpenAPI
	// pattern already checks (oapi-codegen validates minLength/maxLength/pattern
	// from the spec; we add the logical constraint that the slug must not be empty).
	if !tenantIDPattern.MatchString(req.Body.TenantId) {
		return generated.ProvisionVPC422JSONResponse{
			UnprocessableEntityJSONResponse: generated.UnprocessableEntityJSONResponse(
				ValidationBody(`tenant_id must be a DNS label: lowercase alphanumeric and hyphens, ` +
					`cannot start or end with a hyphen`),
			),
		}, nil
	}

	// Transform: REST → service model.
	params := ProvisionRequest{
		TenantID:    req.Body.TenantId,
		DisplayName: req.Body.DisplayName,
		Plan:        string(req.Body.Plan),
	}.ToServiceProvisionParams()

	jobID, err := h.svc.Provision(ctx, params)
	if err != nil {
		status, body := HTTPStatusAndBody(err)
		switch status {
		case 409:
			return generated.ProvisionVPC409JSONResponse{
				ConflictJSONResponse: generated.ConflictJSONResponse(body),
			}, nil
		case 503:
			return generated.ProvisionVPC503JSONResponse{
				ServiceUnavailableJSONResponse: generated.ServiceUnavailableJSONResponse(body),
			}, nil
		default:
			return generated.ProvisionVPC500JSONResponse{
				InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body),
			}, nil
		}
	}

	return generated.ProvisionVPC202JSONResponse(generated.JobAccepted{
		JobId:  jobID, // openapi_types.UUID is a type alias for uuid.UUID
		Status: generated.JobAcceptedStatusQUEUED,
	}), nil
}

// GetJob handles GET /vpc/jobs/{job_id}.
func (h *Handler) GetJob(
	ctx context.Context,
	req generated.GetJobRequestObject,
) (generated.GetJobResponseObject, error) {
	jobID, err := uuid.Parse(req.JobId.String())
	if err != nil {
		return generated.GetJob400JSONResponse{
			BadRequestJSONResponse: generated.BadRequestJSONResponse(BadRequestBody("job_id must be a valid UUID")),
		}, nil
	}

	result, err := h.svc.GetJob(ctx, jobID)
	if errors.Is(err, svc.ErrJobNotFound) {
		return generated.GetJob404JSONResponse{
			NotFoundJSONResponse: generated.NotFoundJSONResponse(
				generated.ErrorResponse{Error: generated.ErrorDetail{
					Code:    generated.NOTFOUND,
					Message: "job not found",
				}},
			),
		}, nil
	}
	if err != nil {
		_, body := HTTPStatusAndBody(err)
		return generated.GetJob500JSONResponse{
			InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body),
		}, nil
	}

	return generated.GetJob200JSONResponse(ToGeneratedJobResponse(result)), nil
}

// DeprovisionVPC handles DELETE /vpc/{tenant_id}.
func (h *Handler) DeprovisionVPC(
	ctx context.Context,
	req generated.DeprovisionVPCRequestObject,
) (generated.DeprovisionVPCResponseObject, error) {
	tenantSlug := req.TenantId
	if !tenantIDPattern.MatchString(tenantSlug) {
		return generated.DeprovisionVPC400JSONResponse{
			BadRequestJSONResponse: generated.BadRequestJSONResponse(BadRequestBody("tenant_id path parameter is invalid")),
		}, nil
	}

	jobID, err := h.svc.Deprovision(ctx, svc.DeprovisionParams{TenantSlug: tenantSlug})
	if errors.Is(err, svc.ErrTenantNotFound) {
		return generated.DeprovisionVPC404JSONResponse{
			NotFoundJSONResponse: generated.NotFoundJSONResponse(
				generated.ErrorResponse{Error: generated.ErrorDetail{
					Code:    generated.NOTFOUND,
					Message: "tenant not found",
				}},
			),
		}, nil
	}
	if err != nil {
		_, body := HTTPStatusAndBody(err)
		return generated.DeprovisionVPC500JSONResponse{
			InternalErrorJSONResponse: generated.InternalErrorJSONResponse(body),
		}, nil
	}

	return generated.DeprovisionVPC202JSONResponse(generated.DeprovisionAccepted{
		JobId:    jobID, // openapi_types.UUID is a type alias for uuid.UUID
		Status:   generated.DeprovisionAcceptedStatusQUEUED,
		TenantId: tenantSlug,
	}), nil
}

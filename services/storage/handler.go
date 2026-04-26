package storage

import (
	"context"
	"net/http"

	cferrors "github.com/jtomasevic/cloud-forge/internal/errors"
	"github.com/jtomasevic/cloud-forge/services/storage/generated"
)

// Handler is the placeholder implementation of [generated.StrictServerInterface].
//
// All methods return 501 Not Implemented until real MinIO-backed logic is
// wired in during Phase 1. The struct exists to:
//  1. Prove that the generated interface is compilable and correctly typed.
//  2. Show the shape that each operation's request/response objects take.
//  3. Provide a scaffold that Phase 1 contributors can fill in without
//     needing to understand oapi-codegen internals.
//
// # How to implement an operation
//
// Each method receives a strongly-typed *RequestObject and must return a
// strongly-typed *ResponseObject. For example, to implement ListBuckets:
//
//	func (h *Handler) ListBuckets(ctx context.Context, req generated.ListBucketsRequestObject) (generated.ListBucketsResponseObject, error) {
//	    tenant := req.Tenant   // string — from path {tenant}
//	    project := req.Project // string — from path {project}
//	    buckets, err := h.store.List(ctx, tenant, project)
//	    if err != nil {
//	        return generated.ListBuckets500JSONResponse{...}, nil
//	    }
//	    return generated.ListBuckets200JSONResponse{Items: buckets, Total: len(buckets)}, nil
//	}
type Handler struct{}

// NewHandler constructs a new placeholder Handler.
// Replace this constructor with one that accepts real dependencies (MinIO
// client, logger, etc.) when implementing Phase 1.
func NewHandler() *Handler {
	return &Handler{}
}

// notImplemented is a convenience helper that writes a 501 Not Implemented
// response using the platform error shape. All placeholder operations call
// this until real logic is added.
func notImplemented(w http.ResponseWriter, r *http.Request) {
	// Use the platform error helper so clients always see the standard JSON
	// envelope even for unimplemented endpoints.
	cferrors.WriteJSON(w, r, &cferrors.Error{
		Code:    "NOT_IMPLEMENTED",
		Message: "this operation is not yet implemented",
		Status:  http.StatusNotImplemented,
	})
}

// ── StrictServerInterface implementation ─────────────────────────────────────

// ListBuckets returns all buckets for a project.
// Phase 1: call MinIO ListBuckets filtered by tenant/project prefix.
func (h *Handler) ListBuckets(
	_ context.Context,
	req generated.ListBucketsRequestObject,
) (generated.ListBucketsResponseObject, error) {
	// Placeholder — Phase 1 will replace this with a real MinIO call.
	// req.Tenant and req.Project are available when ready.
	_ = req.Tenant
	_ = req.Project
	return generated.ListBuckets500JSONResponse{
		InternalErrorJSONResponse: generated.InternalErrorJSONResponse{
			Error: generated.ErrorDetail{
				Code:    generated.INTERNALERROR,
				Message: "not yet implemented",
			},
		},
	}, nil
}

// CreateBucket creates a new bucket for a project.
// Phase 1: call MinIO MakeBucket with tenant/project-scoped name.
func (h *Handler) CreateBucket(
	_ context.Context,
	req generated.CreateBucketRequestObject,
) (generated.CreateBucketResponseObject, error) {
	_ = req.Tenant
	_ = req.Project
	_ = req.Body
	return generated.CreateBucket500JSONResponse{
		InternalErrorJSONResponse: generated.InternalErrorJSONResponse{
			Error: generated.ErrorDetail{
				Code:    generated.INTERNALERROR,
				Message: "not yet implemented",
			},
		},
	}, nil
}

// GetBucket returns metadata for a single bucket.
// Phase 1: call MinIO BucketExists / GetBucketInfo.
func (h *Handler) GetBucket(
	_ context.Context,
	req generated.GetBucketRequestObject,
) (generated.GetBucketResponseObject, error) {
	_ = req.Tenant
	_ = req.Project
	_ = req.Name
	return generated.GetBucket500JSONResponse{
		InternalErrorJSONResponse: generated.InternalErrorJSONResponse{
			Error: generated.ErrorDetail{
				Code:    generated.INTERNALERROR,
				Message: "not yet implemented",
			},
		},
	}, nil
}

// DeleteBucket permanently removes a bucket and its contents.
// Phase 1: call MinIO RemoveBucket (or RemoveBucketWithOptions for force-delete).
func (h *Handler) DeleteBucket(
	_ context.Context,
	req generated.DeleteBucketRequestObject,
) (generated.DeleteBucketResponseObject, error) {
	_ = req.Tenant
	_ = req.Project
	_ = req.Name
	return generated.DeleteBucket500JSONResponse{
		InternalErrorJSONResponse: generated.InternalErrorJSONResponse{
			Error: generated.ErrorDetail{
				Code:    generated.INTERNALERROR,
				Message: "not yet implemented",
			},
		},
	}, nil
}

// notImplemented is kept for use in future HTTP-level handler helpers that
// bypass the strict interface (e.g. health-check endpoints added directly to
// the mux). It is intentionally defined here to keep all 501 logic in one place.
var _ = notImplemented

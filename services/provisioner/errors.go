package provisioner

import (
	"errors"
	"net/http"

	"github.com/jtomasevic/cloud-forge/services/provisioner/generated"
	svc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// ErrorBody is the JSON payload written on every non-2xx response.
// It wraps the generated ErrorResponse so the REST layer stays coupled only
// to the generated types.
type ErrorBody = generated.ErrorResponse

// HTTPStatusAndBody maps a service-layer error to the correct HTTP status
// code and a typed ErrorBody. The error cause is always preserved in the
// message so callers can correlate logs.
//
// Mapping:
//   - ErrTenantAlreadyExists → 409 Conflict
//   - ErrTenantNotFound      → 404 Not Found
//   - ErrJobNotFound         → 404 Not Found
//   - ErrCIDRExhausted       → 503 Service Unavailable
//   - anything else          → 500 Internal Server Error
func HTTPStatusAndBody(err error) (int, ErrorBody) {
	switch {
	case errors.Is(err, svc.ErrTenantAlreadyExists):
		return http.StatusConflict, newBody(generated.CONFLICT, err.Error())

	case errors.Is(err, svc.ErrTenantNotFound):
		return http.StatusNotFound, newBody(generated.NOTFOUND, err.Error())

	case errors.Is(err, svc.ErrJobNotFound):
		return http.StatusNotFound, newBody(generated.NOTFOUND, err.Error())

	case errors.Is(err, svc.ErrCIDRExhausted):
		return http.StatusServiceUnavailable, newBody(generated.SERVICEUNAVAILABLE, err.Error())

	default:
		return http.StatusInternalServerError, newBody(generated.INTERNALERROR, "internal server error")
	}
}

// ValidationBody returns the standard 422 body for business-rule violations
// (e.g. invalid tenant_id format, unknown plan) that do not map to a
// service-layer sentinel error.
func ValidationBody(msg string) ErrorBody {
	return newBody(generated.UNPROCESSABLE, msg)
}

// BadRequestBody returns the standard 400 body for request-parsing failures.
func BadRequestBody(msg string) ErrorBody {
	return newBody(generated.BADREQUEST, msg)
}

func newBody(code generated.ErrorDetailCode, msg string) ErrorBody {
	return ErrorBody{
		Error: generated.ErrorDetail{
			Code:    code,
			Message: msg,
		},
	}
}

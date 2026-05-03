package accounts

import (
	"errors"
	"net/http"

	"github.com/jtomasevic/cloud-forge/services/accounts/generated"
	svc "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

// ErrorBody is the JSON payload written on every non-2xx response.
type ErrorBody = generated.ErrorResponse

// HTTPStatusAndBody maps a service-layer error to the correct HTTP status code
// and a typed ErrorBody.
//
// Mapping:
//   - ErrAccountAlreadyExists    → 409 Conflict
//   - ErrEmailAlreadyRegistered  → 409 Conflict
//   - ErrAccountNotFound         → 404 Not Found
//   - ErrKeyNotFound             → 404 Not Found
//   - ErrAccountNotActive        → 422 Unprocessable Entity
//   - anything else              → 500 Internal Server Error
func HTTPStatusAndBody(err error) (status int, body ErrorBody) {
	switch {
	case errors.Is(err, svc.ErrAccountAlreadyExists):
		return http.StatusConflict, newBody(generated.CONFLICT, err.Error())

	case errors.Is(err, svc.ErrEmailAlreadyRegistered):
		return http.StatusConflict, newBody(generated.CONFLICT, err.Error())

	case errors.Is(err, svc.ErrAccountNotFound):
		return http.StatusNotFound, newBody(generated.NOTFOUND, err.Error())

	case errors.Is(err, svc.ErrKeyNotFound):
		return http.StatusNotFound, newBody(generated.NOTFOUND, err.Error())

	case errors.Is(err, svc.ErrAccountNotActive):
		return http.StatusUnprocessableEntity, newBody(generated.UNPROCESSABLE, err.Error())

	default:
		return http.StatusInternalServerError, newBody(generated.INTERNALERROR, "internal server error")
	}
}

// ValidationBody returns the standard 422 body for business-rule violations
// that do not map to a service-layer sentinel.
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

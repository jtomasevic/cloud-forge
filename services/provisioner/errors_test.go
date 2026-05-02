package provisioner_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	. "github.com/jtomasevic/cloud-forge/services/provisioner"
	"github.com/jtomasevic/cloud-forge/services/provisioner/generated"
	svc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

func TestHTTPStatusAndBody_AllMappings(t *testing.T) {
	tests := []struct { //nolint:govet // field order optimised for readability
		name       string
		err        error
		wantStatus int
		wantCode   generated.ErrorDetailCode
	}{
		{
			name:       "tenant_already_exists",
			err:        svc.ErrTenantAlreadyExists,
			wantStatus: http.StatusConflict,
			wantCode:   generated.CONFLICT,
		},
		{
			name:       "tenant_not_found",
			err:        svc.ErrTenantNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   generated.NOTFOUND,
		},
		{
			name:       "job_not_found",
			err:        svc.ErrJobNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   generated.NOTFOUND,
		},
		{
			name:       "cidr_exhausted",
			err:        svc.ErrCIDRExhausted,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   generated.SERVICEUNAVAILABLE,
		},
		{
			name:       "unknown_error_defaults_to_500",
			err:        errors.New("unexpected db failure"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   generated.INTERNALERROR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := HTTPStatusAndBody(tt.err)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantCode, body.Error.Code)
		})
	}
}

func TestValidationBody_ReturnsUnprocessable(t *testing.T) {
	body := ValidationBody("tenant_id must be lowercase")
	assert.Equal(t, generated.UNPROCESSABLE, body.Error.Code)
	assert.Equal(t, "tenant_id must be lowercase", body.Error.Message)
}

func TestBadRequestBody_ReturnsBadRequest(t *testing.T) {
	body := BadRequestBody("missing required field plan")
	assert.Equal(t, generated.BADREQUEST, body.Error.Code)
	assert.Equal(t, "missing required field plan", body.Error.Message)
}

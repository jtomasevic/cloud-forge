package accounts_test

import (
	"errors"
	"net/http"
	"testing"

	. "github.com/jtomasevic/cloud-forge/services/accounts"
	"github.com/jtomasevic/cloud-forge/services/accounts/generated"
	svc "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

func TestHTTPStatusAndBody_Mapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   generated.ErrorDetailCode
		wantStatus int
	}{
		{name: "account_exists", err: svc.ErrAccountAlreadyExists, wantStatus: http.StatusConflict, wantCode: generated.CONFLICT},
		{name: "email_registered", err: svc.ErrEmailAlreadyRegistered, wantStatus: http.StatusConflict, wantCode: generated.CONFLICT},
		{name: "account_not_found", err: svc.ErrAccountNotFound, wantStatus: http.StatusNotFound, wantCode: generated.NOTFOUND},
		{name: "key_not_found", err: svc.ErrKeyNotFound, wantStatus: http.StatusNotFound, wantCode: generated.NOTFOUND},
		{name: "account_not_active", err: svc.ErrAccountNotActive, wantStatus: http.StatusUnprocessableEntity, wantCode: generated.UNPROCESSABLE},
		{name: "unknown_error", err: errors.New("oops"), wantStatus: http.StatusInternalServerError, wantCode: generated.INTERNALERROR},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := HTTPStatusAndBody(tt.err)
			if code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", code, tt.wantStatus)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("code: got %q, want %q", body.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestValidationBody(t *testing.T) {
	b := ValidationBody("field X is invalid")
	if b.Error.Code != generated.UNPROCESSABLE {
		t.Errorf("code: got %q, want UNPROCESSABLE", b.Error.Code)
	}
	if b.Error.Message != "field X is invalid" {
		t.Errorf("message: got %q", b.Error.Message)
	}
}

func TestBadRequestBody(t *testing.T) {
	b := BadRequestBody("missing body")
	if b.Error.Code != generated.BADREQUEST {
		t.Errorf("code: got %q, want BAD_REQUEST", b.Error.Code)
	}
	if b.Error.Message != "missing body" {
		t.Errorf("message: got %q", b.Error.Message)
	}
}

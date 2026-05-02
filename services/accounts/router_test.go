package accounts_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	. "github.com/jtomasevic/cloud-forge/services/accounts"
	"github.com/jtomasevic/cloud-forge/services/accounts/generated"
)

// TestNewRouter_HealthzProbe verifies that GET /healthz is served by the
// router without going through the business handler.
func TestNewRouter_HealthzProbe(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := NewHandler(&fakeAccountsService{}, silentLog())
	router := NewRouter(h, silentLog(), reg, "accounts_test_svc")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("healthz: got %d, want 200", rr.Code)
	}
}

// TestOpapiErrorHandler_InvalidPathParam verifies that sending a non-UUID key_id
// triggers the opapiErrorHandler (path-parameter parsing) and returns 400 with
// the platform error envelope. This covers the opapiErrorHandler function.
func TestOpapiErrorHandler_InvalidPathParam(t *testing.T) {
	reg := prometheus.NewRegistry()
	f := &fakeAccountsService{}
	h := NewHandler(f, silentLog())
	router := NewRouter(h, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)), reg, "accounts_test_svc")

	// DELETE /accounts/acme/keys/NOT-A-UUID — the generated wrapper tries to
	// parse the {key_id} path param as uuid.UUID and calls ErrorHandlerFunc on failure.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/accounts/acme/keys/not-a-valid-uuid", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}
}

// TestHandler_IssueKey_NilBody tests the defensive nil-body check in IssueKey.
func TestHandler_IssueKey_NilBody(t *testing.T) {
	h := NewHandler(&fakeAccountsService{}, silentLog())
	resp, err := h.IssueKey(
		newBgCtx(),
		generated.IssueKeyRequestObject{TenantSlug: "acme", Body: nil},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rr := httptest.NewRecorder()
	if visitErr := resp.VisitIssueKeyResponse(rr); visitErr != nil {
		t.Fatalf("visit: %v", visitErr)
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}
}

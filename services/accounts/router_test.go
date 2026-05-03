package accounts_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
	router := NewRouter(h, silentLog(), reg, "accounts_test_svc", nil)

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
	router := NewRouter(h, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)), reg, "accounts_test_svc", nil)

	// DELETE /accounts/acme/keys/NOT-A-UUID — the generated wrapper tries to
	// parse the {key_id} path param as uuid.UUID and calls ErrorHandlerFunc on failure.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/accounts/acme/keys/not-a-valid-uuid", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}
}

// TestNewRouter_OpenAPISpecFound verifies that GET /api/v1/openapi.yaml serves
// the file contents with the correct Content-Type when the spec is present.
// It uses t.Chdir to point the working directory at a temp tree that contains
// the expected relative path.
func TestNewRouter_OpenAPISpecFound(t *testing.T) {
	// Build the directory tree the handler expects: api/accounts/v1/openapi.yaml
	dir := t.TempDir()
	specDir := dir + "/api/accounts/v1"
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specDir+"/openapi.yaml", []byte("openapi: 3.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	reg := prometheus.NewRegistry()
	h := NewHandler(&fakeAccountsService{}, silentLog())
	router := NewRouter(h, silentLog(), reg, "accounts_test_svc", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/openapi.yaml", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("openapi.yaml (found): got %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type: got %q, want application/yaml", ct)
	}
}

// TestNewRouter_OpenAPISpecNotFound verifies that GET /api/v1/openapi.yaml
// returns 404 when the spec file is absent (true in CI and unit-test
// environments where the api/ directory is not present).
func TestNewRouter_OpenAPISpecNotFound(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := NewHandler(&fakeAccountsService{}, silentLog())
	router := NewRouter(h, silentLog(), reg, "accounts_test_svc", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/openapi.yaml", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("openapi.yaml: got %d, want 404", rr.Code)
	}
}

// TestNewRouter_SwaggerUIPage verifies that GET /api/v1/docs returns an HTML
// page containing the Swagger UI bootstrap code.
func TestNewRouter_SwaggerUIPage(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := NewHandler(&fakeAccountsService{}, silentLog())
	router := NewRouter(h, silentLog(), reg, "accounts_test_svc", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/docs", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("docs: got %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want text/html; charset=utf-8", ct)
	}
}

// TestNewRouter_CORSHeadersApplied verifies that the CORS middleware is wired
// when corsOrigins is non-empty and the expected header is present.
func TestNewRouter_CORSHeadersApplied(t *testing.T) {
	const origin = "http://localhost:8096"
	reg := prometheus.NewRegistry()
	h := NewHandler(&fakeAccountsService{}, silentLog())
	router := NewRouter(h, silentLog(), reg, "accounts_test_svc", []string{origin})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	req.Header.Set("Origin", origin)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("healthz with CORS: got %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, origin)
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

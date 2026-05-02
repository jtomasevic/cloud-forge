package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/jtomasevic/cloud-forge/services/accounts"
	"github.com/jtomasevic/cloud-forge/services/accounts/generated"
	svc "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

// errGeneric is an untyped error that triggers the default 500 mapping.
var errGeneric = errors.New("unexpected infrastructure failure")

// ── helpers ───────────────────────────────────────────────────────────────────

// silentLog returns a no-op slog.Logger that discards all output.
func silentLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// newTestMux wraps the handler in the generated routing mux (required since
// NewStrictHandler returns a ServerInterface, not an http.Handler).
func newTestMux(h *Handler) http.Handler {
	return generated.Handler(generated.NewStrictHandler(h, nil))
}

// postJSON executes an HTTP POST request with a JSON body against the handler.
func postJSON(t *testing.T, h *Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)
	return rr
}

// ── Handler nil-body guards ───────────────────────────────────────────────────
//
// The generated strict handler always provides a parsed body before calling
// handler methods, so the req.Body == nil guard in Register is unreachable via
// the normal request path. This test calls the method directly to exercise
// the defensive nil-check branch.

func TestHandler_Register_NilBody_Returns400(t *testing.T) {
	h := NewHandler(&fakeAccountsService{}, silentLog())

	resp, err := h.Register(
		context.Background(),
		generated.RegisterRequestObject{Body: nil},
	)

	require.NoError(t, err, "handler must not return a Go error for nil body")
	_, ok := resp.(generated.Register400JSONResponse)
	assert.True(t, ok, "nil body must produce a 400 response, got %T", resp)
}

// ── ProvisionNetwork ──────────────────────────────────────────────────────────

func TestHandler_ProvisionNetwork_202(t *testing.T) {
	jobID := uuid.New()
	tenantID := uuid.New()
	f := &fakeAccountsService{
		provisionResult: svc.ProvisionNetworkResult{
			TenantID: tenantID,
			Slug:     "acme",
			Status:   "PROVISIONING",
			JobID:    jobID,
		},
	}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/accounts/acme/provision", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", rr.Code)
	}

	var resp generated.CreateAccountAccepted
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Slug != "acme" {
		t.Errorf("slug: got %q, want %q", resp.Slug, "acme")
	}
	if resp.JobId != jobID {
		t.Errorf("job_id: got %s, want %s", resp.JobId, jobID)
	}
}

func TestHandler_ProvisionNetwork_404_NotFound(t *testing.T) {
	f := &fakeAccountsService{provisionErr: svc.ErrAccountNotFound}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/accounts/ghost/provision", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rr.Code)
	}
	assertErrorCode(t, rr.Body.Bytes(), generated.NOTFOUND)
}

func TestHandler_ProvisionNetwork_409_Conflict(t *testing.T) {
	f := &fakeAccountsService{provisionErr: svc.ErrAccountAlreadyExists}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/accounts/dup/provision", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", rr.Code)
	}
	assertErrorCode(t, rr.Body.Bytes(), generated.CONFLICT)
}

func TestHandler_ProvisionNetwork_500_InternalError(t *testing.T) {
	f := &fakeAccountsService{provisionErr: errGeneric}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/accounts/fail/provision", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}
}

// ── GetAccount ────────────────────────────────────────────────────────────────

func TestHandler_GetAccount_200(t *testing.T) {
	f := &fakeAccountsService{getResult: activeAccountResult("acme")}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/accounts/acme", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}

	var resp generated.AccountResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Slug != "acme" {
		t.Errorf("slug: got %q", resp.Slug)
	}
	if resp.Status != generated.AccountStatusACTIVE {
		t.Errorf("status: got %q", resp.Status)
	}
}

func TestHandler_GetAccount_404(t *testing.T) {
	f := &fakeAccountsService{getErr: svc.ErrAccountNotFound}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/accounts/missing", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rr.Code)
	}
	assertErrorCode(t, rr.Body.Bytes(), generated.NOTFOUND)
}

func TestHandler_GetAccount_500(t *testing.T) {
	f := &fakeAccountsService{getErr: errGeneric}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/accounts/broken", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}
}

// ── DeleteAccount ─────────────────────────────────────────────────────────────

func TestHandler_DeleteAccount_202(t *testing.T) {
	jobID := uuid.New()
	f := &fakeAccountsService{
		deleteResult: svc.DeleteAccountResult{Slug: "acme", JobID: jobID},
	}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/accounts/acme", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", rr.Code)
	}

	var resp generated.DeleteAccountAccepted
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.JobId != jobID {
		t.Errorf("job_id: got %s, want %s", resp.JobId, jobID)
	}
}

func TestHandler_DeleteAccount_404(t *testing.T) {
	f := &fakeAccountsService{deleteErr: svc.ErrAccountNotFound}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/accounts/ghost", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rr.Code)
	}
}

func TestHandler_DeleteAccount_500(t *testing.T) {
	f := &fakeAccountsService{deleteErr: errGeneric}
	h := NewHandler(f, silentLog())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/accounts/broken", http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}
}

// ── IssueKey ─────────────────────────────────────────────────────────────────

func TestHandler_IssueKey_201(t *testing.T) {
	keyID := uuid.New()
	f := &fakeAccountsService{
		issueResult: svc.KeyResult{
			KeyID:       keyID,
			RawKey:      "cf_live_test123",
			DisplayName: "CI",
			Scopes:      "provision:read",
			Status:      "ACTIVE",
		},
	}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/accounts/acme/keys", generated.IssueKeyRequest{
		DisplayName: "CI",
	})

	if rr.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", rr.Code)
	}

	var resp generated.IssueKeyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.RawKey != "cf_live_test123" {
		t.Errorf("raw_key: got %q", resp.RawKey)
	}
	if resp.KeyId != keyID {
		t.Errorf("key_id: got %s, want %s", resp.KeyId, keyID)
	}
}

func TestHandler_IssueKey_201_WithScopes(t *testing.T) {
	keyID := uuid.New()
	f := &fakeAccountsService{
		issueResult: svc.KeyResult{
			KeyID:       keyID,
			RawKey:      "cf_live_scoped",
			DisplayName: "CI-scoped",
			Scopes:      "provision:read",
			Status:      "ACTIVE",
		},
	}
	h := NewHandler(f, silentLog())
	scopes := "provision:read"

	rr := postJSON(t, h, "/accounts/acme/keys", generated.IssueKeyRequest{
		DisplayName: "CI-scoped",
		Scopes:      &scopes,
	})

	if rr.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", rr.Code)
	}
	var resp generated.IssueKeyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.RawKey != "cf_live_scoped" {
		t.Errorf("raw_key: got %q", resp.RawKey)
	}
}

func TestHandler_IssueKey_404_TenantNotFound(t *testing.T) {
	f := &fakeAccountsService{issueErr: svc.ErrAccountNotFound}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/accounts/ghost/keys", generated.IssueKeyRequest{DisplayName: "k"})

	if rr.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rr.Code)
	}
}

func TestHandler_IssueKey_422_NotActive(t *testing.T) {
	f := &fakeAccountsService{issueErr: svc.ErrAccountNotActive}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/accounts/prov/keys", generated.IssueKeyRequest{DisplayName: "k"})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", rr.Code)
	}
}

func TestHandler_IssueKey_500(t *testing.T) {
	f := &fakeAccountsService{issueErr: errGeneric}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/accounts/acme/keys", generated.IssueKeyRequest{DisplayName: "k"})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}
}

// ── RevokeKey ─────────────────────────────────────────────────────────────────

func TestHandler_RevokeKey_204(t *testing.T) {
	f := &fakeAccountsService{} // no error
	h := NewHandler(f, silentLog())

	keyID := uuid.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/accounts/acme/keys/"+keyID.String(), http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", rr.Code)
	}
}

func TestHandler_RevokeKey_404(t *testing.T) {
	f := &fakeAccountsService{revokeErr: svc.ErrAccountNotFound}
	h := NewHandler(f, silentLog())

	keyID := uuid.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/accounts/acme/keys/"+keyID.String(), http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rr.Code)
	}
}

func TestHandler_RevokeKey_500(t *testing.T) {
	f := &fakeAccountsService{revokeErr: errGeneric}
	h := NewHandler(f, silentLog())

	keyID := uuid.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/accounts/acme/keys/"+keyID.String(), http.NoBody)
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestHandler_Register_201(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	f := &fakeAccountsService{
		registerResult: svc.RegisterResult{
			UserID:        userID,
			TenantID:      tenantID,
			Slug:          "acme-corp",
			InitialAPIKey: "cf_live_testkey",
		},
	}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/register", generated.RegisterRequest{
		Email:       "alice@acme.com",
		Password:    "s3cur3pass!",
		Slug:        "acme-corp",
		DisplayName: "Acme Corporation",
		Plan:        generated.Starter,
	})

	if rr.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", rr.Code)
	}
	var resp generated.RegisterResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.InitialApiKey != "cf_live_testkey" {
		t.Errorf("initial_api_key: got %q", resp.InitialApiKey)
	}
	if resp.Slug != "acme-corp" {
		t.Errorf("slug: got %q", resp.Slug)
	}
}

func TestHandler_Register_409_EmailTaken(t *testing.T) {
	f := &fakeAccountsService{registerErr: svc.ErrEmailAlreadyRegistered}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/register", generated.RegisterRequest{
		Email:       "taken@acme.com",
		Password:    "s3cur3pass!",
		Slug:        "acme-corp",
		DisplayName: "Acme Corporation",
		Plan:        generated.Starter,
	})

	if rr.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", rr.Code)
	}
	assertErrorCode(t, rr.Body.Bytes(), generated.CONFLICT)
}

func TestHandler_Register_409_SlugTaken(t *testing.T) {
	f := &fakeAccountsService{registerErr: svc.ErrAccountAlreadyExists}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/register", generated.RegisterRequest{
		Email:       "alice@acme.com",
		Password:    "s3cur3pass!",
		Slug:        "taken-slug",
		DisplayName: "Acme",
		Plan:        generated.Starter,
	})

	if rr.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", rr.Code)
	}
}

// TestHandler_Register_422_ShortPassword tests that the handler returns 422
// when a password under 8 characters is submitted.
// Note: invalid email addresses cannot be marshalled into generated.RegisterRequest
// because types.Email validates on JSON marshal. Email format validation is
// covered at the transform layer in TestRegisterRequest_ToServiceRegisterParams_InvalidEmail.
func TestHandler_Register_422_ShortPassword(t *testing.T) {
	h := NewHandler(&fakeAccountsService{}, silentLog())

	// Bypass the generated types by sending raw JSON so we can include a
	// short password that oapi-codegen would normally reject at decode time.
	body := `{"email":"alice@acme.com","password":"short","slug":"acme-corp","display_name":"Acme","plan":"starter"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newTestMux(h).ServeHTTP(rr, req)

	// The generated strict server validates the minimum length (8) from the
	// OpenAPI schema and returns 400 before our handler logic runs.
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 400 or 422", rr.Code)
	}
}

func TestHandler_Register_422_FromService(t *testing.T) {
	// ErrAccountNotActive maps to 422 — covers the case=422 branch in Register handler.
	f := &fakeAccountsService{registerErr: svc.ErrAccountNotActive}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/register", generated.RegisterRequest{
		Email:       "alice@acme.com",
		Password:    "s3cur3pass!",
		Slug:        "acme-corp",
		DisplayName: "Acme",
		Plan:        generated.Starter,
	})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", rr.Code)
	}
}

func TestHandler_Register_500(t *testing.T) {
	f := &fakeAccountsService{registerErr: errGeneric}
	h := NewHandler(f, silentLog())

	rr := postJSON(t, h, "/register", generated.RegisterRequest{
		Email:       "alice@acme.com",
		Password:    "s3cur3pass!",
		Slug:        "acme-corp",
		DisplayName: "Acme",
		Plan:        generated.Starter,
	})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}
}

// ── assertErrorCode ───────────────────────────────────────────────────────────

func assertErrorCode(t *testing.T, body []byte, wantCode generated.ErrorDetailCode) {
	t.Helper()
	var resp generated.ErrorResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.Error.Code != wantCode {
		t.Errorf("error.code: got %q, want %q", resp.Error.Code, wantCode)
	}
}

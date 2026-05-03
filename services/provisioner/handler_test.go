package provisioner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/jtomasevic/cloud-forge/services/provisioner"
	"github.com/jtomasevic/cloud-forge/services/provisioner/generated"
	svc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// ── mock service ──────────────────────────────────────────────────────────────

type fakeProvisionerService struct { //nolint:govet // field order optimised for readability in tests
	provisionResult uuid.UUID
	deprovisionID   uuid.UUID
	getJobResult    svc.JobResult
	provisionErr    error
	getJobErr       error
	deprovisionErr  error
}

func (f *fakeProvisionerService) Provision(_ context.Context, _ svc.ProvisionParams) (uuid.UUID, error) {
	return f.provisionResult, f.provisionErr
}

func (f *fakeProvisionerService) GetJob(_ context.Context, _ uuid.UUID) (svc.JobResult, error) {
	return f.getJobResult, f.getJobErr
}

func (f *fakeProvisionerService) Deprovision(_ context.Context, _ svc.DeprovisionParams) (uuid.UUID, error) {
	return f.deprovisionID, f.deprovisionErr
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newProvisionerMux(s svc.ProvisionerService) http.Handler {
	return generated.Handler(generated.NewStrictHandler(NewHandler(s), nil))
}

func postProvisionJSON(t *testing.T, s svc.ProvisionerService, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newProvisionerMux(s).ServeHTTP(rr, req)
	return rr
}

// ── ProvisionVPC ─────────────────────────────────────────────────────────────

func TestHandler_ProvisionVPC_202(t *testing.T) {
	jobID := uuid.New()
	f := &fakeProvisionerService{provisionResult: jobID}

	rr := postProvisionJSON(t, f, "/provision", generated.ProvisionRequest{
		TenantId:    "acme",
		DisplayName: "Acme Corp",
		Plan:        generated.Starter,
	})

	assert.Equal(t, http.StatusAccepted, rr.Code)
	var resp generated.JobAccepted
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp.JobId)
}

func TestHandler_ProvisionVPC_422_InvalidTenantID(t *testing.T) {
	f := &fakeProvisionerService{}

	rr := postProvisionJSON(t, f, "/provision", generated.ProvisionRequest{
		TenantId:    "INVALID_UPPER", // must be lowercase
		DisplayName: "Bad",
		Plan:        generated.Starter,
	})

	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
}

func TestHandler_ProvisionVPC_409_AlreadyExists(t *testing.T) {
	f := &fakeProvisionerService{provisionErr: svc.ErrTenantAlreadyExists}

	rr := postProvisionJSON(t, f, "/provision", generated.ProvisionRequest{
		TenantId:    "acme",
		DisplayName: "Acme Corp",
		Plan:        generated.Starter,
	})

	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestHandler_ProvisionVPC_503_CIDRExhausted(t *testing.T) {
	f := &fakeProvisionerService{provisionErr: svc.ErrCIDRExhausted}

	rr := postProvisionJSON(t, f, "/provision", generated.ProvisionRequest{
		TenantId:    "acme",
		DisplayName: "Acme Corp",
		Plan:        generated.Starter,
	})

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestHandler_ProvisionVPC_500_InternalError(t *testing.T) {
	f := &fakeProvisionerService{provisionErr: errors.New("unexpected db failure")}

	rr := postProvisionJSON(t, f, "/provision", generated.ProvisionRequest{
		TenantId:    "acme",
		DisplayName: "Acme Corp",
		Plan:        generated.Starter,
	})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ── GetJob ────────────────────────────────────────────────────────────────────

func TestHandler_GetJob_200(t *testing.T) {
	jobID := uuid.New()
	f := &fakeProvisionerService{
		getJobResult: svc.JobResult{JobID: jobID, Status: "QUEUED"},
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/jobs/"+jobID.String(), http.NoBody)
	rr := httptest.NewRecorder()
	newProvisionerMux(f).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp generated.JobResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp.JobId)
}

func TestHandler_GetJob_404_JobNotFound(t *testing.T) {
	jobID := uuid.New()
	f := &fakeProvisionerService{getJobErr: svc.ErrJobNotFound}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/jobs/"+jobID.String(), http.NoBody)
	rr := httptest.NewRecorder()
	newProvisionerMux(f).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestHandler_GetJob_500_InternalError(t *testing.T) {
	jobID := uuid.New()
	f := &fakeProvisionerService{getJobErr: errors.New("db gone")}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/jobs/"+jobID.String(), http.NoBody)
	rr := httptest.NewRecorder()
	newProvisionerMux(f).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ── DeprovisionVPC ────────────────────────────────────────────────────────────

func TestHandler_DeprovisionVPC_202(t *testing.T) {
	jobID := uuid.New()
	f := &fakeProvisionerService{deprovisionID: jobID}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/acme", http.NoBody)
	rr := httptest.NewRecorder()
	newProvisionerMux(f).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Code)
	var resp generated.DeprovisionAccepted
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp.JobId)
}

func TestHandler_DeprovisionVPC_400_InvalidTenantID(t *testing.T) {
	f := &fakeProvisionerService{}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/INVALID", http.NoBody)
	rr := httptest.NewRecorder()
	newProvisionerMux(f).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandler_DeprovisionVPC_404_TenantNotFound(t *testing.T) {
	f := &fakeProvisionerService{deprovisionErr: svc.ErrTenantNotFound}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/acme", http.NoBody)
	rr := httptest.NewRecorder()
	newProvisionerMux(f).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestHandler_DeprovisionVPC_500_InternalError(t *testing.T) {
	f := &fakeProvisionerService{deprovisionErr: errors.New("db gone")}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/acme", http.NoBody)
	rr := httptest.NewRecorder()
	newProvisionerMux(f).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

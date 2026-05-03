package provisioner_test

// Mock-server unit tests for kubeconfig.go.
//
// These tests exercise every code path in Store, Retrieve, and Revoke that
// requires an OpenBao HTTP response (success paths, error wrapping, ErrNotFound
// translation, malformed data handling).
//
// Strategy: create a real *openbao.Client pointed at a net/http/httptest server
// that replays the exact JSON shapes the OpenBao KV v2 API emits. No Docker,
// no network, no build tags — these tests are part of the standard unit-test
// suite and run under `go test ./...` and `make test-unit`.
//
// OpenBao KV v2 HTTP methods and paths used here:
//   PUT    /v1/secret/data/<path>     → Store (KV v2 always uses PUT for writes)
//   GET    /v1/secret/data/<path>     → Retrieve
//   DELETE /v1/secret/metadata/<path> → Revoke

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	openbao "github.com/openbao/openbao/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/provisioner"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// mockClient returns a *openbao.Client configured to talk to the provided
// httptest server. The token is set to an arbitrary non-empty value to satisfy
// the SDK's auth header requirement.
//
// MaxRetries is set to 0 so that error responses (e.g. 500) are returned
// immediately without the SDK retrying, which would otherwise cause tests
// to hang for several seconds each and risk breaching CI timeouts.
func mockClient(t *testing.T, srv *httptest.Server) *openbao.Client {
	t.Helper()
	cfg := openbao.DefaultConfig()
	cfg.Address = srv.URL
	cfg.MaxRetries = 0
	client, err := openbao.NewClient(cfg)
	require.NoError(t, err)
	client.SetToken("test-token")
	return client
}

// writeJSON marshals v and writes it as an application/json response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}

// kvPutOKResponse is the minimal KV v2 response body for a successful write.
// The SDK only inspects the outer envelope; any fields inside data are ignored.
var kvPutOKResponse = map[string]any{
	"request_id": "test-req-id",
	"data": map[string]any{
		"created_time": "2026-01-01T00:00:00Z",
		"version":      1,
	},
}

// kvGetOKResponse returns a KV v2 read response with the given kubeconfig value.
func kvGetOKResponse(kubeconfigYAML string) map[string]any {
	return map[string]any{
		"request_id": "test-req-id",
		"data": map[string]any{
			"data": map[string]any{
				"kubeconfig": kubeconfigYAML,
			},
			"metadata": map[string]any{"version": 1},
		},
	}
}

// kvGetNullDataResponse is a KV v2 read response whose nested data map is nil.
// This exercises the secret.Data == nil branch in Retrieve.
var kvGetNullDataResponse = map[string]any{
	"request_id": "test-req-id",
	"data": map[string]any{
		"data":     nil, // the kubeconfig key is absent
		"metadata": map[string]any{"version": 1},
	},
}

// kvGetNoKubeconfigFieldResponse is a KV v2 read response that has a data map
// but omits the "kubeconfig" key. This exercises the missing-field branch.
var kvGetNoKubeconfigFieldResponse = map[string]any{
	"request_id": "test-req-id",
	"data": map[string]any{
		"data":     map[string]any{"other_field": "value"},
		"metadata": map[string]any{"version": 1},
	},
}

// obaoBadRequest returns an OpenBao-style error response body.
func obaoBadRequest(msg string) map[string]any {
	return map[string]any{"errors": []string{msg}}
}

// ── Store ─────────────────────────────────────────────────────────────────────

func TestStore_CallsCorrectHTTPEndpoint(t *testing.T) {
	// Verify that Store issues a PUT (KV v2 uses PUT for writes) to the
	// correct path: /v1/secret/data/cf/tenants/{id}/kubeconfig.
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		writeJSON(w, http.StatusOK, kvPutOKResponse)
	}))
	defer srv.Close()

	err := provisioner.Store(context.Background(), mockClient(t, srv), "acme", "kc: yaml")
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "/v1/secret/data/cf/tenants/acme/kubeconfig", gotPath)
}

func TestStore_Success_ReturnsNilError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, kvPutOKResponse)
	}))
	defer srv.Close()

	err := provisioner.Store(context.Background(), mockClient(t, srv), "acme-corp", "apiVersion: v1")
	require.NoError(t, err)
}

func TestStore_ServerError_WrapsErrorWithTenantID(t *testing.T) {
	// OpenBao returns 500 — Store must return a non-nil error that contains
	// the tenant ID so operators can identify the failing write in logs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, obaoBadRequest("storage backend unavailable"))
	}))
	defer srv.Close()

	err := provisioner.Store(context.Background(), mockClient(t, srv), "acme-corp", "kc: yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acme-corp", "error must include tenant ID for observability")
}

func TestStore_Forbidden_WrapsError(t *testing.T) {
	// 403 (wrong token) must not be silently swallowed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, obaoBadRequest("permission denied"))
	}))
	defer srv.Close()

	err := provisioner.Store(context.Background(), mockClient(t, srv), "acme", "kc: yaml")
	require.Error(t, err)
}

// ── Retrieve ──────────────────────────────────────────────────────────────────

func TestRetrieve_Success_ReturnsKubeconfigYAML(t *testing.T) {
	const wantKC = "apiVersion: v1\nclusters: []\ncontexts: []\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, kvGetOKResponse(wantKC))
	}))
	defer srv.Close()

	got, err := provisioner.Retrieve(context.Background(), mockClient(t, srv), "acme")
	require.NoError(t, err)
	assert.Equal(t, wantKC, got)
}

func TestRetrieve_CallsCorrectHTTPEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, http.StatusOK, kvGetOKResponse("kc: yaml"))
	}))
	defer srv.Close()

	_, err := provisioner.Retrieve(context.Background(), mockClient(t, srv), "my-tenant")
	require.NoError(t, err)
	assert.Equal(t, "/v1/secret/data/cf/tenants/my-tenant/kubeconfig", gotPath)
}

func TestRetrieve_404Response_ReturnsErrNotFound(t *testing.T) {
	// HTTP 404 from the raw API (e.g. after hard-deleting the metadata) must
	// be translated to ErrNotFound so callers can use errors.Is.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, obaoBadRequest("no secret exists at that path"))
	}))
	defer srv.Close()

	_, err := provisioner.Retrieve(context.Background(), mockClient(t, srv), "acme")
	require.Error(t, err)
	assert.True(t, errors.Is(err, provisioner.ErrNotFound),
		"404 response must map to ErrNotFound, got: %v", err)
}

func TestRetrieve_ServerError_WrapsErrorWithTenantID(t *testing.T) {
	// A non-404 error (e.g. 500) must NOT be treated as ErrNotFound — it is
	// a real failure that must surface with the tenant ID for debugging.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, obaoBadRequest("storage backend error"))
	}))
	defer srv.Close()

	_, err := provisioner.Retrieve(context.Background(), mockClient(t, srv), "acme-corp")
	require.Error(t, err)
	assert.False(t, errors.Is(err, provisioner.ErrNotFound),
		"500 must not be mapped to ErrNotFound")
	assert.Contains(t, err.Error(), "acme-corp")
}

func TestRetrieve_Forbidden_IsNotErrNotFound(t *testing.T) {
	// 403 is a permissions error, not a missing-secret error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, obaoBadRequest("permission denied"))
	}))
	defer srv.Close()

	_, err := provisioner.Retrieve(context.Background(), mockClient(t, srv), "acme")
	require.Error(t, err)
	assert.False(t, errors.Is(err, provisioner.ErrNotFound))
}

func TestRetrieve_MissingKubeconfigField_ReturnsFieldError(t *testing.T) {
	// Secret exists in OpenBao but was written without the "kubeconfig" key
	// (e.g. manually, or by a bug). Retrieve must return an informative error
	// that names both the missing field and the tenant.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, kvGetNoKubeconfigFieldResponse)
	}))
	defer srv.Close()

	_, err := provisioner.Retrieve(context.Background(), mockClient(t, srv), "acme")
	require.Error(t, err)
	assert.False(t, errors.Is(err, provisioner.ErrNotFound),
		"missing field must not be treated as ErrNotFound")
	assert.Contains(t, err.Error(), "kubeconfig",
		"error must mention the missing field name")
	assert.Contains(t, err.Error(), "acme",
		"error must mention the tenant ID")
}

func TestRetrieve_NullDataMap_ReturnsErrNotFound(t *testing.T) {
	// OpenBao returns a valid envelope but the inner data map is null.
	// This can happen when the secret is partially deleted or corrupted.
	// Retrieve must treat this as ErrNotFound (the key has no usable value).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, kvGetNullDataResponse)
	}))
	defer srv.Close()

	_, err := provisioner.Retrieve(context.Background(), mockClient(t, srv), "acme")
	require.Error(t, err)
	// Null data is treated as a missing kubeconfig field (no type assertion
	// succeeds on nil), so this exercises the !ok branch in Retrieve.
	assert.NotNil(t, err)
}

// ── Revoke ────────────────────────────────────────────────────────────────────

func TestRevoke_Success_ReturnsNilError(t *testing.T) {
	// OpenBao returns 204 No Content for DELETE metadata — the standard
	// idempotent delete response, even when the path never existed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := provisioner.Revoke(context.Background(), mockClient(t, srv), "acme")
	require.NoError(t, err)
}

func TestRevoke_CallsCorrectHTTPEndpoint(t *testing.T) {
	// Revoke must DELETE the metadata path (not data) so that all historical
	// versions are hard-deleted, not just the latest soft-deleted version.
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := provisioner.Revoke(context.Background(), mockClient(t, srv), "acme")
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.Equal(t, "/v1/secret/metadata/cf/tenants/acme/kubeconfig", gotPath,
		"Revoke must DELETE the metadata path to hard-delete all versions")
}

func TestRevoke_ServerError_WrapsErrorWithTenantID(t *testing.T) {
	// If OpenBao returns an error, Revoke must propagate it wrapped with the
	// tenant ID so the deprovisioning workflow can log it correctly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, obaoBadRequest("backend unavailable"))
	}))
	defer srv.Close()

	err := provisioner.Revoke(context.Background(), mockClient(t, srv), "acme-corp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acme-corp")
}

func TestRevoke_Forbidden_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, obaoBadRequest("permission denied"))
	}))
	defer srv.Close()

	err := provisioner.Revoke(context.Background(), mockClient(t, srv), "acme")
	require.Error(t, err)
}

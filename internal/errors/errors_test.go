package errors_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cferrors "github.com/jtomasevic/cloud-forge/internal/errors"
)

// TestErrorInterface verifies that *Error satisfies the standard error
// interface and that Error() produces a human-readable string.
func TestErrorInterface(t *testing.T) {
	err := cferrors.NotFound("database-instance", "my-db")

	// Must satisfy the error interface.
	var _ error = err

	// Error() string must include the code and the identifier.
	assert.Contains(t, err.Error(), "RESOURCE_NOT_FOUND")
	assert.Contains(t, err.Error(), "my-db")
}

// TestError_WithoutCause verifies that Error() omits the cause section
// when the Cause field is nil.
func TestError_WithoutCause(t *testing.T) {
	err := cferrors.BadRequest("field required")
	// No cause — must not include a colon-separated cause string.
	assert.NotContains(t, err.Error(), ":")
	assert.Contains(t, err.Error(), "BAD_REQUEST")
}

// TestError_WithCause verifies that Error() includes the underlying cause
// so that plain-string log output still shows the full chain.
func TestError_WithCause(t *testing.T) {
	cause := cferrors.BadRequest("root cause")
	wrapped := cferrors.Internal(cause)
	// The string representation must contain the cause message.
	assert.Contains(t, wrapped.Error(), "root cause")
}

// TestErrorUnwrap verifies that Unwrap exposes the underlying cause so
// that errors.Is / errors.As can traverse the chain.
func TestErrorUnwrap(t *testing.T) {
	underlying := cferrors.BadRequest("missing field")
	wrapped := cferrors.Internal(underlying)

	// The wrapped error should unwrap to the underlying cause.
	assert.ErrorIs(t, wrapped, underlying)
}

// TestConstructors verifies that each constructor sets the expected HTTP
// status code and error code.
func TestConstructors(t *testing.T) {
	tests := []struct {
		name           string
		err            *cferrors.Error
		wantCode       string
		wantHTTPStatus int
	}{
		{
			name:           "NotFound",
			err:            cferrors.NotFound("bucket", "my-bucket"),
			wantCode:       cferrors.CodeNotFound,
			wantHTTPStatus: http.StatusNotFound,
		},
		{
			name:           "Unauthorized",
			err:            cferrors.Unauthorized("token expired"),
			wantCode:       cferrors.CodeUnauthorized,
			wantHTTPStatus: http.StatusUnauthorized,
		},
		{
			name:           "Forbidden",
			err:            cferrors.Forbidden("user:alice", "storage:write", "cf://t1/p1/buckets/b"),
			wantCode:       cferrors.CodeForbidden,
			wantHTTPStatus: http.StatusForbidden,
		},
		{
			name:           "BadRequest",
			err:            cferrors.BadRequest("field 'name' is required"),
			wantCode:       cferrors.CodeBadRequest,
			wantHTTPStatus: http.StatusBadRequest,
		},
		{
			name:           "Internal",
			err:            cferrors.Internal(cferrors.BadRequest("root cause")),
			wantCode:       cferrors.CodeInternal,
			wantHTTPStatus: http.StatusInternalServerError,
		},
		{
			name:           "Conflict",
			err:            cferrors.Conflict("tenant", "acme"),
			wantCode:       cferrors.CodeConflict,
			wantHTTPStatus: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantCode, tc.err.Code)
			assert.Equal(t, tc.wantHTTPStatus, tc.err.Status)
			assert.NotEmpty(t, tc.err.Message)
		})
	}
}

// TestIsNotFound verifies that IsNotFound returns true only for not-found errors.
func TestIsNotFound(t *testing.T) {
	assert.True(t, cferrors.IsNotFound(cferrors.NotFound("bucket", "b")))
	assert.False(t, cferrors.IsNotFound(cferrors.Unauthorized("expired")))
	assert.False(t, cferrors.IsNotFound(nil))
}

// TestIsForbidden verifies that IsForbidden returns true only for forbidden errors.
func TestIsForbidden(t *testing.T) {
	assert.True(t, cferrors.IsForbidden(cferrors.Forbidden("user:alice", "write", "cf://res")))
	assert.False(t, cferrors.IsForbidden(cferrors.NotFound("x", "y")))
	assert.False(t, cferrors.IsForbidden(nil))
}

// TestIsUnauthorized verifies that IsUnauthorized returns true only for unauthorized errors.
func TestIsUnauthorized(t *testing.T) {
	assert.True(t, cferrors.IsUnauthorized(cferrors.Unauthorized("token expired")))
	assert.False(t, cferrors.IsUnauthorized(cferrors.Forbidden("u", "a", "r")))
	assert.False(t, cferrors.IsUnauthorized(nil))
}

// TestWriteJSON verifies that WriteJSON produces a well-formed JSON
// response with the correct status code and content-type header.
func TestWriteJSON(t *testing.T) {
	e := cferrors.NotFound("database-instance", "my-db")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/database/v1/tenant/project/instances/my-db", http.NoBody)
	w := httptest.NewRecorder()

	cferrors.WriteJSON(w, req, e)

	resp := w.Result()

	// HTTP status must match the error's Status field.
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Content-Type must be JSON.
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	// Body must be valid JSON with the expected structure.
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, cferrors.CodeNotFound, body.Error.Code)
	assert.NotEmpty(t, body.Error.Message)
}

// TestWriteJSON_Nil verifies that WriteJSON handles a nil error gracefully
// by writing 200 OK without panicking.
func TestWriteJSON_Nil(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()

	// Must not panic.
	cferrors.WriteJSON(w, req, nil)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHTTPStatusFor verifies the status code helper for various inputs.
func TestHTTPStatusFor(t *testing.T) {
	assert.Equal(t, http.StatusOK, cferrors.HTTPStatusFor(nil))
	assert.Equal(t, http.StatusNotFound, cferrors.HTTPStatusFor(cferrors.NotFound("x", "y")))
	assert.Equal(t, http.StatusInternalServerError,
		cferrors.HTTPStatusFor(cferrors.Internal(cferrors.BadRequest("nested"))))

	// A plain stdlib error (not a *cferrors.Error) must map to 500.
	assert.Equal(t, http.StatusInternalServerError,
		cferrors.HTTPStatusFor(fmt.Errorf("plain error")))
}

// TestWriteJSON_InjectsRequestID verifies that WriteJSON reads the
// X-Request-ID header (set by the RequestID middleware) and includes it in
// the response body so callers can correlate errors with platform logs.
func TestWriteJSON_InjectsRequestID(t *testing.T) {
	const reqID = "test-request-id-123"

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()

	// Simulate what the RequestID middleware does — set the header on the
	// response writer before calling the handler.
	w.Header().Set("X-Request-ID", reqID)

	cferrors.WriteJSON(w, req, cferrors.NotFound("thing", "x"))

	var body struct {
		Error struct {
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, reqID, body.Error.RequestID)
}

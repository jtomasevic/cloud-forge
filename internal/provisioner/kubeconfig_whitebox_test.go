package provisioner

// Whitebox unit tests for unexported helpers in kubeconfig.go.
//
// These tests access package-private functions directly (same package name,
// no _test suffix) to cover branches that cannot be reached from the public
// API without a live OpenBao server. No build tag required — they run as part
// of the standard unit test suite (go test -short ./...).

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	openbao "github.com/openbao/openbao/api/v2"
	"github.com/stretchr/testify/assert"
)

// ── isNotFound ────────────────────────────────────────────────────────────────

func TestIsNotFound_NilError_ReturnsFalse(t *testing.T) {
	assert.False(t, isNotFound(nil))
}

func TestIsNotFound_Generic404ResponseError_ReturnsTrue(t *testing.T) {
	// Simulate the *openbao.ResponseError that the SDK emits for HTTP 404
	// responses from the raw Logical client (e.g. when metadata path is deleted).
	// URL is a plain string field in the openbao SDK (not *url.URL).
	re := &openbao.ResponseError{
		StatusCode: http.StatusNotFound,
		Errors:     []string{"no secret exists at that path"},
		URL:        "http://localhost:8200/v1/secret/metadata/cf/tenants/x/kubeconfig",
	}
	assert.True(t, isNotFound(re), "404 ResponseError must be recognised as not-found")
}

func TestIsNotFound_Non404ResponseError_ReturnsFalse(t *testing.T) {
	re := &openbao.ResponseError{
		StatusCode: http.StatusForbidden,
		Errors:     []string{"permission denied"},
		URL:        "http://localhost:8200/v1/secret/data/cf/tenants/x/kubeconfig",
	}
	assert.False(t, isNotFound(re), "403 ResponseError must not be recognised as not-found")
}

func TestIsNotFound_SecretNotFoundTextPrefix_ReturnsTrue(t *testing.T) {
	// The KVv2.Get helper emits this text error when the underlying Logical.Read
	// returns nil (path was never written to).
	err := fmt.Errorf("secret not found: at secret/data/cf/tenants/acme/kubeconfig")
	assert.True(t, isNotFound(err))
}

func TestIsNotFound_OtherTextError_ReturnsFalse(t *testing.T) {
	err := errors.New("connection refused")
	assert.False(t, isNotFound(err))
}

// ── kvPath ────────────────────────────────────────────────────────────────────

func TestKVPath_ProducesExpectedFormat(t *testing.T) {
	assert.Equal(t, "cf/tenants/acme-corp/kubeconfig", kvPath("acme-corp"))
	assert.Equal(t, "cf/tenants/t/kubeconfig", kvPath("t"))
	assert.Equal(t, "cf/tenants/tenant1/kubeconfig", kvPath("tenant1"))
}

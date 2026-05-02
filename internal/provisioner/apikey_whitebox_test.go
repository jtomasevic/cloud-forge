package provisioner

// Whitebox tests for unexported helpers in apikey.go and for GenerateAPIKey
// with a fake APIKeyStorer.
// These tests live in the provisioner package (not provisioner_test) so they
// can access newRawKeyAndHash without exporting it.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// ── fakeAPIKeyStore ───────────────────────────────────────────────────────────

// fakeAPIKeyStore is a test double for APIKeyStorer that records the stored
// key and optionally returns a configured error.
type fakeAPIKeyStore struct {
	err    error
	stored *accounts.APIKey
}

func (f *fakeAPIKeyStore) Store(_ context.Context, k *accounts.APIKey) error {
	if f.err != nil {
		return f.err
	}
	f.stored = k
	return nil
}

// ── GenerateAPIKey tests ──────────────────────────────────────────────────────

// TestGenerateAPIKey_Success verifies the happy path: a key is generated,
// the raw key starts with "cf_live_", and the record is stored in the fake.
func TestGenerateAPIKey_Success(t *testing.T) {
	store := &fakeAPIKeyStore{}
	tenantID := uuid.New()

	result, err := GenerateAPIKey(context.Background(), store, tenantID, "Test Key", "provision:write")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, strings.HasPrefix(result.RawKey, apiKeyPrefix),
		"raw key must start with %q", apiKeyPrefix)
	assert.Len(t, result.KeyHash, 64, "hash must be 64 hex chars")
	require.NotNil(t, store.stored, "Store must have been called")
	assert.Equal(t, result.KeyHash, store.stored.KeyHash)
	assert.Equal(t, tenantID, store.stored.TenantID)
	assert.Equal(t, accounts.APIKeyStatusActive, store.stored.Status)
	assert.Equal(t, "provision:write", store.stored.Scopes)
}

// TestGenerateAPIKey_StoreFails verifies that a storage error is wrapped and
// returned; no partial result is returned.
func TestGenerateAPIKey_StoreFails(t *testing.T) {
	store := &fakeAPIKeyStore{err: errors.New("scylla: write timeout")}

	_, err := GenerateAPIKey(context.Background(), store, uuid.New(), "Fail Key", "provision:write")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist api key")
}

// TestGenerateAPIKey_RawKeyIsNeverSameAcrossCalls verifies that two calls
// generate independent, non-equal keys.
func TestGenerateAPIKey_RawKeyIsNeverSameAcrossCalls(t *testing.T) {
	store1 := &fakeAPIKeyStore{}
	store2 := &fakeAPIKeyStore{}
	id := uuid.New()

	r1, err1 := GenerateAPIKey(context.Background(), store1, id, "Key 1", "provision:write")
	r2, err2 := GenerateAPIKey(context.Background(), store2, id, "Key 2", "provision:write")

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.NotEqual(t, r1.RawKey, r2.RawKey)
	assert.NotEqual(t, r1.KeyHash, r2.KeyHash)
}

// TestNewRawKeyAndHash_Format verifies that the generated key starts with
// the "cf_live_" prefix and is followed by exactly 64 hex characters
// (32 random bytes, hex-encoded).
func TestNewRawKeyAndHash_Format(t *testing.T) {
	raw, hash, err := newRawKeyAndHash()
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(raw, apiKeyPrefix),
		"raw key must start with %q, got %q", apiKeyPrefix, raw[:min(len(raw), 20)])

	// After the prefix the body is hex(32 bytes) = 64 chars.
	body := strings.TrimPrefix(raw, apiKeyPrefix)
	assert.Len(t, body, apiKeyRandomBytes*2, "key body should be %d hex chars", apiKeyRandomBytes*2)

	// Verify the body is valid lowercase hex.
	for _, ch := range body {
		assert.True(t, (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f'),
			"key body character %q is not lowercase hex", ch)
	}

	// Hash must be the BLAKE2b-256 hex output — 64 characters.
	assert.Len(t, hash, 64)
}

// TestNewRawKeyAndHash_Uniqueness verifies that two consecutive calls produce
// different keys (checks that crypto/rand is exercised, not a constant value).
func TestNewRawKeyAndHash_Uniqueness(t *testing.T) {
	raw1, hash1, err1 := newRawKeyAndHash()
	raw2, hash2, err2 := newRawKeyAndHash()

	require.NoError(t, err1)
	require.NoError(t, err2)

	assert.NotEqual(t, raw1, raw2, "two generated keys must be different")
	assert.NotEqual(t, hash1, hash2, "two generated hashes must be different")
}

// TestNewRawKeyAndHash_HashMatchesRawKey verifies that the returned hash is
// the BLAKE2b-256 digest of the returned raw key (consistency check between
// newRawKeyAndHash and HashAPIKey).
func TestNewRawKeyAndHash_HashMatchesRawKey(t *testing.T) {
	raw, hash, err := newRawKeyAndHash()
	require.NoError(t, err)

	expected, err := HashAPIKey(raw)
	require.NoError(t, err)

	assert.Equal(t, expected, hash, "hash returned by newRawKeyAndHash must equal HashAPIKey(rawKey)")
}

// min returns the smaller of two ints. Used for safe string truncation in
// error messages.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

package provisioner_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/provisioner"
)

// TestHashAPIKey_Deterministic verifies that hashing the same key always
// produces the same hex digest (BLAKE2b-256 is deterministic).
func TestHashAPIKey_Deterministic(t *testing.T) {
	key := "cf_live_aabbccddeeff001122334455667788990011223344556677889900112233445566"
	h1, err := provisioner.HashAPIKey(key)
	require.NoError(t, err)
	h2, err := provisioner.HashAPIKey(key)
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
}

// TestHashAPIKey_DifferentInputsDifferentHashes verifies that two different
// keys produce different hashes (collision resistance sanity check).
func TestHashAPIKey_DifferentInputsDifferentHashes(t *testing.T) {
	h1, err := provisioner.HashAPIKey("cf_live_" + strings.Repeat("a", 64))
	require.NoError(t, err)
	h2, err := provisioner.HashAPIKey("cf_live_" + strings.Repeat("b", 64))
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2)
}

// TestHashAPIKey_OutputIsHex verifies that the hash output is a valid lowercase
// hex string of the correct length (BLAKE2b-256 = 32 bytes = 64 hex chars).
func TestHashAPIKey_OutputIsHex(t *testing.T) {
	hash, err := provisioner.HashAPIKey("cf_live_testkey")
	require.NoError(t, err)

	assert.Len(t, hash, 64, "BLAKE2b-256 hex output should be 64 characters")
	for _, ch := range hash {
		assert.True(t, (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f'),
			"hash character %q is not lowercase hex", ch)
	}
}

// TestHashAPIKey_EmptyInputProducesHash verifies that an empty input does not
// panic and returns a non-empty hash.
func TestHashAPIKey_EmptyInputProducesHash(t *testing.T) {
	hash, err := provisioner.HashAPIKey("")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

// TestProvisionerUserID verifies that the sentinel user UUID used for
// provisioner-issued API keys is the expected constant.
func TestProvisionerUserID(t *testing.T) {
	assert.Equal(t, "00000000-0000-0000-0000-000000000001", provisioner.ProvisionerUserID.String())
}

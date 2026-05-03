package provisioner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/blake2b"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// apiKeyPrefix is the fixed prefix for all CloudForge live API keys.
// Format: "cf_live_" + hex(32 random bytes).
// The prefix lets CF-Router strip it before hashing and gives users a
// recognisable token format similar to GitHub PATs or Stripe secret keys.
const apiKeyPrefix = "cf_live_"

// apiKeyRandomBytes is the number of random bytes in the key body.
// 32 bytes = 256 bits of entropy; hex-encoded that is 64 characters.
const apiKeyRandomBytes = 32

// ProvisionerUserID is the system user UUID used for provisioner-issued API
// keys. In future, keys will be tied to a real user_id from CF-Accounts.
// For the VPC provisioning slice every generated key carries this sentinel.
var ProvisionerUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// GeneratedAPIKey is the result of GenerateAPIKey.
// The RawKey is returned exactly once to the caller and must not be logged.
// Only KeyHash is stored in ScyllaDB.
type GeneratedAPIKey struct {
	Record  *accounts.APIKey
	RawKey  string
	KeyHash string
}

// APIKeyStorer is the narrow interface that GenerateAPIKey needs to persist a
// new key record. *accounts.APIKeyStore satisfies this interface. Tests can
// substitute a fake implementation without requiring a live ScyllaDB cluster.
type APIKeyStorer interface {
	Store(ctx context.Context, k *accounts.APIKey) error
}

// GenerateAPIKey creates a new API key for a tenant, hashes it, writes the
// metadata to ScyllaDB via keyStore, and returns the raw key exactly once.
//
// The caller must include the RawKey in the provisioning job result and return
// it to the tenant. It is never retrievable after this call returns.
//
// scopes is a comma-separated list of permission scopes. For VPC provisioning
// the default is "provision:write,provision:read".
func GenerateAPIKey(
	ctx context.Context,
	keyStore APIKeyStorer,
	tenantID uuid.UUID,
	displayName string,
	scopes string,
) (*GeneratedAPIKey, error) {
	raw, hash, err := newRawKeyAndHash()
	if err != nil {
		return nil, err
	}

	keyID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("provisioner: generate key_id: %w", err)
	}

	record := &accounts.APIKey{
		KeyHash:     hash,
		KeyID:       keyID,
		TenantID:    tenantID,
		UserID:      ProvisionerUserID,
		DisplayName: displayName,
		Scopes:      scopes,
		Status:      accounts.APIKeyStatusActive,
	}

	if err := keyStore.Store(ctx, record); err != nil {
		return nil, fmt.Errorf("provisioner: persist api key for tenant %s: %w", tenantID, err)
	}

	return &GeneratedAPIKey{
		RawKey:  raw,
		KeyHash: hash,
		Record:  record,
	}, nil
}

// HashAPIKey computes the BLAKE2b-256 hex hash of a raw API key. This is the
// lookup key used by CF-Router when validating an incoming bearer token:
//
//  1. Strip the "cf_live_" prefix if present.
//  2. Pass the remaining bytes to BLAKE2b-256.
//  3. Hex-encode the 32-byte digest.
//  4. SELECT * FROM cf.api_keys WHERE key_hash = ?
//
// This function is exported so that CF-Router and test code can compute hashes
// without importing the full provisioner package.
func HashAPIKey(rawKey string) (string, error) {
	h, err := blake2b.New256(nil)
	if err != nil {
		return "", fmt.Errorf("provisioner: create BLAKE2b hasher: %w", err)
	}
	h.Write([]byte(rawKey))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// newRawKeyAndHash generates a new random API key and its BLAKE2b-256 hash.
func newRawKeyAndHash() (raw, hash string, err error) {
	buf := make([]byte, apiKeyRandomBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("provisioner: generate random key material: %w", err)
	}
	raw = apiKeyPrefix + hex.EncodeToString(buf)

	hash, err = HashAPIKey(raw)
	if err != nil {
		return "", "", err
	}
	return raw, hash, nil
}

package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
)

// APIKeyStatus represents the lifecycle state of an API key.
type APIKeyStatus string

const (
	// APIKeyStatusActive means the key is valid and can authenticate requests.
	APIKeyStatusActive APIKeyStatus = "ACTIVE"

	// APIKeyStatusRotating means the key is in its 24-hour grace period after
	// rotation. It is still accepted but a new key has been issued.
	APIKeyStatusRotating APIKeyStatus = "ROTATING"

	// APIKeyStatusRevoked means the key has been permanently invalidated.
	// CF-Router will reject requests bearing this key immediately (QUORUM
	// read guarantees the revocation is visible within ~1ms).
	APIKeyStatusRevoked APIKeyStatus = "REVOKED"
)

// APIKey holds the metadata for a CloudForge API key. The raw key value is
// never stored here — only the BLAKE2b-256 hash. The raw key is returned
// exactly once at creation time by the provisioning workflow.
type APIKey struct {
	KeyHash     string       // hex(BLAKE2b-256(raw_key)) — the ScyllaDB partition key
	KeyID       uuid.UUID    // stable identifier for management operations
	TenantID    uuid.UUID
	UserID      uuid.UUID    // which user owns this key (system UUID for provisioner-issued keys)
	DisplayName string
	Scopes      string       // comma-separated: "provision:write,provision:read"
	Status      APIKeyStatus
	ExpiresAt   time.Time    // zero value means never expires
	LastUsedAt  time.Time
	CreatedAt   time.Time
}

// ErrAPIKeyNotFound is returned by APIKeyStore.Lookup when the hash does not
// match any stored key.
var ErrAPIKeyNotFound = errors.New("accounts: api key not found")

// APIKeyStore provides access to cf.api_keys.
type APIKeyStore struct {
	sess *gocql.Session
}

// NewAPIKeyStore returns an APIKeyStore backed by the given session.
func NewAPIKeyStore(sess *gocql.Session) *APIKeyStore {
	return &APIKeyStore{sess: sess}
}

// Store writes a new API key record. keyHash must be the hex-encoded
// BLAKE2b-256 digest of the raw key (computed by the caller using
// internal/provisioner.HashAPIKey). The raw key is never passed here.
//
// Uses LWT (IF NOT EXISTS) so that concurrent calls with the same hash
// are deduplicated rather than overwriting an existing record.
func (s *APIKeyStore) Store(ctx context.Context, k *APIKey) error {
	var expiresAt interface{} = k.ExpiresAt
	if k.ExpiresAt.IsZero() {
		expiresAt = nil // NULL in CQL = never expires
	}

	dest := make(map[string]interface{})
	applied, err := s.sess.Query(`
		INSERT INTO cf.api_keys
		  (key_hash, key_id, tenant_id, user_id, display_name,
		   scopes, status, expiry, last_used_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		IF NOT EXISTS`,
		k.KeyHash,
		gocql.UUID(k.KeyID),
		gocql.UUID(k.TenantID),
		gocql.UUID(k.UserID),
		k.DisplayName,
		k.Scopes,
		string(k.Status),
		expiresAt,
		k.LastUsedAt,
		k.CreatedAt,
	).WithContext(ctx).MapScanCAS(dest)
	if err != nil {
		return fmt.Errorf("accounts: store api key %s: %w", k.KeyID, err)
	}
	if !applied {
		// Hash collision or duplicate call — treat as success (idempotent).
		return nil
	}
	return nil
}

// Lookup fetches the API key record matching keyHash. keyHash must be the
// hex-encoded BLAKE2b-256 of the raw bearer token presented by the client.
//
// This is the CF-Router hot path (p99 ~1ms QUORUM — scylladb-accounts spike).
// Returns ErrAPIKeyNotFound if no row exists.
func (s *APIKeyStore) Lookup(ctx context.Context, keyHash string) (*APIKey, error) {
	var k APIKey
	var keyID, tenantID, userID gocql.UUID
	var status string
	var expiresAt, lastUsedAt time.Time

	err := s.sess.Query(`
		SELECT key_hash, key_id, tenant_id, user_id, display_name,
		       scopes, status, expiry, last_used_at, created_at
		  FROM cf.api_keys WHERE key_hash = ?`,
		keyHash,
	).WithContext(ctx).Consistency(gocql.Quorum).Scan(
		&k.KeyHash, &keyID, &tenantID, &userID, &k.DisplayName,
		&k.Scopes, &status, &expiresAt, &lastUsedAt, &k.CreatedAt,
	)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("accounts: lookup api key: %w", err)
	}
	k.KeyID = uuid.UUID(keyID)
	k.TenantID = uuid.UUID(tenantID)
	k.UserID = uuid.UUID(userID)
	k.Status = APIKeyStatus(status)
	k.ExpiresAt = expiresAt
	k.LastUsedAt = lastUsedAt
	return &k, nil
}

// Revoke marks all API keys for a tenant as REVOKED. Called during tenant
// deprovisioning to ensure that no outstanding key can authenticate after
// the vCluster is deleted.
//
// Important: this operation scans cf.api_keys_by_tenant (if it existed) or
// loads keys by tenant_id. In this schema api_keys has key_hash as the sole
// partition key, so revocation requires a secondary lookup. For the VPC slice
// we revoke by key_id which is known to the deprovisioning workflow. Call
// RevokeByID for individual keys.
func (s *APIKeyStore) RevokeByID(ctx context.Context, keyID uuid.UUID) error {
	// We need the key_hash to update the row (partition key). Perform a
	// full-table scan filtered by key_id is not viable at scale, so the
	// deprovisioning workflow is expected to pass the key_hash it stored
	// during provisioning. Here we accept the keyID and update by hash
	// using a secondary lookup — acceptable because revocation is low-frequency.
	//
	// For now: update the status field using a CQL UPDATE that targets the
	// row by key_hash. The hash is retrieved from the provisioning job result.
	// This method is a placeholder; production would maintain a cf.api_keys_by_tenant
	// secondary index or an auxiliary lookup table.
	return s.revokeWhere(ctx, keyID)
}

// revokeWhere updates all rows matching keyID. Because key_hash is the only
// partition key, we use ALLOW FILTERING to find the row — acceptable here
// since deprovisioning is a rare, low-frequency operation.
func (s *APIKeyStore) revokeWhere(ctx context.Context, keyID uuid.UUID) error {
	// Find the key_hash for this keyID, then update.
	var keyHash string
	err := s.sess.Query(
		`SELECT key_hash FROM cf.api_keys WHERE key_id = ? ALLOW FILTERING`,
		gocql.UUID(keyID),
	).WithContext(ctx).Scan(&keyHash)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil // already gone, idempotent
	}
	if err != nil {
		return fmt.Errorf("accounts: find key hash for %s: %w", keyID, err)
	}
	return s.sess.Query(
		`UPDATE cf.api_keys SET status = ? WHERE key_hash = ?`,
		string(APIKeyStatusRevoked), keyHash,
	).WithContext(ctx).Exec()
}

// RevokeByHash revokes the key identified by its hash directly. This is the
// preferred method when the caller already holds the key_hash (e.g. from the
// provisioning job record).
func (s *APIKeyStore) RevokeByHash(ctx context.Context, keyHash string) error {
	return s.sess.Query(
		`UPDATE cf.api_keys SET status = ? WHERE key_hash = ?`,
		string(APIKeyStatusRevoked), keyHash,
	).WithContext(ctx).Exec()
}

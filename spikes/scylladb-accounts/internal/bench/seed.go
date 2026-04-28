package bench

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"golang.org/x/crypto/blake2b"
)

// ─── Seeding helpers ──────────────────────────────────────────────────────────
//
// These functions insert synthetic rows that the benchmarks read.  They are
// not part of the production code path; they exist only to populate ScyllaDB
// with realistic volumes so latency measurements reflect real-world conditions.

// SeedAPIKeys inserts n API key records into cf.api_keys.
// Each record has a unique BLAKE2b-256 hash of a randomly generated raw key.
// Returns the list of hashes so callers can use them as lookup keys during
// the benchmark without re-computing them.
func SeedAPIKeys(sess *gocql.Session, n int) ([]string, error) {
	hashes := make([]string, 0, n)
	for i := range n {
		rawKey := make([]byte, 32)
		if _, err := rand.Read(rawKey); err != nil {
			return nil, fmt.Errorf("seed key %d: rand: %w", i, err)
		}
		hash := hashAPIKey(rawKey)
		hashes = append(hashes, hash)

		err := sess.Query(`
			INSERT INTO cf.api_keys
			  (key_hash, key_id, tenant_id, user_id, display_name, scopes, status, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			hash,
			gocql.TimeUUID(),
			gocql.TimeUUID(),
			gocql.TimeUUID(),
			fmt.Sprintf("bench-key-%d", i),
			"provision:read,provision:write",
			"ACTIVE",
			time.Now(),
		).Exec()
		if err != nil {
			return nil, fmt.Errorf("seed api_key row %d: %w", i, err)
		}
	}
	return hashes, nil
}

// SeedTenants inserts n tenant records and returns the (tenant_id, slug) pairs
// so benchmarks can issue slug-based MV lookups.
func SeedTenants(sess *gocql.Session, n int) ([]TenantRow, error) {
	rows := make([]TenantRow, 0, n)
	for i := range n {
		id := gocql.TimeUUID()
		slug := fmt.Sprintf("tenant-%04d", i)

		err := sess.Query(`
			INSERT INTO cf.tenants
			  (tenant_id, slug, display_name, status, plan_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id,
			slug,
			fmt.Sprintf("Benchmark Tenant %d", i),
			"ACTIVE",
			"starter",
			time.Now(),
			time.Now(),
		).Exec()
		if err != nil {
			return nil, fmt.Errorf("seed tenant row %d: %w", i, err)
		}
		rows = append(rows, TenantRow{ID: id, Slug: slug})
	}
	return rows, nil
}

// SeedUsers inserts n user records spread evenly across the given tenant IDs
// and returns (email, tenant_id) pairs for MV lookups.
func SeedUsers(sess *gocql.Session, tenants []TenantRow, n int) ([]UserRow, error) {
	rows := make([]UserRow, 0, n)
	for i := range n {
		tenant := tenants[i%len(tenants)]
		email := fmt.Sprintf("user%d@%s.example.com", i, tenant.Slug)

		err := sess.Query(`
			INSERT INTO cf.users
			  (user_id, tenant_id, email, password_hash, role, mfa_enabled, status, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			gocql.TimeUUID(),
			tenant.ID,
			email,
			"$argon2id$v=19$m=65536,t=3,p=4$fakehashforspike",
			"MEMBER",
			false,
			"ACTIVE",
			time.Now(),
		).Exec()
		if err != nil {
			return nil, fmt.Errorf("seed user row %d: %w", i, err)
		}
		rows = append(rows, UserRow{Email: email, TenantID: tenant.ID})
	}
	return rows, nil
}

// TenantRow is a minimal tenant record returned by seeding.
type TenantRow struct {
	ID   gocql.UUID
	Slug string
}

// UserRow is a minimal user record returned by seeding.
type UserRow struct {
	Email    string
	TenantID gocql.UUID
}

// HashAPIKey computes the BLAKE2b-256 hash of rawKey and returns it as a
// lowercase hex string.  This mirrors the hash computed by CF-Router on every
// inbound API request before the ScyllaDB lookup.
func HashAPIKey(rawKey []byte) string { return hashAPIKey(rawKey) }

func hashAPIKey(rawKey []byte) string {
	// blake2b.New256 never returns an error for a nil key.
	h, _ := blake2b.New256(nil)
	h.Write(rawKey)
	return hex.EncodeToString(h.Sum(nil))
}

// RandomRawKey generates n cryptographically random bytes for use as a
// synthetic API key during seeding.
func RandomRawKey(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

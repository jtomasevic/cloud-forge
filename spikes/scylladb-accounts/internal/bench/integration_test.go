package bench

// Integration tests for the ScyllaDB-facing functions.
//
// These tests require a live ScyllaDB instance.  They are skipped automatically
// when:
//   - go test -short is passed (CI unit-test mode), OR
//   - SCYLLADB_HOST is unset AND port 9042 on 127.0.0.1 is unreachable.
//
// To run locally against the k3d dev cluster:
//
//	make test-integration           # from spikes/scylladb-accounts/
//
// The suite re-uses the "cf_spike" keyspace (not "cf") to avoid colliding with
// any production data on the same cluster.

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

const integrationKeyspace = "cf_spike"

// skipIfNoScyllaDB skips the test if ScyllaDB is not reachable.
// It checks SCYLLADB_HOST env (default 127.0.0.1) on port 9042 with a 1s dial
// timeout so the check is fast even in offline CI.
func skipIfNoScyllaDB(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test: -short")
	}
	host := os.Getenv("SCYLLADB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:9042", host), time.Second)
	if err != nil {
		t.Skipf("skipping integration test: ScyllaDB not reachable at %s:9042: %v", host, err)
	}
	conn.Close()
}

// integrationConfig returns a Config pointed at the integration keyspace.
func integrationConfig() Config {
	cfg := DefaultConfig()
	cfg.Keyspace = integrationKeyspace
	// Reduce workload for CI: 100 rows, 200 ops, 10 goroutines.
	cfg.SeedRows = 100
	cfg.Ops = 200
	cfg.Concurrency = 10
	cfg.LWTWriters = 5
	return cfg
}

// TestIntegration_ApplyAndDropSchema verifies ApplySchema creates all expected
// tables and DropSchema removes the keyspace.
func TestIntegration_ApplyAndDropSchema(t *testing.T) {
	skipIfNoScyllaDB(t)

	host := os.Getenv("SCYLLADB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	cfg := integrationConfig()
	cfg.Hosts = []string{host}

	sess, err := NewSession(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	// Read schema from source tree (test runs from the package directory).
	schemaCQL, err := os.ReadFile("../../schema/schema.cql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	// Replace the canonical "cf" keyspace name with the integration keyspace.
	content := strings.ReplaceAll(string(schemaCQL), " cf.", " "+integrationKeyspace+".")
	content = strings.ReplaceAll(content, " cf\n", " "+integrationKeyspace+"\n")
	content = strings.ReplaceAll(content, "'cf'", "'"+integrationKeyspace+"'")
	content = strings.ReplaceAll(content, "KEYSPACE cf", "KEYSPACE "+integrationKeyspace)

	// Apply.
	if err := ApplySchema(sess, content); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Verify expected tables exist via DESCRIBE (or system_schema query).
	tables := []string{"tenants", "users", "api_keys", "service_instances",
		"provisioning_jobs", "provisioning_jobs_by_idem"}
	for _, tbl := range tables {
		var name string
		q := fmt.Sprintf(
			"SELECT table_name FROM system_schema.tables WHERE keyspace_name='%s' AND table_name='%s'",
			integrationKeyspace, tbl)
		if err := sess.Query(q).Scan(&name); err != nil {
			t.Errorf("table %q not found after ApplySchema: %v", tbl, err)
		}
	}

	// Drop.
	if err := DropSchema(sess); err != nil {
		t.Fatalf("DropSchema: %v", err)
	}
}

// TestIntegration_APIKeyRoundtrip seeds one key and reads it back, verifying
// the primary-key lookup works end-to-end.
func TestIntegration_APIKeyRoundtrip(t *testing.T) {
	skipIfNoScyllaDB(t)

	host := os.Getenv("SCYLLADB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	cfg := integrationConfig()
	cfg.Hosts = []string{host}

	sess, err := NewSession(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() {
		// Clean up the spike keyspace.
		_ = DropSchema(sess)
		sess.Close()
	}()

	// Apply schema with integration keyspace substitution.
	schemaCQL, err := os.ReadFile("../../schema/schema.cql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	content := strings.ReplaceAll(string(schemaCQL), " cf.", " "+integrationKeyspace+".")
	content = strings.ReplaceAll(content, " cf\n", " "+integrationKeyspace+"\n")
	content = strings.ReplaceAll(content, "'cf'", "'"+integrationKeyspace+"'")
	content = strings.ReplaceAll(content, "KEYSPACE cf", "KEYSPACE "+integrationKeyspace)
	if err := ApplySchema(sess, content); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Generate a raw key and compute its hash.
	rawKey, err := RandomRawKey(32)
	if err != nil {
		t.Fatalf("RandomRawKey: %v", err)
	}
	hash := HashAPIKey(rawKey)

	// Insert using the integration keyspace table.
	err = sess.Query(fmt.Sprintf(`
		INSERT INTO %s.api_keys
		  (key_hash, key_id, tenant_id, user_id, display_name, scopes, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, integrationKeyspace),
		hash, generateUUID(), generateUUID(), generateUUID(),
		"integration-test-key", "provision:read", "ACTIVE", time.Now(),
	).Exec()
	if err != nil {
		t.Fatalf("insert api_key: %v", err)
	}

	// Look up the key by hash — this is the CF-Router hot path.
	var gotStatus string
	err = sess.Query(fmt.Sprintf(
		`SELECT status FROM %s.api_keys WHERE key_hash = ?`, integrationKeyspace),
		hash,
	).Scan(&gotStatus)
	if err != nil {
		t.Fatalf("lookup api_key: %v", err)
	}
	if gotStatus != "ACTIVE" {
		t.Errorf("status: want ACTIVE, got %q", gotStatus)
	}
}

// generateUUID is a minimal UUID generator for test use that avoids importing
// gocql in the test file (which would create a circular dependency concern).
// It delegates to gocql.TimeUUID via the package-level function.
func generateUUID() interface{} {
	rawKey, _ := RandomRawKey(16)
	return HashAPIKey(rawKey)[:32] // 32 hex chars used as a UUID-like string
}

package bench

import (
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

// NewSession dials ScyllaDB using the given Config and returns an open
// *gocql.Session.  The caller must call session.Close() when done.
//
// The session uses consistency QUORUM by default; individual queries can
// override it with gocql.Query.Consistency().
func NewSession(cfg Config) (*gocql.Session, error) {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Port = cfg.Port
	cluster.ConnectTimeout = cfg.ConnectTimeout
	cluster.Timeout = cfg.QueryTimeout
	cluster.Consistency = gocql.Quorum
	cluster.NumConns = 4 // connections per host; sufficient for benchmark loads
	// Fix protocol negotiation: pin v4 so gocql doesn't try to discover v5
	// (which ScyllaDB may not support in all deployments).
	cluster.ProtoVersion = 4

	// Pass credentials when the server requires authentication (ScyllaDB default).
	if cfg.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}

	sess, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("dial ScyllaDB %v:%d: %w", cfg.Hosts, cfg.Port, err)
	}
	return sess, nil
}

// ApplySchema executes all CQL statements in schemaContent against sess.
// It creates the keyspace and all tables / materialized views if they do not
// already exist.
//
// schemaContent is typically read from schema/schema.cql at startup by the
// CLI binary.  Individual statement errors are returned immediately.
func ApplySchema(sess *gocql.Session, schemaContent string) error {
	stmts := splitStatements(schemaContent)
	for i, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := sess.Query(stmt).Exec(); err != nil {
			return fmt.Errorf("statement %d: %w\n%s", i+1, err, stmt)
		}
	}
	return nil
}

// DropSchema removes the cf keyspace entirely.  Used by make destroy to clean
// up after a benchmark run so subsequent runs start from a known-empty state.
func DropSchema(sess *gocql.Session) error {
	return sess.Query("DROP KEYSPACE IF EXISTS cf").Exec()
}

// WaitForMVReady polls the tenants_by_slug MV with SELECT count(*) until it
// returns without an error, or until timeout is exceeded.  MVs in ScyllaDB
// are built asynchronously; the benchmark must not start before the MV is
// queryable.
func WaitForMVReady(sess *gocql.Session, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var n int
		err := sess.Query("SELECT count(*) FROM cf.tenants_by_slug").Scan(&n)
		if err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("cf.tenants_by_slug MV not ready after %s", timeout)
}

// splitStatements splits a CQL file on ";" delimiters, stripping SQL-style
// comments (lines starting with "--").  Empty statements are included in the
// slice so callers can detect them, but ApplySchema skips them.
func splitStatements(src string) []string {
	// Remove comment lines before splitting on ";"
	var cleaned strings.Builder
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	return strings.Split(cleaned.String(), ";")
}

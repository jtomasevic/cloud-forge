package accounts

import (
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

// Config holds the connection parameters for the ScyllaDB cluster that backs
// CF-Accounts. In dev mode the cluster runs as a single node in the cf-data
// namespace; production uses a 3-node cluster with RF=3 and QUORUM consistency.
type Config struct {
	Username       string
	Password       string
	Hosts          []string
	Port           int
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
}

// DefaultConfig returns a Config suitable for the local dev cluster
// (kubectl port-forward on 127.0.0.1:19042).
func DefaultConfig() Config {
	return Config{
		Hosts:          []string{"127.0.0.1"},
		Port:           19042,
		ConnectTimeout: 10 * time.Second,
		QueryTimeout:   5 * time.Second,
	}
}

// NewSession dials ScyllaDB with the given Config and returns a live
// *gocql.Session. The session uses QUORUM consistency by default for the
// routing hot path; individual queries may override this.
//
// The caller is responsible for calling session.Close() when done.
func NewSession(cfg *Config) (*gocql.Session, error) {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Port = cfg.Port
	cluster.ConnectTimeout = cfg.ConnectTimeout
	cluster.Timeout = cfg.QueryTimeout
	cluster.Consistency = gocql.Quorum
	cluster.NumConns = 4
	// Pin protocol v4 to avoid negotiation issues with some ScyllaDB deployments.
	cluster.ProtoVersion = 4

	if cfg.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}

	sess, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("accounts: dial ScyllaDB %v:%d: %w", cfg.Hosts, cfg.Port, err)
	}
	return sess, nil
}

// ApplySchema executes the CQL DDL in schemaContent against the session.
// It is called once at service startup to create the keyspace, tables, and
// materialized views if they do not already exist. Individual statement
// errors stop execution immediately and are returned with the failing
// statement text for debugging.
func ApplySchema(sess *gocql.Session, schemaContent string) error {
	for i, stmt := range splitStatements(schemaContent) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := sess.Query(stmt).Exec(); err != nil {
			return fmt.Errorf("accounts: schema statement %d: %w\n%s", i+1, err, stmt)
		}
	}
	return nil
}

// splitStatements splits a CQL source file on ";" delimiters, stripping
// comment lines that start with "--".
func splitStatements(src string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	return strings.Split(cleaned.String(), ";")
}

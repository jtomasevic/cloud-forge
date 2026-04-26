//go:build integration

package testutil

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// postgresImage is the Docker image used for integration tests.
// The pgvector extension is pre-installed in this image so vector similarity
// search can be tested without additional setup.
const postgresImage = "pgvector/pgvector:pg16"

// StartPostgres starts a PostgreSQL 16 container with the pgvector extension
// and returns a connected *pgxpool.Pool for use in integration tests.
//
// The container is started with:
//   - Database: "testdb"
//   - User:     "testuser"
//   - Password: "testpassword"
//
// These credentials are intentionally fixed for test isolation — they must
// never be used in production.
//
// The returned cleanup function stops and removes the container. It is also
// registered with t.Cleanup so the container is always removed when the test
// ends, even if the caller panics.
func StartPostgres(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	ctx := context.Background()

	// Start the PostgreSQL container using the testcontainers-go Postgres module.
	// The module handles health-checking, log waiting, and credential injection.
	container, err := tcpostgres.Run(ctx,
		postgresImage,
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpassword"),
	)
	if err != nil {
		t.Fatalf("testutil: starting postgres container: %v", err)
	}

	// Resolve the host:port that Docker has mapped for the container's 5432 port.
	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: getting postgres container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: getting postgres container port: %v", err)
	}

	// Build the pgxpool connection string.
	dsn := fmt.Sprintf(
		"postgres://testuser:testpassword@%s:%s/testdb?sslmode=disable",
		host, port.Port(),
	)

	// Open a connection pool. We use pgxpool rather than a single connection
	// so that tests exercising concurrent database access work correctly.
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: connecting to postgres: %v", err)
	}

	// Verify the connection is actually alive before returning.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: pinging postgres: %v", err)
	}

	cleanup := func() {
		// Close the pool first so all open connections are released before
		// the container is terminated. Terminating while connections are open
		// would produce confusing "connection reset by peer" errors in test logs.
		pool.Close()
		if err := container.Terminate(ctx); err != nil {
			t.Logf("testutil: warning: failed to terminate postgres container: %v", err)
		}
	}

	// Register cleanup with t.Cleanup as a safety net in case the caller
	// forgets to defer the returned cleanup function.
	t.Cleanup(cleanup)

	return pool, cleanup
}

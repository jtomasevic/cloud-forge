//go:build integration

package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// scyllaImage is the ScyllaDB image used for integration tests.
// Pinned to a known-good version that supports LWT and materialized views.
const scyllaImage = "scylladb/scylla:6.1"

// ScyllaSession holds a live *gocql.Session connected to the test container.
type ScyllaSession struct {
	Session *gocql.Session
}

// StartScyllaDB starts a ScyllaDB container and returns a connected
// *gocql.Session along with a cleanup function.
//
// The container uses developer mode (--developer-mode=1) to avoid the
// performance-optimised kernel parameters that are not available in Docker.
// Developer mode is only appropriate for tests — never for production.
//
// The cleanup function terminates the container and is also registered with
// t.Cleanup.
func StartScyllaDB(t *testing.T) (*gocql.Session, func()) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        scyllaImage,
		ExposedPorts: []string{"9042/tcp"},
		Cmd:          []string{"--developer-mode=1", "--smp=1"},
		// Wait until ScyllaDB logs that it is ready to accept CQL connections.
		// This message appears after the gossip protocol is up and the server
		// is accepting connections on port 9042.
		WaitingFor: wait.ForLog("init - Scylla version").
			WithOccurrence(1).
			WithStartupTimeout(90 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("testutil: start ScyllaDB container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: get ScyllaDB host: %v", err)
	}

	port, err := container.MappedPort(ctx, "9042")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: get ScyllaDB port: %v", err)
	}

	// Additional wait to ensure ScyllaDB is fully initialised.
	// The log message alone is not sufficient; the server needs a few more
	// seconds to set up the system keyspaces before accepting user queries.
	var sess *gocql.Session
	for attempt := 1; attempt <= 20; attempt++ {
		cluster := gocql.NewCluster(host)
		cluster.Port, _ = portToInt(port.Port())
		cluster.ConnectTimeout = 5 * time.Second
		cluster.Timeout = 5 * time.Second
		cluster.ProtoVersion = 4
		cluster.Consistency = gocql.Quorum

		sess, err = cluster.CreateSession()
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: connect to ScyllaDB after 20 attempts: %v", err)
	}

	cleanup := func() {
		sess.Close()
		if err := container.Terminate(ctx); err != nil {
			t.Logf("testutil: warning: terminate ScyllaDB container: %v", err)
		}
	}
	t.Cleanup(cleanup)

	return sess, cleanup
}

// StartScyllaDBForSuite starts a shared ScyllaDB container for use in TestMain.
// Unlike StartScyllaDB, it does not require a *testing.T; instead it returns
// an error that the caller must handle (typically with log.Fatal). The returned
// cleanup function terminates the container and must be called when the suite
// finishes (e.g. deferred in TestMain after m.Run()).
func StartScyllaDBForSuite(ctx context.Context) (*gocql.Session, func(), error) {
	req := testcontainers.ContainerRequest{
		Image:        scyllaImage,
		ExposedPorts: []string{"9042/tcp"},
		Cmd:          []string{"--developer-mode=1", "--smp=1"},
		WaitingFor: wait.ForLog("init - Scylla version").
			WithOccurrence(1).
			WithStartupTimeout(90 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("testutil: start ScyllaDB container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("testutil: get ScyllaDB host: %w", err)
	}

	port, err := container.MappedPort(ctx, "9042")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("testutil: get ScyllaDB port: %w", err)
	}

	var sess *gocql.Session
	for attempt := 1; attempt <= 20; attempt++ {
		cluster := gocql.NewCluster(host)
		cluster.Port, _ = portToInt(port.Port())
		cluster.ConnectTimeout = 5 * time.Second
		cluster.Timeout = 5 * time.Second
		cluster.ProtoVersion = 4
		cluster.Consistency = gocql.Quorum

		sess, err = cluster.CreateSession()
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("testutil: connect to ScyllaDB after 20 attempts: %w", err)
	}

	cleanup := func() {
		sess.Close()
		_ = container.Terminate(ctx)
	}
	return sess, cleanup, nil
}

// portToInt converts a port string (e.g. "9042") to an int.
func portToInt(port string) (int, error) {
	var n int
	_, err := fmt.Sscanf(port, "%d", &n)
	return n, err
}

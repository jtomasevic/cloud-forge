//go:build integration

package testutil

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
)

// natsImage is the NATS server image. JetStream is enabled via the command
// flag in the container module, so persistent streams and key-value stores
// are available for tests without any extra configuration.
const natsImage = "nats:2.10-alpine"

// StartNATS starts a NATS server container with JetStream enabled and
// returns a connected *nats.Conn for use in integration tests.
//
// The returned *nats.Conn is configured with:
//   - MaxReconnects: -1 (unlimited — avoids test flakiness during short outages)
//   - ReconnectWait: 100ms
//
// The cleanup function drains and closes the connection then terminates the
// container. It is also registered with t.Cleanup.
func StartNATS(t *testing.T) (*nats.Conn, func()) {
	t.Helper()

	ctx := context.Background()

	// Start the NATS container. The NATS module automatically passes the
	// -js flag to enable JetStream so callers can create streams without
	// any extra server configuration.
	container, err := tcnats.Run(ctx, natsImage)
	if err != nil {
		t.Fatalf("testutil: starting nats container: %v", err)
	}

	// Retrieve the NATS client URL in the form nats://host:port.
	url, err := container.ConnectionString(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: getting nats connection string: %v", err)
	}

	// Connect to NATS. Unlimited reconnect attempts prevent transient
	// test failures when the container is slow to accept connections.
	conn, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(100),
	)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: connecting to nats: %v", err)
	}

	cleanup := func() {
		// Drain ensures in-flight messages are processed before the connection
		// is closed. This prevents test flakiness caused by message loss.
		if err := conn.Drain(); err != nil {
			t.Logf("testutil: warning: nats drain error: %v", err)
		}
		if err := container.Terminate(ctx); err != nil {
			t.Logf("testutil: warning: failed to terminate nats container: %v", err)
		}
	}

	t.Cleanup(cleanup)

	return conn, cleanup
}

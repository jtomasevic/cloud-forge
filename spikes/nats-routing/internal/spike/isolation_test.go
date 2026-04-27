package spike_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

// TestRunIsolationTest_Pass verifies the "happy path": ncA must not receive
// messages published by ncB, and ncA can still receive its own messages.
//
// The test uses startMultiAccountServer which starts an in-process NATS server
// with TENANT_A and TENANT_B accounts, so no external NATS process is needed.
func TestRunIsolationTest_Pass(t *testing.T) {
	t.Parallel()

	srv := startMultiAccountServer(t)
	ncA := connectAs(t, srv, "user-a", "pass-a")
	ncB := connectAs(t, srv, "user-b", "pass-b")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := t.Context()

	pass, detail := spike.RunIsolationTest(ctx, ncA, ncB, "events.spy", logger)

	require.True(t, pass, "isolation test must pass: %s", detail)
	assert.Contains(t, detail, "isolation is complete")
}

// TestRunIsolationTest_SameAccountFails verifies (by inversion) that when
// both connections are in the SAME account the message IS delivered, and
// therefore the isolation test returns false — confirming that the test
// correctly detects non-isolation.
func TestRunIsolationTest_SameAccountFails(t *testing.T) {
	t.Parallel()

	srv := startMultiAccountServer(t)

	// Both connections authenticate as user-a (same account TENANT_A).
	ncA1 := connectAs(t, srv, "user-a", "pass-a")
	ncA2 := connectAs(t, srv, "user-a", "pass-a")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := t.Context()

	// When both connections are in the same account, the "spy" message from
	// ncA2 will arrive at ncA1's subscription — isolation should be reported
	// as BROKEN.
	pass, _ := spike.RunIsolationTest(ctx, ncA1, ncA2, "events.spy.invert", logger)

	// This is the inversion test: we expect pass=false here because there is
	// NO isolation between two connections in the same account.
	assert.False(t, pass, "same-account connections should NOT appear isolated")
}

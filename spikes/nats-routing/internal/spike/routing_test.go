package spike_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

// TestRunContentBasedRouting_DefaultRoutes verifies the full JetStream-backed
// routing loop with the default handler map.  Both "created" and "deleted"
// events must be dispatched to their respective handlers.
func TestRunContentBasedRouting_DefaultRoutes(t *testing.T) {
	t.Parallel()

	srv := startSingleServer(t)
	nc := connectAnon(t, srv)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := t.Context()

	pass, detail := spike.RunContentBasedRouting(ctx, nc, nil, logger)

	require.True(t, pass, "routing test must pass: %s", detail)
	assert.Contains(t, detail, "content-based routing works")
}

// TestRunContentBasedRouting_CustomRoutes verifies that a custom routes map
// overrides the default and that the provided handler is called for the
// matching event type.
func TestRunContentBasedRouting_CustomRoutes(t *testing.T) {
	t.Parallel()

	srv := startSingleServer(t)
	nc := connectAnon(t, srv)

	called := make(map[string]int)
	routes := map[string]spike.RouteHandler{
		"com.cloudforge.bucket.created": func(ev spike.CloudEvent, _ interface{ Info(string, ...any) }) {
			called[ev.Type]++
		},
		"com.cloudforge.bucket.deleted": func(ev spike.CloudEvent, _ interface{ Info(string, ...any) }) {
			called[ev.Type]++
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := t.Context()

	pass, _ := spike.RunContentBasedRouting(ctx, nc, routes, logger)
	require.True(t, pass)

	// Both event types must have been dispatched exactly once.
	assert.Equal(t, 1, called["com.cloudforge.bucket.created"])
	assert.Equal(t, 1, called["com.cloudforge.bucket.deleted"])
}

// TestRunContentBasedRouting_UnknownType verifies that a routes map that
// doesn't match any published event type causes the dispatch loop to receive
// DispatchUnknownType, which means msgs are Term'd and the overall test still
// completes (albeit without calling any handler).
//
// We confirm this indirectly: RunContentBasedRouting publishes "created" and
// "deleted" events, but if neither type is in the routes map the wg will still
// reach Done (via the Term branch), and the function returns true because no
// dispatch *error* occurs.
func TestRunContentBasedRouting_TermsUnknownType(t *testing.T) {
	t.Parallel()

	srv := startSingleServer(t)
	nc := connectAnon(t, srv)

	// Empty routes map — all events will be routed to the unknown-type branch.
	routes := map[string]spike.RouteHandler{}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	pass, _ := spike.RunContentBasedRouting(ctx, nc, routes, logger)
	// The loop completes (Term is not an error) so pass should be true.
	require.True(t, pass)
}

// TestBuildBenchmarkPayload_IsValidCloudEvent verifies that BuildBenchmarkPayload
// returns a valid CloudEvent that can be marshalled and matches the spec.
func TestBuildBenchmarkPayload_IsValidCloudEvent(t *testing.T) {
	t.Parallel()

	payload := spike.BuildBenchmarkPayload()
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	// Unmarshal back and verify type field.
	var ev spike.CloudEvent
	require.NoError(t, json.Unmarshal(data, &ev))
	assert.Equal(t, "com.cloudforge.bucket.created", ev.Type)
	assert.Equal(t, "1.0", ev.SpecVersion)
}

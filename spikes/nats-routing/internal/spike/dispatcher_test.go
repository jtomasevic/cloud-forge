package spike_test

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

// TestDispatch_KnownType verifies that a well-formed CloudEvent with a
// registered type is dispatched successfully.
func TestDispatch_KnownType(t *testing.T) {
	t.Parallel()

	called := false
	routes := map[string]spike.RouteHandler{
		"com.cloudforge.bucket.created": func(_ spike.CloudEvent, _ interface{ Info(string, ...any) }) {
			called = true
		},
	}

	ev := spike.BuildCloudEvent("com.cloudforge.bucket.created", "svc", `{"name":"b"}`)
	data, err := json.Marshal(ev)
	require.NoError(t, err)

	result, dispErr := spike.Dispatch(data, routes, testLogger)

	assert.Equal(t, spike.DispatchOK, result)
	assert.NoError(t, dispErr)
	assert.True(t, called, "handler must have been invoked")
}

// TestDispatch_UnknownType verifies that a CloudEvent whose type is not in
// the routes map returns DispatchUnknownType and a descriptive error.
func TestDispatch_UnknownType(t *testing.T) {
	t.Parallel()

	routes := map[string]spike.RouteHandler{
		"com.cloudforge.bucket.created": spike.HandleBucketCreated,
	}

	ev := spike.BuildCloudEvent("com.cloudforge.UNKNOWN", "svc", `{}`)
	data, err := json.Marshal(ev)
	require.NoError(t, err)

	result, dispErr := spike.Dispatch(data, routes, testLogger)

	assert.Equal(t, spike.DispatchUnknownType, result)
	assert.ErrorContains(t, dispErr, "com.cloudforge.UNKNOWN")
}

// TestDispatch_InvalidJSON verifies that malformed bytes return
// DispatchDecodeError and an error that mentions "unmarshal".
func TestDispatch_InvalidJSON(t *testing.T) {
	t.Parallel()

	result, dispErr := spike.Dispatch([]byte("not-json"), spike.NewDefaultRoutes(), testLogger)

	assert.Equal(t, spike.DispatchDecodeError, result)
	assert.ErrorContains(t, dispErr, "unmarshal")
}

// TestDispatch_EmptyRoutes verifies that an empty routes map returns
// DispatchUnknownType for any valid event.
func TestDispatch_EmptyRoutes(t *testing.T) {
	t.Parallel()

	ev := spike.BuildCloudEvent("any.type", "svc", `{}`)
	data, _ := json.Marshal(ev)

	result, _ := spike.Dispatch(data, map[string]spike.RouteHandler{}, testLogger)
	assert.Equal(t, spike.DispatchUnknownType, result)
}

// TestNewDefaultRoutes_ContainsExpectedTypes verifies that the default route
// map registers exactly the two bucket event types.
func TestNewDefaultRoutes_ContainsExpectedTypes(t *testing.T) {
	t.Parallel()

	routes := spike.NewDefaultRoutes()

	assert.Contains(t, routes, "com.cloudforge.bucket.created")
	assert.Contains(t, routes, "com.cloudforge.bucket.deleted")
	assert.Len(t, routes, 2)
}

// TestHandleBucketCreated_NosPanic verifies that HandleBucketCreated does not
// panic when called with a minimal CloudEvent.
func TestHandleBucketCreated_NoPanic(t *testing.T) {
	t.Parallel()

	ev := spike.BuildCloudEvent("com.cloudforge.bucket.created", "svc", `{"name":"b"}`)
	assert.NotPanics(t, func() {
		spike.HandleBucketCreated(ev, testLogger)
	})
}

// TestHandleBucketDeleted_NoPanic verifies that HandleBucketDeleted does not
// panic when called with a minimal CloudEvent.
func TestHandleBucketDeleted_NoPanic(t *testing.T) {
	t.Parallel()

	ev := spike.BuildCloudEvent("com.cloudforge.bucket.deleted", "svc", `{"name":"b"}`)
	assert.NotPanics(t, func() {
		spike.HandleBucketDeleted(ev, testLogger)
	})
}

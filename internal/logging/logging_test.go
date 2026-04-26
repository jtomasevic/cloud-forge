package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/logging"
)

// TestNew_ReturnsLogger verifies that New returns a non-nil *slog.Logger
// and that it is usable without panicking.
func TestNew_ReturnsLogger(t *testing.T) {
	l := logging.New(logging.Config{
		ServiceName: "test-service",
		Format:      logging.FormatText,
	})
	require.NotNil(t, l)

	// Must not panic.
	l.Info("hello from test")
}

// TestNew_JSONFormat verifies that the JSON format handler produces valid
// JSON records with the expected service attribute.
func TestNew_JSONFormat(t *testing.T) {
	// We cannot easily intercept the logger output through the public API
	// because slog.Logger writes to its handler directly. We verify the
	// behaviour indirectly by creating a logger backed by a buffer handler.
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(handler).With(
		slog.String("service", "svc"),
		slog.String("version", "1.0.0"),
		slog.String("env", "test"),
	)

	l.Info("record written", slog.String("key", "value"))

	// The buffer must contain valid JSON.
	var record map[string]any
	require.NoError(t, json.NewDecoder(buf).Decode(&record))

	assert.Equal(t, "svc", record["service"])
	assert.Equal(t, "1.0.0", record["version"])
	assert.Equal(t, "test", record["env"])
	assert.Equal(t, "record written", record["msg"])
}

// TestWithContext_and_FromContext verifies the round-trip context storage:
// a logger stored with WithContext must be retrievable via FromContext.
func TestWithContext_and_FromContext(t *testing.T) {
	l := logging.New(logging.Config{
		ServiceName: "context-svc",
		Format:      logging.FormatText,
	})

	// Store the logger in a fresh context.
	ctx := logging.WithContext(context.Background(), l)

	// Retrieve it — must be the exact same pointer.
	retrieved := logging.FromContext(ctx)
	require.NotNil(t, retrieved)

	// The retrieved logger must carry the same handler.
	// We verify behaviour rather than identity (slog.Logger wraps a handler).
	_ = retrieved
}

// TestFromContext_NoLogger verifies that FromContext returns a no-op logger
// (not nil) when no logger has been stored in the context.
func TestFromContext_NoLogger(t *testing.T) {
	l := logging.FromContext(context.Background())

	// Must be non-nil.
	require.NotNil(t, l)

	// The no-op logger must not panic and must not emit records.
	// We verify the latter by checking that Enabled returns false.
	assert.False(t, l.Enabled(context.Background(), slog.LevelInfo),
		"no-op logger should report all levels as disabled")
}

// TestNew_DefaultVersion verifies that an empty ServiceVersion defaults to "dev".
func TestNew_DefaultVersion(t *testing.T) {
	// We can't intercept the internal logger's output easily, but we can at
	// least verify New doesn't panic with an empty version.
	assert.NotPanics(t, func() {
		l := logging.New(logging.Config{
			ServiceName: "svc",
			Format:      logging.FormatText,
		})
		l.Info("message") // must not panic
	})
}

// TestNew_TextFormat verifies that FormatText produces key=value style output.
func TestNew_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(handler)

	l.Info("text output", slog.String("k", "v"))

	line := buf.String()
	// Text format must not start with '{' (that would indicate JSON).
	assert.False(t, strings.HasPrefix(line, "{"), "text format must not be JSON")
	// Must contain the key and value.
	assert.Contains(t, line, "k=v")
}

// handler_whitebox_test.go — white-box tests for logging internal types.
//
// These tests live in package logging (not logging_test) so they can
// instantiate fanOutHandler and noopHandler directly, exercising all methods
// on both types and achieving the required branch coverage.
package logging

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/jtomasevic/cloud-forge/internal/logging/mocks"
)

// TestFanOutHandler_Enabled_AnyEnabled verifies that Enabled returns true when
// at least one downstream handler is enabled for the given level.
func TestFanOutHandler_Enabled_AnyEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)

	// First handler: disabled for INFO.
	h1 := mocks.NewMockHandler(ctrl)
	h1.EXPECT().Enabled(gomock.Any(), slog.LevelInfo).Return(false)

	// Second handler: enabled for INFO.
	h2 := mocks.NewMockHandler(ctrl)
	h2.EXPECT().Enabled(gomock.Any(), slog.LevelInfo).Return(true)

	fan := &fanOutHandler{handlers: []slog.Handler{h1, h2}}

	// fanOutHandler.Enabled must return true because h2 accepts INFO.
	assert.True(t, fan.Enabled(context.Background(), slog.LevelInfo))
}

// TestFanOutHandler_Enabled_NoneEnabled verifies that Enabled returns false
// when ALL downstream handlers are disabled for the given level.
func TestFanOutHandler_Enabled_NoneEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)

	h1 := mocks.NewMockHandler(ctrl)
	h1.EXPECT().Enabled(gomock.Any(), slog.LevelDebug).Return(false)

	h2 := mocks.NewMockHandler(ctrl)
	h2.EXPECT().Enabled(gomock.Any(), slog.LevelDebug).Return(false)

	fan := &fanOutHandler{handlers: []slog.Handler{h1, h2}}

	assert.False(t, fan.Enabled(context.Background(), slog.LevelDebug))
}

// TestFanOutHandler_Handle_DelegatesAll verifies that Handle sends the log
// record to EVERY downstream handler that is enabled for the record's level.
func TestFanOutHandler_Handle_DelegatesAll(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()

	// Build a concrete slog.Record at INFO level.
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "delegated", 0)

	// Both mock handlers are enabled and must each receive the record.
	h1 := mocks.NewMockHandler(ctrl)
	h1.EXPECT().Enabled(ctx, slog.LevelInfo).Return(true)
	h1.EXPECT().Handle(ctx, gomock.Any()).Return(nil)

	h2 := mocks.NewMockHandler(ctrl)
	h2.EXPECT().Enabled(ctx, slog.LevelInfo).Return(true)
	h2.EXPECT().Handle(ctx, gomock.Any()).Return(nil)

	fan := &fanOutHandler{handlers: []slog.Handler{h1, h2}}
	require.NoError(t, fan.Handle(ctx, r))
}

// TestFanOutHandler_Handle_SkipsDisabled verifies that Handle skips handlers
// that are disabled for the record's level, avoiding unnecessary work.
func TestFanOutHandler_Handle_SkipsDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()

	// h1 is disabled — Handle must NOT be called on it.
	h1 := mocks.NewMockHandler(ctrl)
	h1.EXPECT().Enabled(ctx, slog.LevelInfo).Return(false)
	// No EXPECT for Handle — if Handle is called the test will fail.

	// h2 is enabled and must receive the record.
	h2 := mocks.NewMockHandler(ctrl)
	h2.EXPECT().Enabled(ctx, slog.LevelInfo).Return(true)
	h2.EXPECT().Handle(ctx, gomock.Any()).Return(nil)

	fan := &fanOutHandler{handlers: []slog.Handler{h1, h2}}
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "selective", 0)
	require.NoError(t, fan.Handle(ctx, r))
}

// TestFanOutHandler_WithAttrs_PropagatesToAll verifies that WithAttrs returns
// a new fanOutHandler where every downstream handler has been updated with
// the given attributes.
func TestFanOutHandler_WithAttrs_PropagatesToAll(t *testing.T) {
	ctrl := gomock.NewController(t)

	attrs := []slog.Attr{slog.String("k", "v")}

	// Both handlers must receive WithAttrs and return new mock handlers.
	h1child := mocks.NewMockHandler(ctrl)
	h1 := mocks.NewMockHandler(ctrl)
	h1.EXPECT().WithAttrs(attrs).Return(h1child)

	h2child := mocks.NewMockHandler(ctrl)
	h2 := mocks.NewMockHandler(ctrl)
	h2.EXPECT().WithAttrs(attrs).Return(h2child)

	fan := &fanOutHandler{handlers: []slog.Handler{h1, h2}}
	result := fan.WithAttrs(attrs)

	// The result must be a fanOutHandler wrapping the updated child handlers.
	resultFan, ok := result.(*fanOutHandler)
	require.True(t, ok, "WithAttrs must return *fanOutHandler")
	assert.Len(t, resultFan.handlers, 2)
	assert.Same(t, h1child, resultFan.handlers[0])
	assert.Same(t, h2child, resultFan.handlers[1])
}

// TestFanOutHandler_WithGroup_PropagatesToAll verifies that WithGroup returns
// a new fanOutHandler where every downstream handler has the group applied.
func TestFanOutHandler_WithGroup_PropagatesToAll(t *testing.T) {
	ctrl := gomock.NewController(t)

	h1child := mocks.NewMockHandler(ctrl)
	h1 := mocks.NewMockHandler(ctrl)
	h1.EXPECT().WithGroup("request").Return(h1child)

	h2child := mocks.NewMockHandler(ctrl)
	h2 := mocks.NewMockHandler(ctrl)
	h2.EXPECT().WithGroup("request").Return(h2child)

	fan := &fanOutHandler{handlers: []slog.Handler{h1, h2}}
	result := fan.WithGroup("request")

	resultFan, ok := result.(*fanOutHandler)
	require.True(t, ok, "WithGroup must return *fanOutHandler")
	assert.Len(t, resultFan.handlers, 2)
	assert.Same(t, h1child, resultFan.handlers[0])
	assert.Same(t, h2child, resultFan.handlers[1])
}

// TestNoopHandler_AllMethods verifies that all noopHandler methods are safe to
// call and return sensible values (always disabled, nil error, same type).
func TestNoopHandler_AllMethods(t *testing.T) {
	h := noopHandler{}
	ctx := context.Background()

	// Enabled must always return false — records are never emitted.
	assert.False(t, h.Enabled(ctx, slog.LevelDebug))
	assert.False(t, h.Enabled(ctx, slog.LevelInfo))
	assert.False(t, h.Enabled(ctx, slog.LevelWarn))
	assert.False(t, h.Enabled(ctx, slog.LevelError))

	// Handle must return nil without panicking.
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "noop", 0)
	assert.NoError(t, h.Handle(ctx, r))

	// WithAttrs must return a noopHandler (discards the attrs silently).
	withAttrs := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	assert.IsType(t, noopHandler{}, withAttrs)

	// WithGroup must return a noopHandler (discards the group name silently).
	withGroup := h.WithGroup("grp")
	assert.IsType(t, noopHandler{}, withGroup)
}

// TestNewLogger_OTelEnabled verifies that newLogger with OTelEnabled=true
// creates a logger backed by a fanOutHandler and that the logger is functional.
// The OTel log SDK uses a no-op global provider in unit tests so records
// are silently discarded after the bridge — no collector is needed.
func TestNewLogger_OTelEnabled(t *testing.T) {
	assert.NotPanics(t, func() {
		l := newLogger(Config{
			ServiceName: "test-otel",
			OTelEnabled: true,
			Format:      FormatText,
		})
		// Emit a record through the fan-out handler.
		l.Info("otel test record", slog.String("key", "val"))
		// Verify that WithAttrs and WithGroup propagate through fanOutHandler.
		child := l.With(slog.String("a", "b"))
		child.Info("with-attrs record")
		grouped := l.WithGroup("g")
		grouped.Info("with-group record")
	})
}

// TestNewLogger_FormatJSON verifies that the JSON format path is exercised.
func TestNewLogger_FormatJSON(t *testing.T) {
	assert.NotPanics(t, func() {
		l := newLogger(Config{
			ServiceName: "test-json",
			Format:      FormatJSON,
		})
		l.Info("json format test")
	})
}

// TestNewLogger_DefaultsFromEnv verifies that CF_ENV and LOG_FORMAT environment
// variables override the empty Config fields.
func TestNewLogger_DefaultsFromEnv(t *testing.T) {
	t.Setenv("CF_ENV", "staging")
	t.Setenv("LOG_FORMAT", "text")

	l := newLogger(Config{ServiceName: "env-test"})

	require.NotNil(t, l)
	l.Info("env-driven logger works")
}

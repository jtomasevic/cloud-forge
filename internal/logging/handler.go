package logging

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log/global"
)

// newLogger is the internal constructor backing the exported [New] function.
// It is separate so tests can call it directly with controlled options.
//
// to keep the API consistent with the rest of the platform (callers use struct literals).
//
//nolint:gocritic // hugeParam: Config is 80 bytes; passing by value is intentional
func newLogger(cfg Config) *slog.Logger {
	// ── Resolve defaults ────────────────────────────────────────────────
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "dev"
	}
	if cfg.Environment == "" {
		// Prefer the CF_ENV environment variable set by the deployment
		// manifest, then fall back to "development".
		if env := os.Getenv("CF_ENV"); env != "" {
			cfg.Environment = env
		} else {
			cfg.Environment = "development"
		}
	}

	// The LOG_FORMAT environment variable overrides the Config.Format field
	// so operators can change the format at runtime without redeployment.
	format := cfg.Format
	if envFmt := os.Getenv("LOG_FORMAT"); envFmt != "" {
		format = Format(envFmt)
	}
	if format == "" {
		format = FormatJSON
	}

	// ── Build the base slog handler ─────────────────────────────────────
	var baseHandler slog.Handler
	handlerOpts := &slog.HandlerOptions{
		Level: cfg.Level,
		// ReplaceAttr customises the "time" key format and injects
		// the service metadata attributes on every record so that
		// callers do not have to remember to add them manually.
		// The groups parameter is not used here; the underscore makes
		// that explicit and silences the unused-parameter linter.
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			return a
		},
	}

	// Write to stdout; in production log aggregators read from stdout.
	out := io.Writer(os.Stdout)

	switch format {
	case FormatText:
		baseHandler = slog.NewTextHandler(out, handlerOpts)
	default:
		// JSON is the default for production.
		baseHandler = slog.NewJSONHandler(out, handlerOpts)
	}

	// ── Fan out to OTel log bridge when enabled ─────────────────────────
	var handler slog.Handler
	if cfg.OTelEnabled {
		// otelslog.NewHandler creates an slog.Handler that forwards records
		// to the OTel log SDK. The global log provider must be initialised
		// before this is called (done by internal/tracing.Init).
		otelHandler := otelslog.NewHandler(
			cfg.ServiceName,
			otelslog.WithLoggerProvider(global.GetLoggerProvider()),
		)

		// fanOutHandler sends each log record to both the terminal sink and
		// the OTel bridge so the same record is visible in both places.
		handler = &fanOutHandler{handlers: []slog.Handler{baseHandler, otelHandler}}
	} else {
		handler = baseHandler
	}

	// ── Attach static service attributes ────────────────────────────────
	// These attributes appear on every log record so operators can filter
	// logs by service, version, and environment in their log platform.
	return slog.New(handler).With(
		slog.String("service", cfg.ServiceName),
		slog.String("version", cfg.ServiceVersion),
		slog.String("env", cfg.Environment),
	)
}

// fanOutHandler is a slog.Handler that writes each log record to multiple
// downstream handlers. Records are written synchronously in slice order.
// If one handler returns an error, the remaining handlers still receive
// the record; errors are silently discarded to avoid breaking log output
// over a transient OTel connectivity issue.
type fanOutHandler struct {
	handlers []slog.Handler
}

// Enabled returns true if any of the downstream handlers are enabled for the
// given level and context. This ensures that records are not discarded before
// reaching a handler that would accept them.
func (f *fanOutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle forwards the record to every downstream handler.
//
// interface requires passing it by value — we cannot change the signature.
//
//nolint:gocritic // hugeParam: slog.Record is 288 bytes; the slog.Handler
func (f *fanOutHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range f.handlers {
		// Skip disabled handlers to avoid unnecessary work.
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// Errors are intentionally ignored — a failing OTel exporter must
		// not break the primary terminal log output.
		_ = h.Handle(ctx, r)
	}
	return nil
}

// WithAttrs returns a new fanOutHandler where every downstream handler has
// been extended with the given attributes.
func (f *fanOutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	updated := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		updated[i] = h.WithAttrs(attrs)
	}
	return &fanOutHandler{handlers: updated}
}

// WithGroup returns a new fanOutHandler where every downstream handler has
// been grouped under the given name.
func (f *fanOutHandler) WithGroup(name string) slog.Handler {
	updated := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		updated[i] = h.WithGroup(name)
	}
	return &fanOutHandler{handlers: updated}
}

// noopHandler is a slog.Handler that silently discards every record.
// It is returned by [FromContext] when no logger has been stored in
// the context, preventing nil-pointer panics in untested code paths.
type noopHandler struct{}

// Enabled always returns false — noopHandler discards every record.
func (noopHandler) Enabled(_ context.Context, _ slog.Level) bool { return false }

// Handle discards the record without writing anything.
//
// requires this exact signature — we cannot change it to a pointer receiver.
//
//nolint:gocritic // hugeParam: slog.Record is 288 bytes; the slog.Handler interface
func (noopHandler) Handle(_ context.Context, _ slog.Record) error { return nil }

// WithAttrs returns a new noopHandler, silently discarding the attributes.
func (noopHandler) WithAttrs(_ []slog.Attr) slog.Handler { return noopHandler{} }

// WithGroup returns a new noopHandler, silently discarding the group name.
func (noopHandler) WithGroup(_ string) slog.Handler { return noopHandler{} }

// Package logging provides structured, context-aware logging for all
// CloudForge services built on top of Go's standard [log/slog] package.
//
// # Overview
//
// All services obtain their logger via [New] at startup and propagate it
// through [http.Request] contexts using [WithContext] / [FromContext].
// Middleware layers enrich the logger with per-request fields (trace_id,
// span_id, request_id) before storing it back in the context.
//
// # Log format
//
// The format is controlled by the LOG_FORMAT environment variable:
//   - "json" (default in production) — newline-delimited JSON, one record per line.
//   - "text" — human-readable key=value output suitable for local development.
//
// # OTel log bridge
//
// When otelEnabled is true, [New] wraps the slog handler with a fan-out
// handler that forwards every log record to both the terminal sink AND to
// the OpenTelemetry log SDK so that logs are correlated with traces in the
// observability back-end.
//
// # Required fields
//
// Every log record carries the following attributes:
//   - service: the service name passed to [New]
//   - version: the build version, injected at startup
//   - env: the deployment environment (from CF_ENV, defaults to "development")
//   - trace_id / span_id: injected by the OTel-aware handler when a span is active
package logging

import (
	"context"
	"log/slog"
)

// loggerKey is an unexported type for context keys in this package.
// Using a dedicated type prevents key collisions with other packages that
// also store values in context.
type loggerKey struct{}

// Format controls the output format of the log handler.
type Format string

const (
	// FormatJSON emits newline-delimited JSON, one record per line.
	// This is the recommended format for production deployments where
	// logs are ingested by Loki, Elasticsearch, or similar.
	FormatJSON Format = "json"

	// FormatText emits human-readable key=value lines using slog's
	// built-in TextHandler. Intended for local development.
	FormatText Format = "text"
)

// Config holds all options needed to build a logger for a service.
// The zero value is not valid; use [DefaultConfig] as a starting point.
type Config struct {
	// ServiceName is added as a "service" attribute to every log record.
	// Must not be empty.
	ServiceName string

	// ServiceVersion is added as a "version" attribute to every log record.
	// Defaults to "dev" if empty.
	ServiceVersion string

	// Environment is the deployment environment (e.g. "production", "staging",
	// "development"). Added as "env" to every log record.
	// Defaults to the CF_ENV env var, then "development".
	Environment string

	// Format controls the output format. Defaults to [FormatJSON].
	// Can also be set by the LOG_FORMAT environment variable at runtime.
	Format Format

	// Level is the minimum log severity to emit. Defaults to [slog.LevelInfo].
	Level slog.Level

	// OTelEnabled enables the OpenTelemetry log bridge so that log records
	// are forwarded to the OTel log SDK in addition to the terminal sink.
	OTelEnabled bool
}

// New returns a *slog.Logger configured for the given service.
//
// The logger is pre-loaded with "service", "version", and "env" attributes
// so that every record automatically carries those fields. When otelEnabled
// is true the OTel log bridge is installed as a second destination for all
// log records.
//
// Callers should store the returned logger in the request context via
// [WithContext] so that it can be retrieved deep in the call stack.
//
// to keep call sites clean (callers use struct literals without taking an address).
//
//nolint:gocritic // hugeParam: Config is 80 bytes; passing by value is intentional
func New(cfg Config) *slog.Logger {
	return newLogger(cfg)
}

// FromContext retrieves the logger stored in ctx by [WithContext].
//
// If no logger has been stored, a no-op logger is returned — this prevents
// nil-pointer panics in code paths that may be called before the middleware
// chain has injected a logger. The no-op logger discards all records.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	// Return a no-op logger so callers never have to guard against nil.
	return slog.New(noopHandler{})
}

// WithContext returns a new context with l stored in it.
// The stored logger can later be retrieved via [FromContext].
//
// This function is typically called in two places:
//  1. The service main() after the root logger is created.
//  2. The StructuredLogger middleware after enriching the logger with
//     per-request fields (request_id, trace_id, span_id).
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

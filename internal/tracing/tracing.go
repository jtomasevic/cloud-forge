// Package tracing initialises the OpenTelemetry (OTel) tracer provider for
// all CloudForge services.
//
// # Overview
//
// Every service calls [Init] once at startup, defers the returned shutdown
// function, and then uses the globally-registered tracer provider via the
// standard otel.Tracer helper.
//
// # Exporters
//
// In production (when Config.OTLPEndpoint is non-empty) traces are exported
// over gRPC to the OTLP endpoint (typically the OpenTelemetry Collector
// deployed in the cf-observability namespace).
//
// In development (empty OTLPEndpoint or OTEL_EXPORTER_OTLP_ENDPOINT not set)
// a pretty-printing stdout exporter is used instead, so developers can see
// trace output without running a collector.
//
// # Resource attributes
//
// Every span carries the following resource attributes:
//   - service.name
//   - service.version
//   - deployment.environment
//   - cf.component (set to Config.ServiceName)
//
// # Sampling
//
// The sampler is controlled by Config.Sampler:
//   - "always"      → sample every trace (default in development)
//   - "never"       → drop all traces (useful in load tests)
//   - "ratio:0.1"   → probabilistic sampling at 10%
//   - "parent"      → respect the parent span's sampling decision (default in production)
package tracing

import "context"

// Config holds all options needed to initialise the OTel tracer provider.
// The zero value is not valid; use [DefaultConfig] as a starting point.
type Config struct {
	// ServiceName is used as the OTel resource attribute "service.name".
	// Must not be empty.
	ServiceName string

	// ServiceVersion is used as the OTel resource attribute "service.version".
	// Defaults to "dev" if empty.
	ServiceVersion string

	// Environment is the deployment environment (e.g. "production", "staging").
	// Used as the OTel resource attribute "deployment.environment".
	// Defaults to the CF_ENV env var, then "development".
	Environment string

	// OTLPEndpoint is the address of the OTLP gRPC receiver.
	// For the in-cluster OTel Collector this is typically "otel-collector:4317".
	// When empty, a stdout exporter is used for local development.
	OTLPEndpoint string

	// Sampler controls which traces are recorded. Accepted values:
	//   "always"    — record every trace
	//   "never"     — drop all traces
	//   "ratio:N"   — record N fraction of traces (e.g. "ratio:0.1" = 10 %)
	//   "parent"    — follow the parent span's sampling decision
	// Defaults to "always" in development and "parent" when OTLPEndpoint is set.
	Sampler string
}

// ShutdownFunc is returned by [Init] and must be called when the service
// is shutting down to flush and close the exporter connection gracefully.
// It respects the given context deadline for draining in-flight spans.
type ShutdownFunc func(ctx context.Context) error

// Init initialises the global OTel tracer provider and log provider.
//
// The returned ShutdownFunc must be deferred in main() so that buffered
// spans are flushed before the process exits:
//
//	shutdown, err := tracing.Init(ctx, cfg)
//	if err != nil { log.Fatal(err) }
//	defer shutdown(ctx)
//
// After Init returns, services obtain a tracer with:
//
//	tracer := otel.Tracer("component-name")
//
// to keep call sites clean (callers use struct literals without taking an address).
//
//nolint:gocritic // hugeParam: Config is 80 bytes; passing by value is intentional
func Init(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	return initProvider(ctx, cfg)
}

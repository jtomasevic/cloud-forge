package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// initProvider is the internal implementation of [Init].
// It returns a combined shutdown function that closes both the trace and
// log providers when called.
//
// to keep call sites consistent with the rest of the platform.
//
//nolint:gocritic // hugeParam: Config is 80 bytes; passing by value is intentional
func initProvider(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	// ── Resolve defaults ────────────────────────────────────────────────
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "dev"
	}
	if cfg.Environment == "" {
		if env := os.Getenv("CF_ENV"); env != "" {
			cfg.Environment = env
		} else {
			cfg.Environment = "development"
		}
	}

	// Fall back to the standard OTEL_EXPORTER_OTLP_ENDPOINT env var when
	// Config.OTLPEndpoint is not set programmatically.
	if cfg.OTLPEndpoint == "" {
		cfg.OTLPEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}

	// ── Build the OTel resource ─────────────────────────────────────────
	// The resource describes the entity being observed — in CloudForge's
	// case, this is always a specific service instance.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
		// Automatically populate host.name, os.type, process.pid etc.
		resource.WithHost(),
		resource.WithProcess(),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: building resource: %w", err)
	}

	// ── Build the sampler ───────────────────────────────────────────────
	sampler, err := buildSampler(cfg.Sampler, cfg.OTLPEndpoint)
	if err != nil {
		return nil, fmt.Errorf("tracing: building sampler: %w", err)
	}

	// ── Set up the trace exporter ────────────────────────────────────────
	traceExporter, err := buildTraceExporter(ctx, cfg.OTLPEndpoint)
	if err != nil {
		return nil, fmt.Errorf("tracing: building trace exporter: %w", err)
	}

	// ── Create the TracerProvider ────────────────────────────────────────
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter,
			// Batch options tuned for low-latency CloudForge services.
			// The 5-second max export interval balances tail-latency with
			// the cost of frequent exporter round trips.
			sdktrace.WithMaxExportBatchSize(512),
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Register the provider globally so otel.Tracer() works anywhere.
	otel.SetTracerProvider(tp)

	// Install the W3C Trace Context + Baggage propagators so that trace
	// context is automatically forwarded in outgoing HTTP/gRPC requests.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// ── Set up the log provider (OTel log bridge) ────────────────────────
	var logShutdown func(context.Context) error

	if cfg.OTLPEndpoint != "" {
		// In production, export logs via OTLP gRPC alongside traces.
		logExporter, err := buildLogExporter(ctx, cfg.OTLPEndpoint)
		if err != nil {
			// Shut down the already-initialised trace provider before
			// returning to avoid a goroutine/connection leak.
			_ = tp.Shutdown(ctx)
			return nil, fmt.Errorf("tracing: building log exporter: %w", err)
		}

		lp := sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
			sdklog.WithResource(res),
		)
		global.SetLoggerProvider(lp)

		logShutdown = lp.Shutdown
	}
	// When OTLPEndpoint is empty the log provider is left as the no-op global
	// default; logs go only to the terminal via internal/logging.

	// ── Build combined shutdown function ─────────────────────────────────
	// The shutdown function must be idempotent: calling it multiple times
	// must not panic or return additional errors.
	shutdown := func(ctx context.Context) error {
		var errs []string

		// Always shut down the trace provider first so in-flight spans are
		// flushed before the log provider is closed.
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("trace provider: %v", err))
		}

		if logShutdown != nil {
			if err := logShutdown(ctx); err != nil {
				errs = append(errs, fmt.Sprintf("log provider: %v", err))
			}
		}

		if len(errs) > 0 {
			return fmt.Errorf("tracing shutdown errors: %s", strings.Join(errs, "; "))
		}
		return nil
	}

	return shutdown, nil
}

// buildTraceExporter returns an OTLP gRPC trace exporter when endpoint is
// non-empty, otherwise returns a pretty-printing stdout exporter suitable
// for local development.
func buildTraceExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	if endpoint == "" {
		// Stdout exporter: shows human-readable span JSON in the terminal.
		// This is intentionally verbose to help developers see what their
		// instrumentation is producing without running a collector.
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	}

	// Establish a gRPC connection to the OTLP endpoint.
	// InsecureCredentials are acceptable inside the cluster because the
	// traffic stays within the pod network. TLS termination happens at the
	// cluster ingress, not between CloudForge services and the collector.
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dialling OTLP gRPC endpoint %q: %w", endpoint, err)
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}
	return exporter, nil
}

// buildLogExporter returns an OTLP gRPC log exporter connected to endpoint.
func buildLogExporter(ctx context.Context, endpoint string) (sdklog.Exporter, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dialling OTLP gRPC endpoint for logs %q: %w", endpoint, err)
	}

	exporter, err := otlploggrpc.New(ctx, otlploggrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP log exporter: %w", err)
	}
	return exporter, nil
}

// buildSampler converts the human-friendly sampler string from [Config] to
// a concrete sdktrace.Sampler implementation.
//
// Supported values:
//   - "" or "always"   → AlwaysSample (default for development)
//   - "never"          → NeverSample
//   - "ratio:N"        → TraceIDRatioBased(N)
//   - "parent"         → ParentBased(AlwaysSample root) — honours parent decision
func buildSampler(s, endpoint string) (sdktrace.Sampler, error) {
	// If no sampler is specified, choose a sensible default based on whether
	// an OTLP endpoint is configured: "parent" in production, "always" in dev.
	if s == "" {
		if endpoint != "" {
			return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
		}
		return sdktrace.AlwaysSample(), nil
	}

	switch {
	case s == "always":
		return sdktrace.AlwaysSample(), nil

	case s == "never":
		return sdktrace.NeverSample(), nil

	case s == "parent":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil

	case strings.HasPrefix(s, "ratio:"):
		// Parse the ratio value after the colon, e.g. "ratio:0.1" → 0.1.
		ratioStr := strings.TrimPrefix(s, "ratio:")
		ratio, err := strconv.ParseFloat(ratioStr, 64)
		if err != nil || ratio < 0 || ratio > 1 {
			return nil, fmt.Errorf("invalid ratio sampler value %q (must be 0.0–1.0)", ratioStr)
		}
		return sdktrace.TraceIDRatioBased(ratio), nil

	default:
		return nil, fmt.Errorf("unknown sampler %q; accepted values: always, never, parent, ratio:N", s)
	}
}

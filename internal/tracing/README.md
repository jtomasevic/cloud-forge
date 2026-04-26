# `internal/tracing`

OpenTelemetry (OTel) tracer and log provider initialisation for all CloudForge services. Exports traces over OTLP gRPC to the OpenTelemetry Collector in production, or pretty-prints them to stdout for local development.

---

## File layout

| File | Contents |
|------|----------|
| `tracing.go` | `Config`, `ShutdownFunc` types; `Init` public function |
| `provider.go` | Tracer provider, log provider, exporter selection, sampler parsing |

---

## Quick start

```go
func main() {
    ctx := context.Background()

    // 1. Initialise tracing (and log bridge) at startup.
    shutdown, err := tracing.Init(ctx, tracing.Config{
        ServiceName:    "database-service",
        ServiceVersion: "1.2.0",
        Environment:    "production",
        OTLPEndpoint:   "otel-collector:4317",
        Sampler:        "parent",
    })
    if err != nil {
        log.Fatal("tracing init:", err)
    }
    defer shutdown(ctx) // flushes buffered spans before the process exits

    // 2. Anywhere in the codebase, get a tracer from the global registry:
    tracer := otel.Tracer("database-service/storage")

    ctx, span := tracer.Start(ctx, "CreateInstance")
    defer span.End()
}
```

---

## Config fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ServiceName` | `string` | required | OTel `service.name` resource attribute |
| `ServiceVersion` | `string` | `"dev"` | OTel `service.version` resource attribute |
| `Environment` | `string` | `CF_ENV` → `"development"` | OTel `deployment.environment` attribute |
| `OTLPEndpoint` | `string` | `OTEL_EXPORTER_OTLP_ENDPOINT` | gRPC endpoint of the OTel Collector, e.g. `"otel-collector:4317"` |
| `Sampler` | `string` | `"always"` (dev) / `"parent"` (prod) | See sampler values below |

---

## Sampler values

| Value | Behaviour | When to use |
|-------|-----------|-------------|
| `"always"` | Record every trace | Local development, low-traffic services |
| `"never"` | Drop all traces | Load tests, benchmark runs |
| `"parent"` | Follow parent span's decision | Production (default when `OTLPEndpoint` is set) |
| `"ratio:N"` | Record N fraction of traces (0.0 – 1.0) | High-volume services where `"always"` is too expensive |

```go
// Record 5% of traces in a high-throughput gateway:
Sampler: "ratio:0.05"
```

---

## Exporters

| Condition | Exporter | Notes |
|-----------|----------|-------|
| `OTLPEndpoint` is set | OTLP gRPC | Sends to OTel Collector; uses plain-text (insecure) within the cluster pod network |
| `OTLPEndpoint` is empty | stdout | Pretty-prints spans to terminal; useful for local development |

The `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable is respected as a fallback when `Config.OTLPEndpoint` is empty, following the OTel specification.

---

## Resource attributes on every span

These attributes are set once at provider creation and inherited by all spans:

| Attribute | Example |
|-----------|---------|
| `service.name` | `"database-service"` |
| `service.version` | `"1.2.0"` |
| `deployment.environment` | `"production"` |
| `host.name` | `"pod-xyz-123"` |
| `process.pid` | `12345` |

---

## Creating spans

After `Init`, use the global OTel API — no package-level reference to `internal/tracing` needed in service code:

```go
import "go.opentelemetry.io/otel"

tracer := otel.Tracer("component-name")

func (s *Store) Get(ctx context.Context, id string) (*Instance, error) {
    ctx, span := tracer.Start(ctx, "Store.Get",
        trace.WithAttributes(attribute.String("instance.id", id)),
    )
    defer span.End()

    // If this function returns an error, mark the span as failed:
    inst, err := s.db.QueryRow(ctx, query, id)
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        return nil, err
    }
    return inst, nil
}
```

---

## OTel log bridge

When `OTLPEndpoint` is non-empty, `Init` also creates an OTel log provider and registers it globally. This enables the `internal/logging` package's OTel bridge (`Config.OTelEnabled = true`) to forward `slog` records alongside traces, making log-trace correlation available in the observability back-end.

---

## Shutdown

The `ShutdownFunc` returned by `Init` must always be deferred in `main()`. It:
1. Flushes all buffered spans from the batch exporter.
2. Closes the gRPC connection to the OTel Collector.
3. Shuts down the log provider (when configured).

```go
shutdown, err := tracing.Init(ctx, cfg)
if err != nil { log.Fatal(err) }
defer func() {
    // Give in-flight spans up to 10 s to flush before the process exits.
    shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := shutdown(shutCtx); err != nil {
        log.Println("tracing shutdown error:", err)
    }
}()
```

---

## Local development

Leave `OTLPEndpoint` empty (or unset `OTEL_EXPORTER_OTLP_ENDPOINT`). Spans are printed to stdout in a human-readable JSON format — no Collector needed.

```bash
# Run the service locally — spans appear in the terminal:
CF_ENV=development ./database-service
```

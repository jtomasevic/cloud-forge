# `internal/logging`

Structured, context-aware logging for all CloudForge services. Built on Go's standard `log/slog` package with an optional OpenTelemetry log bridge that forwards log records to the OTel log SDK alongside traces.

---

## File layout

| File | Contents |
|------|----------|
| `logging.go` | `Config`, `Format` types; `New`, `WithContext`, `FromContext` public API |
| `handler.go` | `fanOutHandler` (terminal + OTel bridge), `noopHandler` (safe context fallback) |

---

## Quick start

### In `main()`

```go
logger := logging.New(logging.Config{
    ServiceName:    "database-service",
    ServiceVersion: "1.2.0",
    Environment:    "production",   // or read from CF_ENV
    Format:         logging.FormatJSON,
    Level:          slog.LevelInfo,
    OTelEnabled:    true,           // forward logs to OTel (requires tracing.Init first)
})

// Store in context so the middleware chain can enrich it per-request.
ctx := logging.WithContext(context.Background(), logger)
```

### In a handler (after middleware runs)

```go
func (h *Handler) CreateBucket(w http.ResponseWriter, r *http.Request) {
    // Retrieves the per-request logger enriched with request_id, trace_id, span_id.
    log := logging.FromContext(r.Context())

    log.Info("creating bucket", slog.String("name", bucketName))

    if err := h.store.Create(r.Context(), bucketName); err != nil {
        log.Error("failed to create bucket", slog.String("error", err.Error()))
        cferrors.WriteJSON(w, r, cferrors.Internal(err))
        return
    }

    log.Info("bucket created", slog.String("name", bucketName))
    w.WriteHeader(http.StatusCreated)
}
```

---

## Log formats

Controlled by the `Format` field in `Config` **or** the `LOG_FORMAT` environment variable at runtime (the env var takes precedence):

| Value | Output | Use case |
|-------|--------|----------|
| `"json"` (default) | `{"time":"...","level":"INFO","msg":"...","service":"..."}` | Production — ingest with Loki / Elasticsearch |
| `"text"` | `time=... level=INFO msg=... service=...` | Local development — human-readable terminal output |

```bash
# Switch to text format without recompiling:
LOG_FORMAT=text ./database-service
```

---

## Automatic fields

Every log record produced by a logger from `New()` automatically carries:

| Field | Example | Source |
|-------|---------|--------|
| `service` | `"database-service"` | `Config.ServiceName` |
| `version` | `"1.2.0"` | `Config.ServiceVersion` (default: `"dev"`) |
| `env` | `"production"` | `Config.Environment` → `CF_ENV` env var → `"development"` |

The `StructuredLogger` middleware enriches the per-request logger with additional fields:

| Field | Example | Source |
|-------|---------|--------|
| `request_id` | `"3f7a-..."` | `RequestID` middleware |
| `trace_id` | `"4bf9...2b3c"` | OTel span context |
| `span_id` | `"ab3f...12cd"` | OTel span context |

---

## Context helpers

```go
// Store a logger in context:
ctx = logging.WithContext(ctx, logger)

// Retrieve the logger from context (always safe — returns a no-op logger if none stored):
log := logging.FromContext(ctx)
log.Info("this is always safe, even before middleware runs")
```

`FromContext` never returns `nil`. When no logger has been stored it returns a **no-op logger** that discards all records, preventing nil-pointer panics in code paths that may run before the middleware injects a logger.

---

## OTel log bridge

When `Config.OTelEnabled = true`, a `fanOutHandler` is created that sends each log record to two destinations simultaneously:

1. The terminal sink (stdout, JSON or text).
2. The OTel log SDK via `go.opentelemetry.io/contrib/bridges/otelslog`.

This means log records are correlated with traces in your observability back-end (Grafana, Jaeger, etc.) automatically — no manual trace-ID injection needed.

**Prerequisite:** `tracing.Init` must be called before `logging.New` with `OTelEnabled: true`, because the bridge reads the globally-registered OTel log provider set by `tracing.Init`.

```go
// Correct startup order:
shutdown, _ := tracing.Init(ctx, tracingCfg)
defer shutdown(ctx)

logger := logging.New(logging.Config{OTelEnabled: true, ...})
```

---

## Adding structured fields

Use `slog.String`, `slog.Int`, `slog.Duration`, etc. for per-record fields:

```go
log.Info("replica lag measured",
    slog.String("replica", "replica-2"),
    slog.Duration("lag", 230*time.Millisecond),
    slog.Int("pending_writes", 142),
)
```

Use `logger.With(...)` to create a child logger with fields that persist across all subsequent records:

```go
// Create a logger scoped to a specific tenant/project.
tenantLog := logging.FromContext(ctx).With(
    slog.String("tenant", tenant),
    slog.String("project", project),
)
tenantLog.Info("operation started")   // carries tenant + project on every record
tenantLog.Info("operation completed")
```

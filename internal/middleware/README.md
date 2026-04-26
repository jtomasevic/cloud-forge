# `internal/middleware`

Standard HTTP middleware chain for all CloudForge services. Built on pure Go 1.22+ `net/http` — no third-party router required. Provides request correlation, distributed tracing, structured logging, panic recovery, Prometheus metrics, and tenant context extraction.

---

## File layout

| File | Contents |
|------|----------|
| `chain.go` | `Middlewares` type, `Apply`, `Chain`, `TenantFromContext` |
| `request_id.go` | `RequestID` middleware, `RequestIDFromContext` |
| `otel_span.go` | `OTelSpan` middleware (W3C Trace Context propagation) |
| `logger.go` | `StructuredLogger` middleware |
| `recovery.go` | `PanicRecovery` middleware |
| `tenant.go` | `TenantContext` middleware |
| `response_writer.go` | `responseWriter` — captures status code and byte count |

---

## Quick start

```go
mux := http.NewServeMux()

// Build the standard middleware chain.
chain := middleware.Chain(logger, registry, "database-service")

// Apply the chain to each route handler.
// Note: TenantContext (inside the chain) uses r.PathValue(), which is
// populated by the mux BEFORE calling the handler — so it works here.
const pattern = "GET /v1/tenants/{tenant}/projects/{project}/instances"
mux.Handle(pattern,
    metrics.WithRoutePattern(pattern,
        chain.Apply(http.HandlerFunc(listInstancesHandler))))

http.ListenAndServe(":8080", mux)
```

---

## Middleware stack (execution order)

```
Request →  RequestID  →  OTelSpan  →  StructuredLogger  →  PanicRecovery  →  HTTPMetrics  →  TenantContext  →  Handler
Response ←      ←              ←               ←                 ←               ←               ←
```

| # | Middleware | What it does |
|---|-----------|--------------|
| 1 | `RequestID` | Generates or forwards `X-Request-ID`; stores ID in context and response header |
| 2 | `OTelSpan` | Starts an OTel server span; propagates `traceparent` / `tracestate` from incoming headers |
| 3 | `StructuredLogger` | Enriches logger with `request_id`, `trace_id`, `span_id`; stores in context; logs request + response |
| 4 | `PanicRecovery` | Recovers panics; logs stack trace; returns `INTERNAL_ERROR` JSON |
| 5 | `HTTPMetrics` | Records duration, request count, and response size histograms in Prometheus |
| 6 | `TenantContext` | Reads `{tenant}` and `{project}` from path values; stores in context |

---

## `Middlewares` type and `Apply`

`Chain` returns a `Middlewares` value (`[]func(http.Handler) http.Handler`) with a single `Apply` method:

```go
chain := middleware.Chain(logger, registry, "my-svc")
handler := chain.Apply(http.HandlerFunc(myHandler))
mux.Handle("GET /path", handler)
```

You can also build a custom chain without the full default set:

```go
custom := middleware.Middlewares{
    middleware.RequestID,
    middleware.StructuredLogger(logger),
    middleware.PanicRecovery(logger),
}
mux.Handle("GET /admin/...", custom.Apply(http.HandlerFunc(adminHandler)))
```

---

## Individual middlewares

### `RequestID`

```go
mux.Handle("GET /", middleware.RequestID(handler))

// In the handler — read the ID from context:
id := middleware.RequestIDFromContext(r.Context()) // "3f7a1b2c-..."
```

Behaviour:
- If the incoming request has `X-Request-ID`, that value is used (client correlation).
- Otherwise a new UUID v4 is generated.
- The ID is echoed in the `X-Request-ID` response header.

### `OTelSpan`

```go
mux.Handle("GET /", middleware.OTelSpan()(handler))
```

- Uses the globally-registered OTel provider set by `tracing.Init`.
- Propagates W3C Trace Context (`traceparent`, `tracestate`) from incoming headers into the request context.
- Records `http.method`, `http.url`, `http.status_code` span attributes automatically.

### `StructuredLogger`

```go
mux.Handle("GET /", middleware.StructuredLogger(logger)(handler))
```

Logs two records per request (at `INFO` level):
- `"request started"` — on entry, with `method` and `path`.
- `"request completed"` — on exit, with `status`, `latency_ms`, and `bytes`.

Also stores the enriched per-request logger in context. Retrieve it in handlers:

```go
log := logging.FromContext(r.Context())
log.Info("doing something", slog.String("key", "value"))
```

### `PanicRecovery`

```go
mux.Handle("GET /", middleware.PanicRecovery(logger)(handler))
```

- Recovers any `panic` in a downstream handler.
- Logs the panic value and full goroutine stack trace at `ERROR` level.
- Writes a `500 INTERNAL_ERROR` JSON response — the panic message is **never** exposed to callers.

### `TenantContext`

```go
// Apply per-route so the mux has already populated path values:
mux.Handle("GET /v1/tenants/{tenant}/projects/{project}/items",
    middleware.TenantContext(handler))

// Read in the handler or any downstream middleware:
tenant, project, ok := middleware.TenantFromContext(r.Context())
if !ok {
    // Route does not have {tenant}/{project} parameters.
}
```

> **Important:** `TenantContext` must be applied as the `handler` argument to `mux.Handle()` — not as a wrapper around the entire mux. The Go 1.22 `http.ServeMux` populates `r.PathValue()` during dispatch, so path values are available to the handler chain but not to code that wraps the mux itself.

---

## Reading context values

| Value | How to read |
|-------|-------------|
| Request ID | `middleware.RequestIDFromContext(ctx)` |
| Logger (enriched) | `logging.FromContext(ctx)` |
| Tenant | `middleware.TenantFromContext(ctx)` → `(tenant, project, ok)` |
| OTel span | `trace.SpanFromContext(ctx)` |

---

## Go 1.22+ routing (no chi required)

The middleware package has no dependency on `github.com/go-chi/chi`. All routing uses the standard library:

```go
mux := http.NewServeMux()

// Method + path routing (Go 1.22+):
mux.HandleFunc("GET  /healthz",                          healthHandler)
mux.HandleFunc("POST /v1/tenants/{tenant}/instances",    createHandler)
mux.HandleFunc("GET  /v1/tenants/{tenant}/instances",    listHandler)
mux.HandleFunc("GET  /v1/tenants/{tenant}/instances/{id}", getHandler)

// Path value in handler:
func getHandler(w http.ResponseWriter, r *http.Request) {
    tenant := r.PathValue("tenant") // "acme"
    id     := r.PathValue("id")     // "inst-123"
    // ...
}
```

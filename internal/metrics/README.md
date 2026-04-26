# `internal/metrics`

Prometheus registry and standard HTTP metrics middleware for all CloudForge services. Provides out-of-the-box Go runtime metrics, per-request HTTP histograms, and a `/metrics` endpoint — all using an isolated per-service registry.

---

## File layout

| File | Contents |
|------|----------|
| `metrics.go` | `NewRegistry`, `HTTPMiddleware`, `Handler`, `HTTPMetrics` public API |
| `registry.go` | Registry creation, Go runtime + process collector registration, handler wiring |
| `http_middleware.go` | Per-request duration/count/size histograms, `WithRoutePattern`, `responseRecorder` |

---

## Quick start

```go
// 1. Create a registry for this service.
reg := metrics.NewRegistry("database_service")

// 2. Expose the /metrics endpoint.
mux.Handle("GET /metrics", metrics.Handler(reg))

// 3. Wrap route handlers with the HTTP metrics middleware.
mux.Handle("GET /v1/tenants/{tenant}/projects/{project}/instances",
    metrics.HTTPMiddleware(reg, "database_service")(
        http.HandlerFunc(listInstancesHandler),
    ),
)
```

In practice, `HTTPMiddleware` is applied automatically when you use `middleware.Chain` — you do not need to add it manually to every route.

---

## Registry

```go
reg := metrics.NewRegistry("my_service")
```

`NewRegistry` returns an **isolated** `*prometheus.Registry` (not the default global one). Each service gets its own registry, which:
- Prevents metric name collisions when multiple services share a test binary.
- Avoids `MustRegister` panics from duplicate registrations in tests.
- Pre-registers the following collectors automatically:

| Metric family | Description |
|---------------|-------------|
| `go_goroutines` | Number of goroutines |
| `go_gc_duration_seconds` | GC pause time distribution |
| `go_memstats_*` | Heap, stack, and GC memory statistics |
| `go_sched_*` | Go scheduler metrics (Go runtime metrics) |
| `{svcName}_process_cpu_seconds_total` | Total CPU time |
| `{svcName}_process_open_fds` | Open file descriptors |
| `{svcName}_process_virtual_memory_bytes` | Virtual memory size |

---

## HTTP metrics middleware

`HTTPMiddleware` records three histograms for every handled request:

| Metric | Labels | Description |
|--------|--------|-------------|
| `{svc}_http_server_request_duration_seconds` | method, path, status | Request latency |
| `{svc}_http_server_requests_total` | method, path, status | Request count |
| `{svc}_http_server_response_size_bytes` | method, path, status | Response body size |

### Label cardinality and `WithRoutePattern`

By default the `path` label uses `r.URL.Path` (e.g. `/v1/tenants/acme/projects/p1/instances/i-123`). This creates a new time series for every unique combination of tenant, project, and resource ID — which can explode your Prometheus cardinality.

Use `WithRoutePattern` to replace the raw path with the route pattern:

```go
const pattern = "GET /v1/tenants/{tenant}/projects/{project}/instances"
mux.Handle(pattern,
    metrics.WithRoutePattern(pattern,      // ← stores pattern in context
        chain.Apply(
            http.HandlerFunc(handler))),
)
// The "path" label is now "/v1/tenants/{tenant}/projects/{project}/instances"
// instead of "/v1/tenants/acme/projects/proj-1/instances".
```

---

## `/metrics` endpoint

```go
mux.Handle("GET /metrics", metrics.Handler(reg))
```

The handler serves Prometheus text format and also supports [OpenMetrics](https://github.com/OpenObservability/OpenMetrics) format via content negotiation (the `Accept` header).

---

## Registering custom metrics

Use the registry returned by `NewRegistry` directly with the standard `prometheus` package:

```go
reg := metrics.NewRegistry("my_svc")

// Custom counter:
instancesCreated := prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Namespace: "my_svc",
        Name:      "instances_created_total",
        Help:      "Total number of database instances created.",
    },
    []string{"tier"},
)
reg.MustRegister(instancesCreated)

// Increment in a handler:
instancesCreated.WithLabelValues("standard").Inc()
```

---

## Example Prometheus query (PromQL)

```promql
# p99 request latency for the database service over the last 5 minutes:
histogram_quantile(0.99,
  sum by (le, path) (
    rate(database_service_http_server_request_duration_seconds_bucket[5m])
  )
)

# Error rate (non-2xx) per endpoint:
sum by (path) (rate(database_service_http_server_requests_total{status!~"2.."}[5m]))
/
sum by (path) (rate(database_service_http_server_requests_total[5m]))
```

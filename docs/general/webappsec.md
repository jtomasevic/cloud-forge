I want all REST services to follow a strict 3-layer architecture:

1. REST API layer
2. Service layer
3. DB / external API layer

Core rule:
- each layer has its own models
- each layer has its own typed errors
- layers must never leak models or errors into each other
- all crossing between layers must happen through explicit transform methods

Additional implementation rule:
- everything must be built using only the Go standard library
- use the latest HTTP features available in Go 1.22+
- do not use third-party web frameworks, routers, middleware libraries, validation libraries, or error libraries

### OpenAPI contract (step 0 — must exist before any code is written)

Every REST service starts with an OpenAPI 3.1 specification. The spec is the
source of truth for the HTTP contract. Server stubs, client SDKs, and request
validation are all generated from it. No hand-written HTTP types should
duplicate what the spec already defines.

Location: `api/<service>/v1/openapi.yaml`

Codegen config files live alongside the spec:
- `oapi-server.cfg.yaml` — configuration for the server stub generator
- `oapi-client.cfg.yaml` — configuration for the client SDK generator

Run codegen before writing any handler code:
```
oapi-codegen -config api/<service>/v1/oapi-server.cfg.yaml api/<service>/v1/openapi.yaml
oapi-codegen -config api/<service>/v1/oapi-client.cfg.yaml api/<service>/v1/openapi.yaml
```

Output lands in `services/<service>/generated/` and must never be hand-edited.

Rule: if the HTTP contract changes, update `openapi.yaml` first, then regenerate.
Never change the generated files directly.

---

### REST API layer
Files:
- `handler.go` — implements generated `StrictServerInterface`
- `service.go` — `NewRouter()` and middleware chain
- `generated/` — codegen output (generated from `api/<service>/v1/openapi.yaml`)
- `models.go` — HTTP-only models not already covered by the generated types
- `models_transform.go` — mapping REST <-> service models
- `errors.go` — typed REST API errors and mapping from service errors

Rules:
- handlers must always use transform methods
- do not create service-layer params inline in handlers
- REST models must never go below REST layer
- REST error mapping must preserve original cause/stack

Transformation examples:
- `restModelInstance.ToService<ServiceName><ServiceModelName>() <ServiceModelType>`
- `To<RestModel>From<ServiceName><ServiceModelName>(model <ServiceModelType>) <RestModelType>`

### Service layer
Files:
- `api.go` — interface and constructor
- `models.go` — service models
- `models_transform.go` — mapping service <-> repository/client models
- `service.go` — implementation
- `errors.go` — typed service errors

Rules:
- service layer contains business logic only
- no REST models or REST errors allowed here
- transformations to lower layers must be explicit

### Naming rule: concrete service type
The concrete struct that implements the service interface must be named using
the pattern `CF<InterfaceName>`.

Example: if the interface is `ProvisionerService`, the concrete type is
`CFProvisionerService`.

Never use `impl`, `service`, `srv`, `handler`, or any other generic name for
the concrete struct. The name must be unique and meaningful so it is
identifiable in stack traces, logs, and test output.

The concrete type must never be exported directly from the package. It is always
returned as the interface type from the `New()` constructor:

```go
// Correct
func New(d Deps) ProvisionerService {
    return &CFProvisionerService{ ... }
}

// Wrong — exposes the concrete type
func New(d Deps) *CFProvisionerService {
    return &CFProvisionerService{ ... }
}
```

### DB / external API layer
Each repository/client should be isolated in its own subfolder.

Files:
- `api.go` — interface and constructor
- `models.go` — layer-specific models
- `models_transform.go` — mapping to/from DB or external SDK models when needed
- `client.go` or `repository.go` — implementation
- `errors.go` — typed errors

Rules:
- do not leak raw SDK/DB models upward
- define explicit models and conversions

### Error rule
Each layer must define all its errors in `errors.go`.
Most can be static typed errors.
If needed, use dynamic typed errors with runtime context.
Always preserve error cause/stack when wrapping and mapping.

### HTTP / routing rule
Because this must use only pure Go libraries:
- use `net/http`
- use Go 1.22+ method-aware routing and path patterns
- implement middleware using standard `http.Handler` composition
- do not introduce external routing or middleware packages

### Goal
Strict separation of transport, business logic, and integration concerns.
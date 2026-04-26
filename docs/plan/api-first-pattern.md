# CloudForge API-First Pattern

This document explains how to add a new REST API to CloudForge using the
API-first workflow. Read it before creating any new service endpoint.

## Principles

1. **Spec first, code second.** Write the OpenAPI 3.0 spec before any Go code.
   The spec is the contract; the code is an implementation detail.
2. **No external routers.** Generated server code uses the standard library
   `net/http` with Go 1.22+ pattern matching. Do not add `chi`, `gorilla/mux`,
   or any other routing dependency.
3. **Single source of truth.** The spec in `api/<service>/v1/openapi.yaml` is
   the only place where routes, request/response shapes, and error codes are
   defined. Do not duplicate schema definitions in Go structs.
4. **Generated code is not edited.** Files in `services/<service>/generated/`
   and `pkg/client/<service>/` are regenerated from the spec on every change.
   All edits go into the spec or the hand-written implementation files.

---

## Directory layout

```
api/
└── <service>/
    └── v1/
        ├── openapi.yaml          ← the spec (edit this)
        ├── oapi-server.cfg.yaml  ← codegen config for server stubs
        └── oapi-client.cfg.yaml  ← codegen config for client SDK

services/
└── <service>/
    ├── generated/
    │   └── server.gen.go         ← DO NOT EDIT — regenerated from spec
    ├── server.go                 ← wire-up: NewRouter() function
    └── handler.go                ← business logic implementation

pkg/
└── client/
    └── <service>/
        └── client.gen.go         ← DO NOT EDIT — regenerated from spec
```

---

## Step-by-step: adding a new service API

### 1 — Create the directory structure

```bash
mkdir -p api/<service>/v1
mkdir -p services/<service>/generated
mkdir -p pkg/client/<service>
```

### 2 — Write the OpenAPI spec

Create `api/<service>/v1/openapi.yaml`. Start from the Storage API spec as a
template (`api/storage/v1/openapi.yaml`). Key rules:

- Use **OpenAPI 3.0.3** (not 3.1 — `oapi-codegen` does not fully support 3.1 yet).
- Prefix all paths with `/<service>/v1/{tenant}/{project}/…` to match the
  platform's URL convention and allow `TenantContext` middleware to run.
- Use `$ref` to the shared `ErrorResponse` / `ErrorDetail` components for all
  non-2xx responses — do not define inline error schemas.
- Use `operationId` on every operation; it becomes the Go method name.

**Shared error schemas to reuse:**

```yaml
components:
  schemas:
    ErrorDetail:
      # … (copy from api/storage/v1/openapi.yaml)
    ErrorResponse:
      # … (copy from api/storage/v1/openapi.yaml)
  responses:
    BadRequest:
      $ref: "#/components/responses/BadRequest"
    # … etc.
```

### 3 — Create the codegen config files

**`api/<service>/v1/oapi-server.cfg.yaml`:**

```yaml
package: generated
generate:
  std-http-server: true   # ← pure net/http, no chi
  strict-server: true
  models: true
  embedded-spec: true
output: ../../../services/<service>/generated/server.gen.go
output-options:
  skip-prune: false
```

> **Important:** Always use `std-http-server: true`. Never use `chi-server: true`.

**`api/<service>/v1/oapi-client.cfg.yaml`:**

```yaml
package: <service>client
generate:
  client: true
  models: true
output: ../../../pkg/client/<service>/client.gen.go
```

### 4 — Generate the code

```bash
make gen-api SERVICE=<service>
```

This runs `oapi-codegen` twice (server stubs + client SDK) from inside the
`api/<service>/v1/` directory so the relative output paths resolve correctly.

Check the output:

```bash
# Should print nothing (no chi import)
grep -r "go-chi" services/<service>/generated/

# Should show the route patterns registered by oapi-codegen
grep "HandleFunc" services/<service>/generated/server.gen.go
```

### 5 — Implement the business logic

Create `services/<service>/handler.go` implementing the generated
`StrictServerInterface`. Every method receives a typed `*RequestObject` and
must return a typed `*ResponseObject`:

```go
package <service>

import (
    "context"
    "github.com/jtomasevic/cloud-forge/services/<service>/generated"
)

type Handler struct {
    // inject dependencies here: MinIO client, DB pool, logger, etc.
}

func NewHandler(/* deps */) *Handler { return &Handler{} }

// Each operation maps 1-to-1 to an OpenAPI operation via its operationId.
func (h *Handler) ListBuckets(
    ctx context.Context,
    req generated.ListBucketsRequestObject,
) (generated.ListBucketsResponseObject, error) {
    // req.Tenant, req.Project, req.Body are all typed
    // return a typed 200/4xx/5xx response object
    return generated.ListBuckets200JSONResponse{...}, nil
}
```

Return **typed response objects**, not raw `http.ResponseWriter` calls.
The strict handler adapter handles serialisation and status codes.

### 6 — Wire up the router

Create `services/<service>/server.go`:

```go
package <service>

import (
    "log/slog"
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/jtomasevic/cloud-forge/internal/middleware"
    "github.com/jtomasevic/cloud-forge/services/<service>/generated"
)

// NewRouter wires the StrictServerInterface to a plain http.ServeMux
// and wraps it with the shared CloudForge middleware chain.
func NewRouter(
    impl generated.StrictServerInterface,
    logger *slog.Logger,
    reg *prometheus.Registry,
    svcName string,
) http.Handler {
    mux := generated.HandlerWithOptions(
        generated.NewStrictHandler(impl, nil),
        generated.StdHTTPServerOptions{
            ErrorHandlerFunc: requestErrorHandler,
        },
    )
    chain := middleware.Chain(logger, reg, svcName)
    return chain.Apply(mux)
}

func requestErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
    cferrors.WriteJSON(w, r, cferrors.BadRequest(err.Error()))
}
```

### 7 — Use the generated client

Other CloudForge services and the `cf` CLI import the typed client:

```go
import storageclient "github.com/jtomasevic/cloud-forge/pkg/client/storage"

c, err := storageclient.NewClientWithResponses("http://storage-svc:8080")
resp, err := c.ListBucketsWithResponse(ctx, "acme", "my-project")
if resp.JSON200 != nil {
    for _, bucket := range resp.JSON200.Items {
        fmt.Println(bucket.Name)
    }
}
```

### 8 — Keep the spec and generated code in sync

After any spec change, regenerate immediately:

```bash
make gen-api SERVICE=<service>
go build ./...
```

CI will fail if the generated code is out of sync (the spec is embedded in the
binary and the build will catch type mismatches).

---

## Adding a new operation to an existing service

1. Add the operation to `api/<service>/v1/openapi.yaml`.
2. Run `make gen-api SERVICE=<service>`.
3. The `StrictServerInterface` in `server.gen.go` will gain a new method.
   The compiler will now fail on `handler.go` until you implement it.
4. Implement the method in `handler.go`.
5. Run `go build ./...` and `make lint` to verify.

---

## Path parameter convention

All CloudForge routes use the `{tenant}/{project}` path prefix. The
`TenantContext` middleware reads these values with `r.PathValue("tenant")` and
stores them in the request context. The generated strict handler also passes
them as typed fields on the `*RequestObject`:

```go
func (h *Handler) CreateBucket(ctx context.Context, req generated.CreateBucketRequestObject) ... {
    tenant  := req.Tenant   // == r.PathValue("tenant")
    project := req.Project  // == r.PathValue("project")
}
```

Never call `r.PathValue` directly inside handler methods — use the typed
fields on the request object instead.

---

## Error response convention

Always return one of the pre-defined `*JSONResponse` error types from the
generated package. They map 1-to-1 to the error codes in `internal/errors`:

| Situation | Return type |
|---|---|
| Resource not found | `generated.XxxNotFound404JSONResponse` |
| Invalid request body / params | `generated.XxxBadRequest400JSONResponse` |
| Not authenticated | `generated.XxxUnauthorized401JSONResponse` |
| IAM permission denied | `generated.XxxForbidden403JSONResponse` |
| Already exists | `generated.XxxConflict409JSONResponse` |
| Unexpected failure | `generated.XxxInternalError500JSONResponse` |

---

## FAQ

**Q: Can I use `chi` for the generated server code?**
No. The `oapi-codegen` config must always specify `std-http-server: true`.
See the project constraint in `api/storage/v1/oapi-server.cfg.yaml`.

**Q: The spec uses OpenAPI 3.1 — what do I need to change?**
Downgrade the `openapi:` field to `"3.0.3"`. Avoid 3.1-only features
(`type: [string, null]`, `const`, `$dynamicRef`). Everything used in
CloudForge specs is available in 3.0.

**Q: How do I expose `GET /openapi.json` at runtime?**
The spec is embedded in `server.gen.go` as `GetSwagger()`. Register it:
```go
mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
    sw, _ := generated.GetSwagger()
    json.NewEncoder(w).Encode(sw)
})
```

**Q: Where do I add service-specific middleware (e.g. JWT validation)?**
Pass it as the second argument to `generated.NewStrictHandler`:
```go
generated.NewStrictHandler(impl, []generated.StrictMiddlewareFunc{jwtMiddleware})
```

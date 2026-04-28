# Phase 0 — Foundation and Spikes
## Detailed Task Breakdown for AI Agent Execution

**Phase:** 0  
**Timeline:** Weeks 1–4  
**Status:** Ready to start  
**Dependencies:** None  
**Source plan:** `docs/2-cloud-forge-implementation-plan.v0.1.md`

---

## Phase Overview

Phase 0 is the prerequisite for everything. Nothing in phases 1–9 can be built without a working repository, a reproducible local development environment, shared Go libraries, a CI pipeline, and confirmed answers to the highest-risk technical integration questions.

The phase has two parallel tracks:

- **Infrastructure track (Tasks 0.1–0.5):** Sets up the monorepo, CI/CD, local cluster, shared libraries, and the API scaffolding toolchain. These are sequential within the track and must complete in order.
- **Spike track (Tasks 0.6–0.9):** Four time-boxed prototypes that validate the most uncertain integration points before any full-service implementation begins. Spikes run in parallel with each other and with the later infrastructure tasks (0.3 onwards).

**Execution sequence:**
```
0.1 → 0.2 → 0.3 → 0.4 (parallel with 0.5)
                 ↓
     Spikes 0.6, 0.7, 0.8, 0.9 (all parallel, start after 0.3 is running)
```

**What this phase unlocks:** All subsequent phases. Engineers can write, test, and deploy code. Spikes generate confirmed architecture decisions or early course corrections on NATS multi-tenancy, OPA policy evaluation, Knative cold start, and GPU/AI runtime deployment — before any full-service implementation is committed to.

---

## Task 0.1 — Monorepo and Go Module Setup

### Goal
Establish the canonical repository structure and Go workspace configuration. This is the first thing that must exist — all other tasks create files within this structure.

### Context
CloudForge is a monorepo with a single Go workspace (`go.work`). All custom CloudForge services, shared libraries, CLIs, controllers, and spike prototypes live here. A consistent layout from day one prevents drift and makes shared library changes immediately visible across all services.

### Inputs / Prerequisites
- Empty Git repository at `/Users/jtomasevic/my-repos/cloud-forge`
- Go 1.26+ installed
- `golangci-lint` installed

### Outputs / Deliverables

| Deliverable | Path | Description |
|---|---|---|
| Go workspace file | `go.work` | Declares all workspace modules |
| Root module | `go.mod` | Root module `github.com/cloud-forge/cloud-forge` |
| Directory skeleton | see below | All top-level directories created with `.gitkeep` |
| Linter config | `.golangci.yml` | Project-standard lint rules |
| Pre-commit hooks | `.pre-commit-config.yaml` | Runs lint, vet, test on staged Go files |
| `.gitignore` | `.gitignore` | Standard Go + IDE + secrets ignores |

**Full directory structure to create:**

```
cloud-forge/
├── go.work
├── go.mod
├── cmd/
│   ├── cf/                    # CloudForge CLI
│   ├── cf-install/            # Bootstrap CLI
│   ├── cf-iam/
│   ├── cf-secrets/
│   ├── cf-resource/
│   ├── cf-events/
│   ├── cf-functions/
│   ├── cf-db/
│   ├── cf-gateway/
│   ├── cf-observe/
│   └── cf-ai/
├── internal/
│   ├── config/
│   ├── errors/
│   ├── logging/
│   ├── metrics/
│   ├── middleware/
│   ├── tracing/
│   └── testutil/
├── pkg/
│   ├── apisix/
│   ├── authz/
│   ├── client/
│   ├── events/
│   ├── grafana/
│   ├── inference/
│   ├── keycloak/
│   ├── kserve/
│   ├── minio/
│   ├── openbao/
│   ├── opensearch/
│   └── resource/
├── services/
│   ├── ai/
│   ├── db/
│   ├── events/
│   ├── functions/
│   ├── gateway/
│   ├── iam/
│   ├── observe/
│   ├── resource/
│   ├── secrets/
│   └── storage/
├── controllers/
│   ├── ai/
│   ├── db/
│   ├── functions/
│   └── platform/
├── api/
│   ├── ai/v1/
│   ├── database/v1/
│   ├── events/v1/
│   ├── functions/v1/
│   ├── gateway/v1/
│   ├── iam/v1/
│   ├── observability/v1/
│   ├── resource/v1/
│   ├── secrets/v1/
│   └── storage/v1/
├── deploy/
│   ├── helm/
│   │   ├── cloudforge/
│   │   └── components/
│   ├── crds/
│   └── kustomize/
├── spikes/
│   ├── ai-runtime/
│   ├── knative-coldstart/
│   ├── nats-routing/
│   └── opa-embedded/
├── examples/
│   └── ai/
│       ├── rag/
│       ├── event-driven-inference/
│       └── fine-tuning-job/
├── tests/
│   └── e2e/
├── docs/
│   ├── plan/
│   └── ai/
└── Taskfile.yml
```

### Linter Configuration
Configure `golangci-lint` with at minimum:
- `errcheck` — all errors must be checked
- `govet` — standard vet checks
- `staticcheck` — SA family
- `gosec` — security checks
- `gofmt` / `goimports` — formatting
- `revive` — idiomatic Go
- `unused` — no dead code

### Acceptance Criteria
- [ ] `go work sync` runs without error
- [ ] `go build ./...` from repo root succeeds (even against empty packages with placeholder `package X` files)
- [ ] `golangci-lint run ./...` runs without configuration errors
- [ ] Pre-commit hook fires and runs `go vet ./...` on staged Go files
- [ ] Directory structure matches the layout above

### Implementation Steps for AI Agent
1. Initialize `go.mod` at repo root with module path `github.com/jtomasevic/cloud-forge` and Go version `1.26`
2. Create `go.work` referencing the root module
3. Create all directories listed above; populate each leaf directory with a placeholder `doc.go` file containing `package <dirname>` so the module is valid
4. Create `.golangci.yml` with the linters listed above
5. Create `.pre-commit-config.yaml` with hooks for `go vet`, `golangci-lint`, and `go test ./...`
6. Create a root `Taskfile.yml` stub with task groups: `dev:*`, `gen:*`, `test:*`, `deploy:*`
7. Update `.gitignore` to cover: `bin/`, `*.test`, `*.out`, `dist/`, `.env`, `*.pem`, `kubeconfig`

---

## Task 0.2 — CI/CD Pipeline

### Goal
Automated build, lint, test, and container image publish pipeline for all CloudForge services. Every push to a feature branch runs lint and tests. Every merge to `main` builds and publishes images.

### Context
This task depends on Task 0.1 (the repo structure must exist). It defines the CI workflow that every future service will rely on. Getting this right early means new services automatically get CI coverage just by existing in the correct directory.

### Inputs / Prerequisites
- Task 0.1 complete (repo structure and Go module exist)
- GitHub repository with GitHub Actions enabled
- Container registry: GitHub Container Registry (`ghcr.io/jtomasevic/cloud-forge/<service>`)

### Outputs / Deliverables

| Deliverable | Path | Description |
|---|---|---|
| PR lint+test workflow | `.github/workflows/ci.yml` | Triggered on every PR and push |
| Image build+push workflow | `.github/workflows/release.yml` | Triggered on merge to `main` and on tags |
| Service Dockerfile template | `deploy/docker/Dockerfile.service` | Multi-stage distroless image template |
| Ko config | `.ko.yaml` | `github.com/google/ko` configuration |
| Image tag strategy doc | `docs/plan/image-tagging.md` | semver + git SHA scheme |

**CI workflow stages (in order):**
1. `lint` — `golangci-lint run ./...`
2. `test:unit` — `go test -short ./...`
3. `test:integration` — `go test -tags=integration ./...` (with testcontainers)
4. `build` — `go build ./cmd/...`
5. `image:build` — build container images for all services in `cmd/`
6. `image:push` — push to registry (main branch + tags only)

**Dockerfile pattern (multi-stage distroless):**
```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum go.work* ./
RUN go mod download
COPY . .
ARG SERVICE_NAME
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/service ./cmd/${SERVICE_NAME}

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /bin/service /service
ENTRYPOINT ["/service"]
```

**Image tagging scheme:**
- Feature branch: `sha-<7-char-git-sha>`
- Main branch: `main-<date>-<sha>`
- Release tag: semver `v0.1.0`

### Acceptance Criteria
- [ ] A PR with a linter violation fails the CI `lint` job
- [ ] A PR with a failing unit test fails the CI `test:unit` job
- [ ] A successful merge to `main` produces a tagged image in the registry for each service in `cmd/`
- [ ] Each image is based on `gcr.io/distroless/static:nonroot`
- [ ] Images are labeled with: `org.opencontainers.image.version`, `org.opencontainers.image.revision`, `org.opencontainers.image.created`

### Go Tools
- `github.com/google/ko` — preferred for Go container image builds without handwritten Dockerfiles; use ko for services, keep the Dockerfile template for services with non-Go assets

### Implementation Steps for AI Agent
1. Create `.github/workflows/ci.yml`:
   - Trigger: `pull_request`, `push` to `main`
   - Jobs: `lint` → `test-unit` → `test-integration` → `build`
   - Use `actions/setup-go@v5` with Go 1.26 and module cache
   - Cache: `~/.cache/golangci-lint` and Go module cache
2. Create `.github/workflows/release.yml`:
   - Trigger: `push` to `main` and `push` to `tags: v*`
   - Jobs: build images via `ko` and push to `ghcr.io/jtomasevic/cloud-forge/`
3. Create `deploy/docker/Dockerfile.service` with the multi-stage pattern above
4. Create `.ko.yaml` with `defaultBaseImage: gcr.io/distroless/static:nonroot`
5. Add `image:build` and `image:push` Taskfile targets wrapping `ko build`

---

## Task 0.3 — Local Development Cluster

### Goal
Every engineer can run the full (or partial) CloudForge platform locally using `k3d`. A single command spins up a Kubernetes cluster, and a set of `Taskfile` commands manage the full dev lifecycle.

### Context
This task is the prerequisite for all four spikes (0.6–0.9). Without a working local cluster, spike prototypes cannot be deployed and tested. The cluster configuration defines the namespace structure that all subsequent manifests target.

### Inputs / Prerequisites
- Task 0.1 complete
- `k3d` installed locally (minimum v5.7)
- `kubectl` and `helm` installed
- Docker Desktop or equivalent container runtime running

### Outputs / Deliverables

| Deliverable | Path | Description |
|---|---|---|
| k3d cluster config | `deploy/k3d/cluster.yaml` | Reproducible local cluster definition |
| Namespace manifests | `deploy/kustomize/base/namespaces.yaml` | All platform namespaces |
| Taskfile dev commands | `Taskfile.yml` | `dev:up`, `dev:down`, `dev:reset`, `dev:status` |
| Bootstrap script | `scripts/dev-bootstrap.sh` | Self-signed certs, initial admin credentials |
| README | `docs/local-dev.md` | Step-by-step setup guide |

**k3d cluster specification:**
```yaml
# deploy/k3d/cluster.yaml
apiVersion: k3d.io/v1alpha5
kind: Simple
metadata:
  name: cloudforge-dev
servers: 1
agents: 2
ports:
  - port: "8080:80"
    nodeFilters: ["loadbalancer"]
  - port: "8443:443"
    nodeFilters: ["loadbalancer"]
options:
  k3s:
    extraArgs:
      - arg: "--disable=traefik"   # CloudForge uses Contour
        nodeFilters: ["server:*"]
```

**Required namespaces:**

| Namespace | Purpose |
|---|---|
| `cf-system` | Core platform services (IAM, secrets, resource controller) |
| `cf-identity` | Keycloak |
| `cf-data` | Databases, MinIO |
| `cf-events` | NATS JetStream |
| `cf-compute` | Knative, KServe, vLLM |
| `cf-observability` | OTel, Prometheus, Grafana, OpenSearch |
| `cf-gateway` | APISIX |
| `cf-tenant-*` | Dynamically created per tenant |

**Taskfile commands:**

```yaml
tasks:
  dev:up:
    desc: Start local k3d cluster and bootstrap base infrastructure
    cmds:
      - k3d cluster create --config deploy/k3d/cluster.yaml
      - kubectl apply -f deploy/kustomize/base/namespaces.yaml
      - bash scripts/dev-bootstrap.sh

  dev:down:
    desc: Stop and delete local cluster
    cmds:
      - k3d cluster delete cloudforge-dev

  dev:reset:
    desc: Destroy and recreate cluster from scratch
    cmds:
      - task: dev:down
      - task: dev:up

  dev:status:
    desc: Show cluster status and running components
    cmds:
      - k3d cluster list
      - kubectl get pods -A

  deploy:component:
    desc: Deploy a named CloudForge component
    vars:
      COMPONENT: '{{.CLI_ARGS}}'
    cmds:
      - helm upgrade --install {{.COMPONENT}} deploy/helm/components/{{.COMPONENT}} -n cf-system
```

**Bootstrap script responsibilities (`scripts/dev-bootstrap.sh`):**
1. Generate a self-signed CA certificate and leaf certs for `*.cloudforge.local`
2. Store certs as Kubernetes secrets in relevant namespaces
3. Generate random initial admin password and store as a Kubernetes secret in `cf-system`
4. Create a `kubeconfig` for the dev cluster at `.dev/kubeconfig`
5. Print a summary of what was created

**GPU note:** Local dev does not require a GPU. CPU-mode Ollama is used in the local cluster as a substitute for vLLM. The spike in Task 0.9 tests the GPU path separately against a cloud GPU node.

### Acceptance Criteria
- [ ] `task dev:up` creates a running k3d cluster with all namespaces present
- [ ] `task dev:down` cleanly removes the cluster
- [ ] `task dev:reset` produces a clean cluster identical to a fresh `dev:up`
- [ ] `task deploy:component cf-iam` can deploy a component into the cluster
- [ ] `scripts/dev-bootstrap.sh` is idempotent (safe to run more than once)
- [ ] All namespace RBAC, resource quotas, and network policies are applied as part of `dev:up`

### Implementation Steps for AI Agent
1. Create `deploy/k3d/cluster.yaml` with the spec above
2. Create `deploy/kustomize/base/namespaces.yaml` defining all namespaces with labels and annotations
3. Create `deploy/kustomize/base/rbac.yaml` with default service account annotations per namespace
4. Add `dev:up`, `dev:down`, `dev:reset`, `dev:status`, `deploy:component` to `Taskfile.yml`
5. Write `scripts/dev-bootstrap.sh` using `openssl` for cert generation and `kubectl create secret` for storage
6. Create `docs/local-dev.md` with prerequisites, setup steps, troubleshooting
7. Summarizy all important commands in Makefile. Create command which check if necessary tools are installed like k3d, kubectl etc, and install them if they are not. In Makefile call this command before starting cluster.

---

## Task 0.4 — Shared Internal Libraries

### Goal
Build the shared Go library packages that every CloudForge service will import. These must be written before any service is built, because without them each service would duplicate logging setup, OTel initialization, config loading, and middleware wiring.

### Context
These packages live in `internal/` and are not exported. They are shared across services within the monorepo. Getting them right here avoids a painful refactor when adding the fifth or tenth service.

### Inputs / Prerequisites
- Task 0.1 complete (repo and module structure exist)

### Outputs / Deliverables

Each package below must be created with full implementation and unit tests.

---

#### Package: `internal/logging`

**Purpose:** Structured logging for all services using `log/slog` with OpenTelemetry log bridge.

**API surface:**
```go
// New returns a *slog.Logger configured for the service.
// If otelEnabled is true, logs are also forwarded to the OTel log bridge.
func New(serviceName string, otelEnabled bool) *slog.Logger

// FromContext extracts the logger from context (injected by middleware).
func FromContext(ctx context.Context) *slog.Logger

// WithContext returns a new context with the logger stored in it.
func WithContext(ctx context.Context, l *slog.Logger) context.Context
```

**Required log fields on every record:** `service`, `version`, `trace_id`, `span_id`, `env`

**Format:** JSON in production, human-readable text in `dev` mode (controlled by `LOG_FORMAT` env var).

---

#### Package: `internal/tracing`

**Purpose:** OpenTelemetry tracer initialization for all services.

**API surface:**
```go
// Init initializes the global OTel tracer provider.
// Returns a shutdown function that must be deferred.
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error)

type Config struct {
    ServiceName    string
    ServiceVersion string
    OTLPEndpoint   string  // e.g. "otel-collector:4317"
    Sampler        string  // "always", "never", "ratio:0.1"
}
```

**Exporter:** OTLP gRPC. Fallback to stdout in dev mode when `OTEL_EXPORTER_OTLP_ENDPOINT` is not set.

**Resource attributes to set:** `service.name`, `service.version`, `deployment.environment`, `cf.component`.

---

#### Package: `internal/metrics`

**Purpose:** Prometheus registry and standard HTTP middleware metrics.

**API surface:**
```go
// NewRegistry returns a Prometheus registry pre-populated with Go runtime metrics.
func NewRegistry(serviceName string) *prometheus.Registry

// HTTPMiddleware returns an http.Handler middleware that records request duration,
// request count, and response size histograms labeled by method, path, and status.
func HTTPMiddleware(registry *prometheus.Registry, serviceName string) func(http.Handler) http.Handler

// Handler returns an http.Handler for the /metrics endpoint.
func Handler(registry *prometheus.Registry) http.Handler
```

---

#### Package: `internal/config`

**Purpose:** Viper-based configuration loading with YAML file + environment variable + Kubernetes secret file override chain.

**Loading order (later overrides earlier):**
1. Default values (defined in code)
2. YAML config file (path from `CF_CONFIG_FILE` env or default `./config.yaml`)
3. Environment variables (prefix `CF_`, e.g. `CF_SERVER_PORT` → `server.port`)
4. Kubernetes-mounted secret files (directory from `CF_SECRETS_DIR`)

**API surface:**
```go
// Load populates the given config struct from the source chain above.
// Panics if required fields are missing or validation fails.
func Load[T any](defaults T) (T, error)

// MustLoad is like Load but panics on error. Use in main().
func MustLoad[T any](defaults T) T
```

**Validation:** Use `github.com/go-playground/validator/v10` struct tags on config structs.

---

#### Package: `internal/errors`

**Purpose:** Platform error types with HTTP status mapping for consistent API error responses.

**API surface:**
```go
type Error struct {
    Code    string // e.g. "RESOURCE_NOT_FOUND"
    Message string
    Status  int    // HTTP status code
    Cause   error
}

func NotFound(resource, id string) *Error
func Unauthorized(reason string) *Error
func Forbidden(principal, action, resource string) *Error
func BadRequest(message string) *Error
func Internal(cause error) *Error
func Conflict(resource, id string) *Error

// WriteJSON writes a structured JSON error response to w.
func WriteJSON(w http.ResponseWriter, err *Error)

// IsNotFound returns true if the error is a not-found platform error.
func IsNotFound(err error) bool
```

**JSON error response format:**
```json
{
  "error": {
    "code": "RESOURCE_NOT_FOUND",
    "message": "database instance 'my-db' not found in project 'proj-1'",
    "request_id": "req-abc123"
  }
}
```

---

#### Package: `internal/middleware`

**Purpose:** HTTP middleware chain used by all CloudForge service HTTP servers.

**Middlewares to implement:**

| Middleware | Description |
|---|---|
| `RequestID` | Injects `X-Request-ID` into context and response header; generates UUID if not present in request |
| `StructuredLogger` | Logs each request/response: method, path, status, latency, request_id, trace_id |
| `OTelSpan` | Starts an OTel span for the request; propagates `traceparent` from incoming headers |
| `PanicRecovery` | Recovers from panics; returns 500 JSON error; logs the panic with stack trace |
| `TenantContext` | Extracts `{tenant}` and `{project}` path parameters and stores them in context (for chi routers) |

**API surface:**
```go
// Chain returns a [Middlewares] slice with all standard CloudForge middlewares
// pre-wired in the correct execution order.
//
// Prerequisites:
//   - [tracing.Init] must have been called before Chain is invoked so the
//     globally-registered OTel provider is in place for [OTelSpan].
//
// Parameters:
//   - logger:   structured logger from logging.New(...)
//   - registry: Prometheus registry from metrics.NewRegistry(...)
//   - svcName:  service name used to namespace Prometheus metric labels
func Chain(
	logger *slog.Logger,
	registry *prometheus.Registry,
	svcName string,
) Middlewares
// TenantFromContext extracts the tenant from context injected by TenantContext middleware.
func TenantFromContext(ctx context.Context) (tenant, project string, ok bool)
```

---

#### Package: `internal/testutil`

**Purpose:** Testcontainer helpers for integration tests across all services.

**Helpers to implement:**

| Function | Container | What it starts |
|---|---|---|
| `StartNATS(t)` | NATS with JetStream | Returns a `*nats.Conn` and cleanup func |
| `StartPostgres(t)` | PostgreSQL 16 + pgvector | Returns `*pgxpool.Pool` and cleanup func |
| `StartOpenBao(t)` | OpenBao (Vault-compatible) | Returns `*api.Client` and cleanup func |
| `StartMinIO(t)` | MinIO | Returns `*minio.Client` and cleanup func |
| `StartOPA(t)` | OPA (open-policy-agent) | Returns the OPA API base URL and cleanup func |

**Pattern all helpers follow:**
```go
func StartPostgres(t *testing.T) (*pgxpool.Pool, func()) {
    t.Helper()
    // use testcontainers-go to start container
    // register t.Cleanup for the returned func
    // return ready-to-use client
}
```

**Build tag:** All integration tests must be tagged `//go:build integration` so they are excluded from `go test -short ./...` in CI unit test jobs.

### Acceptance Criteria
- [ ] Each package has unit tests covering the primary happy path and at least one error case
- [ ] `go test ./internal/...` passes
- [ ] `internal/testutil` helpers start real containers and return usable clients
- [ ] `internal/middleware.Chain(...)` can be applied to a test HTTP server with all middlewares firing correctly
- [ ] `internal/config.Load[T]()` reads from env vars and YAML file; validation errors are returned correctly

### Go Libraries Required

```
go.opentelemetry.io/otel
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
go.opentelemetry.io/otel/sdk
go.opentelemetry.io/otel/bridge/opencensus
go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
github.com/prometheus/client_golang
github.com/spf13/viper
github.com/go-playground/validator/v10
github.com/google/uuid
github.com/testcontainers/testcontainers-go
github.com/jackc/pgx/v5
github.com/nats-io/nats.go
github.com/minio/minio-go/v7
github.com/openbao/openbao/api/v2
```

---

## Task 0.5 — OpenAPI-First API Scaffolding

### Goal
Establish the API-first development pattern. Every CloudForge REST API is defined in an OpenAPI 3.1
spec first, and server stubs + client SDKs are generated from it. The generated server code must use
the **standard library `net/http` router** (Go 1.22+ pattern matching) — no third-party routing
framework. Validate the entire toolchain end-to-end with one sample service before any real service
is built.

> **No external routing dependency.** CloudForge already implements all middleware in pure
> `net/http` (`internal/middleware`). Generated service code must follow the same convention:
> `http.NewServeMux()` with Go 1.22 `METHOD /path/{param}` patterns.

### Context
This task depends on Task 0.1 only. It defines the toolchain that all subsequent service tasks will
use. The pattern must be working and documented before Phase 1 begins.

### Inputs / Prerequisites
- Task 0.1 complete (monorepo, Go workspace, shared `internal/` libraries)
- Task 0.4 complete (shared middleware chain available at `internal/middleware`)
- `oapi-codegen` v2 installed:
  ```
  go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
  ```

### Outputs / Deliverables

| Deliverable | Path | Description |
|---|---|---|
| oapi-codegen config (server) | `api/<service>/v1/oapi-server.cfg.yaml` | Generates `net/http` strict server stubs |
| oapi-codegen config (client) | `api/<service>/v1/oapi-client.cfg.yaml` | Generates typed Go client SDK |
| Makefile gen target | `Makefile` | `make gen-api SERVICE=<name>` target |
| Taskfile gen target | `Taskfile.yml` | `gen:api SERVICE=<name>` target |
| Sample spec | `api/storage/v1/openapi.yaml` | Example CloudForge Storage API spec |
| Generated stubs | `services/storage/generated/` | Generated `StrictServerInterface` + types |
| Generated client | `pkg/client/storage/` | Generated typed Go client |
| Wire-up example | `services/storage/server.go` | Registers routes on `http.NewServeMux()` |
| Pattern document | `docs/plan/api-first-pattern.md` | How to add a new service API |

**oapi-codegen server config template:**
```yaml
# api/<service>/v1/oapi-server.cfg.yaml
package: generated
generate:
  # "std-http-server" emits a StrictServerInterface wired to net/http
  std-http-server: true
  strict-server: true
  models: true
  embedded-spec: true
output: ../../../services/<service>/generated/server.gen.go
output-options:
  skip-prune: false
```

> `std-http-server` was added in `oapi-codegen` v2.1. It generates an `http.Handler`-compatible
> adapter that registers routes on a plain `*http.ServeMux` using Go 1.22 method+path patterns
> (`GET /storage/v1/{tenant}/{project}/buckets`). 

**oapi-codegen client config template:**
```yaml
# api/<service>/v1/oapi-client.cfg.yaml
package: <service>client
generate:
  client: true
  models: true
output: ../../../pkg/client/<service>/client.gen.go
```

**Makefile gen target:**
```makefile
.PHONY: gen-api
gen-api: ## Generate server stubs and client SDK for a service (usage: make gen-api SERVICE=storage)
	oapi-codegen --config api/$(SERVICE)/v1/oapi-server.cfg.yaml api/$(SERVICE)/v1/openapi.yaml
	oapi-codegen --config api/$(SERVICE)/v1/oapi-client.cfg.yaml api/$(SERVICE)/v1/openapi.yaml

.PHONY: gen-all
gen-all: ## Regenerate all service stubs
	@for svc in storage iam secrets resource events functions ai gateway; do \
		$(MAKE) gen-api SERVICE=$$svc; \
	done
```

**Taskfile gen target:**
```yaml
gen:api:
  desc: Generate server stubs and client from OpenAPI spec for a service
  vars:
    SERVICE: '{{.CLI_ARGS}}'
  cmds:
    - oapi-codegen --config api/{{.SERVICE}}/v1/oapi-server.cfg.yaml api/{{.SERVICE}}/v1/openapi.yaml
    - oapi-codegen --config api/{{.SERVICE}}/v1/oapi-client.cfg.yaml api/{{.SERVICE}}/v1/openapi.yaml
```

**Validation spec — `api/storage/v1/openapi.yaml`:**
Implement a minimal but valid Storage API spec with at least:
- `POST /storage/v1/{tenant}/{project}/buckets`
- `GET /storage/v1/{tenant}/{project}/buckets`
- `DELETE /storage/v1/{tenant}/{project}/buckets/{name}`
- Standard error response schemas (reuse the `Error` shape from `internal/errors`)
- Request/response schemas with proper validation constraints

**Wire-up example — `services/storage/server.go`:**

The generated `StrictServerInterface` adapter must be registered on a plain `http.ServeMux`, then
wrapped with the shared middleware chain from `internal/middleware`:

```go
// services/storage/server.go
package storage

import (
    "net/http"

    "github.com/jtomasevic/cloud-forge/internal/middleware"
    "github.com/jtomasevic/cloud-forge/internal/metrics"
    "github.com/jtomasevic/cloud-forge/services/storage/generated"
)

// NewRouter builds the storage service HTTP router.
// Routes are registered on a stdlib mux using Go 1.22 pattern matching;
// no third-party router is used.
func NewRouter(impl generated.StrictServerInterface, mw middleware.Middlewares, reg *prometheus.Registry) http.Handler {
    mux := http.NewServeMux()

    // generated.HandlerWithOptions wires the StrictServerInterface to the mux.
    // The "std-http-server" codegen target produces this helper.
    generated.HandlerWithOptions(
        generated.NewStrictHandler(impl, nil),
        generated.StdHTTPServerOptions{RequestErrorHandlerFunc: errorHandler},
    )

    // Register routes on the mux — oapi-codegen registers them for us via HandlerWithOptions.
    // Apply the shared middleware chain on top.
    return mw.Apply(mux)
}

// errorHandler translates oapi-codegen request validation errors into
// the platform's standard JSON error shape.
func errorHandler(w http.ResponseWriter, r *http.Request, err error) {
    cferrors.WriteJSON(w, r, cferrors.BadRequest(err.Error()))
}
```

- Implement a placeholder `StorageHandlerImpl` that returns `501 Not Implemented` for every method
- Show that the server compiles and paths like `/storage/v1/acme/my-project/buckets` route correctly

### Acceptance Criteria
- [ ] `make gen-api SERVICE=storage` generates valid, compilable Go code with pure go, no third parties
- [ ] The generated `server.gen.go` imports only `net/http`
- [ ] Routes are registered using `http.NewServeMux()` with Go 1.22 `METHOD /path/{param}` patterns
- [ ] Path values are read with `r.PathValue("tenant")`, consistent with `internal/middleware/tenant.go`
- [ ] The middleware chain from `internal/middleware` wraps the mux (request-ID, logger, metrics, panic recovery)
- [ ] The generated client in `pkg/client/storage/` compiles and has typed methods matching the spec
- [ ] Adding a new endpoint to the spec and re-running `make gen-api SERVICE=storage` updates the generated code correctly
- [ ] `docs/plan/api-first-pattern.md` documents the full workflow for a new engineer, explicitly noting the `std-http-server` config option.

---

## Spike 0.6 — NATS JetStream Multi-Tenant Routing

### Context and Motivation
CF-EventRouter (Phase 5) requires NATS JetStream to provide:
1. Hard tenant isolation (Tenant A cannot read Tenant B's messages)
2. Per-tenant stream provisioning at runtime (when a tenant is onboarded)
3. Content-based routing (routing rules that inspect CloudEvents fields)
4. High throughput with low latency per-tenant streams

The NATS accounts model is the candidate mechanism for tenant isolation, but dynamic account provisioning in NATS has known operational complexity. This spike must answer whether it works reliably before Phase 5 commits to it.

### Questions This Spike Must Answer

| # | Question | Acceptable Answer |
|---|---|---|
| Q1 | Can NATS accounts be provisioned dynamically at runtime without restarting the NATS cluster? | Yes, via NATS config reload or NSC/NKS API |
| Q2 | Is stream/subject isolation between NATS accounts complete (Tenant A cannot subscribe to Tenant B's subjects even with explicit subject names)? | Yes — cross-account access is impossible without explicit import/export configuration |
| Q3 | What is the per-message latency for CloudEvents payloads (1KB) published to a JetStream stream? | < 5ms p99 on local cluster |
| Q4 | How is content-based routing implemented — within NATS subjects, or via a consumer-side filter? | Document the chosen approach with trade-offs |
| Q5 | Can 50 tenant accounts be provisioned sequentially in under 2 minutes total? | Yes |

### Spike Scope
Time-box: **3 days maximum.**

**What to build in `spikes/nats-routing/`:**
1. A `docker-compose.yaml` or Kubernetes manifest that starts a 3-node NATS JetStream cluster
2. A Go program (`cmd/main.go`) that:
   - Provisions 2 NATS accounts (`tenant-a`, `tenant-b`) dynamically
   - Creates a JetStream stream per account
   - Publishes CloudEvents payloads to each account's stream
   - Attempts a cross-account subscribe (must fail)
   - Implements a simple content-based routing rule in Go: read message, inspect `type` field of CloudEvents envelope, dispatch to one of two target handlers based on value
   - Measures and prints p50/p95/p99 publish latency for 10,000 messages
3. A `README.md` in the spike directory documenting findings

### Required Findings Document
After the spike, write `spikes/nats-routing/FINDINGS.md` answering all five questions above, plus:

- **Dynamic provisioning model decision:** CRD-based (Kubernetes operator watches `NATSAccount` CRDs and applies NATS config) vs Config API (CF-ResourceController calls a NATS management API). State which is chosen and why.
- **Routing engine design input:** Based on what the spike shows, describe the recommended approach for CF-EventRouter's runtime routing engine.
- **Gaps CF-EventRouter must close:** Anything NATS JetStream does not provide natively that the routing rules engine must implement in Go.

### Spike Fails If
- Cross-account isolation cannot be confirmed
- Dynamic account provisioning requires a NATS cluster restart
- p99 publish latency exceeds 50ms on local hardware under load

### Output Files

```
spikes/nats-routing/
├── cmd/
│   └── main.go
├── config/
│   └── nats-cluster.yaml
├── go.mod
├── go.sum
├── README.md
└── FINDINGS.md
```

### Go Libraries for This Spike
```
github.com/nats-io/nats.go
github.com/cloudevents/sdk-go/v2
```

---
## Task 0.7 — Deploy ScyllaDB in the k3d Dev Cluster

### Goal

Install the Scylla Operator and provision a single-node ScyllaDB cluster in the
`cf-data` namespace of the local k3d dev cluster. Create the
`cloudforge_platform` keyspace and the `resource_bindings` table that the
Resource Capability Binding System (Spike 0.7) will use as its durable store.

This gives every engineer a running, schema-ready ScyllaDB instance from
`make dev:up` onwards — no manual steps required.

---

### Context

ScyllaDB is CloudForge's DynamoDB replacement (architecture doc §5.5). It runs
in the `cf-data` namespace alongside CloudNativePG and MinIO. For local
development a single-node cluster is sufficient; the schema and access patterns
are identical to what production will use.

The binding store holds platform resource-to-resource permission records.
The access patterns that drive the schema design are:

- **Check** — "does binding (tenant A, function X → consume → queue Y) exist?"
  — primary key point lookup on `(tenant_id, binding_key)`
- **List by subject** — "what can this function do?"
  — prefix range scan on `(tenant_id, binding_key)` using the subject prefix
- **List by target** — "what can access this queue?"
  — served by a materialized view partitioned on `(tenant_id, target_kind, target_name)`

ScyllaDB's shard-per-core architecture makes single-digit microsecond point
lookups routine, which reduces pressure on the in-process cache hit rate
threshold compared to PostgreSQL.

---

### Inputs / Prerequisites

- Task 0.3 complete — k3d cluster running (`cloudforge-dev`), all namespaces
  applied, `local-path` storage class available
- `helm` ≥ 3.14 installed
- `kubectl` configured to the `cloudforge-dev` context

---

### Outputs / Deliverables

| Deliverable | Path | Description |
|---|---|---|
| Scylla Operator Helm values | `deploy/helm/components/scylla-operator/values.yaml` | Operator Helm overrides |
| ScyllaCluster manifest | `deploy/kustomize/components/scylladb/scyllacluster.yaml` | Single-node dev cluster spec |
| ScyllaDB ConfigMap | `deploy/kustomize/components/scylladb/config.yaml` | `scylla.yaml` server overrides |
| Schema CQL file | `deploy/kustomize/components/scylladb/schema.cql` | DDL for keyspace + tables |
| Schema init Job | `deploy/kustomize/components/scylladb/init-job.yaml` | Kubernetes Job that applies the DDL |
| Kustomization | `deploy/kustomize/components/scylladb/kustomization.yaml` | Wires all manifests above |
| k3d cluster patch | `deploy/k3d/cluster.yaml` | Add CQL port `9042` to exposed ports |
| Taskfile targets | `Taskfile.yml` | `deploy:scylladb`, `wait:scylladb`; called from `dev:up` |
| Makefile targets | `Makefile` | Mirror of Taskfile targets |

---

### k3d Cluster Patch

Add one entry to the `ports` list in `deploy/k3d/cluster.yaml` so `cqlsh`
and other local tooling can reach ScyllaDB without going through the ingress:

```yaml
# ScyllaDB CQL — exposed for local cqlsh and tooling
- port: "9042:9042"
  nodeFilters: ["server:0"]
```

---

### Scylla Operator Installation

The Scylla Operator must be installed once into a dedicated `scylla-operator`
namespace before any `ScyllaCluster` resource can be created.

```bash
helm repo add scylladb https://storage.googleapis.com/scylla-operator-charts/stable
helm repo update

helm upgrade --install scylla-operator scylladb/scylla-operator \
  --namespace scylla-operator \
  --create-namespace \
  --version 1.15.0 \
  --set logLevel=2
```

`deploy/helm/components/scylla-operator/values.yaml`:

```yaml
# Minimal overrides for the dev cluster.
# Production values are managed by CF-DBController in Phase 4.
logLevel: 2
```

---

### ScyllaDB Cluster Manifest

`deploy/kustomize/components/scylladb/scyllacluster.yaml`:

```yaml
apiVersion: scylla.scylladb.com/v1
kind: ScyllaCluster
metadata:
  name: cloudforge-scylla
  namespace: cf-data
  labels:
    app.kubernetes.io/part-of: cloudforge
    cloudforge.io/component: scylladb
spec:
  version: "6.2.2"
  agentVersion: "3.3.0"

  # developerMode: true disables kernel-level OS checks (huge pages, CPU governor)
  # that cannot be satisfied inside a k3d Docker node.
  # MUST be false in production.
  developerMode: true

  datacenter:
    name: local
    racks:
      - name: rack1
        scyllaConfig: cloudforge-scylla-config

        # local-path is k3d's default storage class.
        # Production uses a fast NVMe-backed StorageClass.
        storage:
          capacity: 10Gi
          storageClassName: local-path

        # Single member for dev. Production uses >= 3 members across racks.
        members: 1

        resources:
          requests:
            cpu: "500m"
            memory: "1Gi"
          limits:
            cpu: "1000m"
            memory: "2Gi"
```

`deploy/kustomize/components/scylladb/config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cloudforge-scylla-config
  namespace: cf-data
data:
  scylla.yaml: |
    # Enable password authentication so the platform service account
    # (cloudforge_svc) is the only client allowed to connect.
    authenticator: PasswordAuthenticator
    authorizer: CassandraAuthorizer
    # Alternator (DynamoDB API) is NOT enabled here.
    # The binding store uses native CQL for better performance and full schema control.
    # Alternator is activated by CF-DBController for consumer-facing DynamoDB
    # workloads in Phase 4.
```

---

### Schema

`deploy/kustomize/components/scylladb/schema.cql`:

```cql
-- ─────────────────────────────────────────────────────────────────────────────
-- CloudForge Platform Keyspace
-- RF=1 for dev; CF-DBController sets RF=3 in production.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE KEYSPACE IF NOT EXISTS cloudforge_platform
    WITH REPLICATION = {
        'class': 'NetworkTopologyStrategy',
        'local': 1
    }
    AND DURABLE_WRITES = true;

-- ─────────────────────────────────────────────────────────────────────────────
-- Resource Capability Bindings
--
-- Primary access pattern (hot path):
--   CHECK   SELECT * FROM resource_bindings
--           WHERE tenant_id = ? AND binding_key = ?
--
-- binding_key is a composite sort key built by the Go layer:
--   <subject_kind>#<subject_name>#<permission>#<target_kind>#<target_name>
--
-- This makes the permission check a single-partition point lookup (O(1)) and
-- "list all bindings for subject X" a narrow prefix range scan on the same
-- partition — no scatter reads, no secondary index fan-out.
--
-- The unpacked columns (subject_kind, subject_name, …) are stored alongside
-- binding_key for readability and direct Go struct mapping via gocqlx.
-- ScyllaDB stores them on the same SSTable row at no extra I/O cost.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cloudforge_platform.resource_bindings (
    tenant_id    TEXT,
    binding_key  TEXT,
    id           UUID,
    subject_kind TEXT,
    subject_name TEXT,
    permission   TEXT,
    target_kind  TEXT,
    target_name  TEXT,
    created_at   TIMESTAMP,
    created_by   TEXT,
    PRIMARY KEY (tenant_id, binding_key)
) WITH CLUSTERING ORDER BY (binding_key ASC)
  AND gc_grace_seconds = 86400
  AND compaction = {'class': 'LeveledCompactionStrategy'}
  AND comment = 'Platform resource-to-resource capability bindings. See pkg/authz/README.md.';

-- ─────────────────────────────────────────────────────────────────────────────
-- Materialized View: look up bindings by target
--
-- Secondary access pattern:
--   LIST BY TARGET   SELECT * FROM resource_bindings_by_target
--                    WHERE tenant_id = ? AND target_kind = ? AND target_name = ?
--
-- Serves the "what resources have permission over this Queue/Storage/…?" query
-- used by the binding management API list-by-target endpoint and by the
-- admission webhook that validates new resource bindings.
--
-- A materialized view is used instead of a secondary index because ScyllaDB
-- secondary indexes are shard-local and require scatter reads across all shards.
-- A MV is a full replica of the data with a different partition key — point
-- lookups remain single-partition regardless of cluster size.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE MATERIALIZED VIEW IF NOT EXISTS cloudforge_platform.resource_bindings_by_target AS
    SELECT *
    FROM cloudforge_platform.resource_bindings
    WHERE tenant_id   IS NOT NULL
      AND binding_key  IS NOT NULL
      AND target_kind  IS NOT NULL
      AND target_name  IS NOT NULL
    PRIMARY KEY (
        (tenant_id, target_kind, target_name),
        binding_key
    )
    WITH CLUSTERING ORDER BY (binding_key ASC);

-- ─────────────────────────────────────────────────────────────────────────────
-- Service Account
-- The platform uses a dedicated credential pair; the default cassandra/cassandra
-- superuser is used only for this bootstrap step and must be disabled afterward.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE ROLE IF NOT EXISTS cloudforge_svc
    WITH PASSWORD = 'cf-dev-secret-change-in-prod'
    AND LOGIN = true
    AND SUPERUSER = false;

GRANT ALL PERMISSIONS ON KEYSPACE cloudforge_platform TO cloudforge_svc;
```

**Schema design notes (for implementers):**

- **Partition key is `tenant_id` alone.** All bindings for one tenant land on
  the same shard. Correct for an SME platform with tens to hundreds of tenants.
  At much larger scale a compound partition `(tenant_id, subject_kind)` would
  distribute load better — this is a CF-DBController concern for Phase 4.
- **`binding_key` encodes the full binding identity** as a single string so a
  `CHECK` query needs only one CQL equality predicate on the clustering column.
  The Go layer constructs it as
  `fmt.Sprintf("%s#%s#%s#%s#%s", subjectKind, subjectName, perm, targetKind, targetName)`.
- **`DURABLE_WRITES = true`** ensures the commit log is flushed before
  acknowledging a create or revoke — required for permission records where
  a lost write would silently grant or retain access.
- **`LeveledCompactionStrategy`** is preferred for the binding table because
  reads are frequent and uniform (point lookups by tenants), and LCS minimises
  read amplification by keeping SSTable count low.

---

### Schema Init Job

`deploy/kustomize/components/scylladb/init-job.yaml`:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: scylladb-schema-init
  namespace: cf-data
  annotations:
    cloudforge.io/schema-version: "0.1.0"
spec:
  # Clean up the completed pod automatically after 5 minutes.
  ttlSecondsAfterFinished: 300
  # Retry up to 10 times while ScyllaDB is still starting.
  backoffLimit: 10
  template:
    spec:
      restartPolicy: OnFailure
      initContainers:
        # Block until the CQL port is reachable before running DDL.
        - name: wait-for-scylla
          image: busybox:1.36
          command:
            - sh
            - -c
            - |
              until nc -z cloudforge-scylla-client.cf-data.svc.cluster.local 9042; do
                echo "waiting for ScyllaDB CQL port..."; sleep 3;
              done
      containers:
        - name: cqlsh
          image: scylladb/scylla:6.2.2
          command:
            - cqlsh
            - cloudforge-scylla-client.cf-data.svc.cluster.local
            - "9042"
            - -u
            - cassandra
            - -p
            - cassandra
            - -f
            - /schema/schema.cql
          volumeMounts:
            - name: schema
              mountPath: /schema
      volumes:
        - name: schema
          configMap:
            name: scylladb-schema-cql
```

---

### Kustomization

`deploy/kustomize/components/scylladb/kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1alpha1
kind: Component
resources:
  - config.yaml
  - scyllacluster.yaml
  - init-job.yaml
configMapGenerator:
  - name: scylladb-schema-cql
    namespace: cf-data
    files:
      - schema.cql
    options:
      disableNameSuffixHash: true
```

---

### Taskfile Targets

Add to `Taskfile.yml`. The `dev:up` task must call `deploy:scylladb` after
namespaces are applied.

```yaml
deploy:scylladb:
  desc: "Install Scylla Operator and provision the dev ScyllaDB cluster"
  cmds:
    - helm repo add scylladb https://storage.googleapis.com/scylla-operator-charts/stable --force-update
    - helm repo update
    - |
      helm upgrade --install scylla-operator scylladb/scylla-operator \
        --namespace scylla-operator \
        --create-namespace \
        --version 1.15.0 \
        -f deploy/helm/components/scylla-operator/values.yaml \
        --wait
    - kubectl apply -k deploy/kustomize/components/scylladb/
    - task: wait:scylladb

wait:scylladb:
  desc: "Wait for ScyllaDB cluster and schema init Job to become ready"
  cmds:
    - |
      kubectl wait scyllacluster/cloudforge-scylla \
        --for='condition=Available' \
        --timeout=300s \
        -n cf-data
    - |
      kubectl wait job/scylladb-schema-init \
        --for=condition=complete \
        --timeout=120s \
        -n cf-data
```

---

### Acceptance Criteria

- [ ] `make dev:up` results in a `ScyllaCluster` with `Available: true` in `cf-data`
- [ ] `kubectl get scyllacluster -n cf-data` shows `cloudforge-scylla` with `READY: 1`
- [ ] Schema init Job status is `Complete`
- [ ] The following CQL query succeeds from localhost:
  ```bash
  cqlsh localhost 9042 -u cloudforge_svc -p cf-dev-secret-change-in-prod \
    -e "DESCRIBE KEYSPACE cloudforge_platform;"
  ```
  Output includes both `resource_bindings` table and `resource_bindings_by_target` view.
- [ ] A test INSERT + SELECT roundtrip on `resource_bindings` succeeds via `cqlsh`
- [ ] `developerMode: true` is confirmed set (k3d cannot satisfy ScyllaDB's kernel requirements without it)
- [ ] The default `cassandra/cassandra` password is changed or the role is disabled after init
- [ ] `make dev:down` cleanly removes all ScyllaDB resources and their PVCs

---

### Notes for Spike 0.8 (Resource Capability Binding System)

Once this task is complete, the spike implements `ScyllaStore` using
`github.com/scylladb/gocqlx/v3` (the recommended ScyllaDB Go client, as noted
in the implementation plan §7):

```go
// pkg/authz/store/scylla.go
//
// Uses native CQL, NOT the Alternator (DynamoDB) API.
// Alternator is reserved for consumer-facing DynamoDB workloads in Phase 4.
cluster := gocql.NewCluster(
    "cloudforge-scylla-client.cf-data.svc.cluster.local",
)
cluster.Authenticator = gocql.PasswordAuthenticator{
    Username: "cloudforge_svc",
    Password: os.Getenv("CF_SCYLLA_PASSWORD"),
}
cluster.Keyspace = "cloudforge_platform"
session, err := gocqlx.WrapSession(cluster.CreateSession())
```

The `BindingStore` interface remains backend-agnostic. Only this constructor
differs between the PostgreSQL fallback (used if ScyllaDB is not yet deployed)
and the ScyllaDB implementation. The hot-path `Check` method is identical to
callers in CF-EventRouter and CF-FunctionTrigger regardless of which backend
is active.

---

## Spike 0.8 — Resource Capability Binding System

### Context and Motivation

CloudForge has **two distinct authorization problems** that must not be conflated:

**Problem A — User-to-Platform authorization** (human or service principal → API call):
> "Can tenant-user Alice invoke `storage:write` on bucket `my-model-weights`?"

This is a classic IAM question and belongs entirely in CF-IAM (Phase 1).
OPA + Rego is the correct tool here because the logic is complex: role hierarchies,
tenant and project scoping, service account principals, deny-override rules, and
AI workload identity patterns all need expressive policy evaluation.
OPA performance for this path is validated separately in Task 1.3 of Phase 1.

**Problem B — Resource-to-Resource authorization** (platform resource → platform resource):
> "Can this Function consume from this Queue?"
> "Can this Queue write to this Storage bucket?"
> "Can this AI training job read from this database?"

This is a different problem entirely. There is no human calling an API.
CF-EventRouter is routing an event, CF-FunctionTrigger is wiring a trigger —
and before doing so, the platform must confirm the binding between the source
resource and the destination resource is permitted.

**Why OPA is the wrong tool for Problem B:**
OPA is a stateless policy evaluation engine. It evaluates rules against data you
push into it, but it does not store state. For resource-to-resource permissions,
you need a **binding store** (who is connected to what, tenant-scoped, with full
CRUD lifecycle) combined with **structural type rules** (which resource kinds are
allowed to bind at all). Routing binding data into OPA and keeping it in sync
creates a secondary synchronisation problem with no benefit: the core decisions
are either Go-constant type rules (platform invariants that never change per-tenant)
or simple database lookups (does binding X exist?). Neither requires a policy language.

This spike validates the **Resource Capability Binding system** design, its
hot-path performance, its runtime mutability, and the proposed API surface — so
CF-ResourceController (Phase 1, Task 1.x) knows exactly what to build.

---

### Authorization Model

The system uses two layers:

**Layer 1 — Structural rules (compiled into `pkg/authz/rules.go`)**

Platform invariants, defined once by the platform team. Express which resource
*type* combinations are allowed to bind at all. Never changed at runtime; no
policy language required.

```
Function   → consume  → Queue
Function   → publish  → Queue
Function   → read     → Storage
Function   → write    → Storage
Function   → read     → Database
Queue      → trigger  → Function
App        → read     → Storage
Aoo        → write    → Storage
Queue      → write    → Storage
AIJob      → read     → Storage
AIJob      → read     → Database
AIServing  → read     → Storage
...
```

**Layer 2 — Instance bindings (PostgreSQL table, tenant-scoped)**

Actual bindings created by tenants through the CloudForge API. Stored in the
platform database. The subject and target must both belong to the same tenant
(enforced by a DB constraint and by the Go checker). A binding is rejected at
creation time if the subject-kind → permission → target-kind combination does
not exist in Layer 1.

```sql
resource_bindings (
    id          UUID PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    subject_kind TEXT NOT NULL,   -- e.g. "function"
    subject_name TEXT NOT NULL,   -- e.g. "process-orders"
    permission   TEXT NOT NULL,   -- e.g. "consume"
    target_kind  TEXT NOT NULL,   -- e.g. "queue"
    target_name  TEXT NOT NULL,   -- e.g. "order-events"
    created_at  TIMESTAMPTZ NOT NULL,
    created_by  TEXT NOT NULL,    -- IAM principal that created this binding
    UNIQUE (tenant_id, subject_kind, subject_name, permission, target_kind, target_name)
)
```

**Hot-path check (CF-EventRouter, CF-FunctionTrigger):**

```
Step 1 — structural (O(1) Go map):
  Is (Function, consume, Queue) in AllowedBindings?  → if not, deny immediately

Step 2 — instance (cache hit or DB query):
  Does binding (tenant-a/process-orders → consume → tenant-a/order-events) exist?
  → check write-through in-process cache first (< 1µs on hit)
  → fall back to PostgreSQL query on miss (single indexed lookup)
```

---

### Questions This Spike Must Answer

| # | Question | Acceptable Answer |
|---|---|---|
| Q1 | What is the p99 latency of a binding check via direct PostgreSQL query in the event routing hot path? | < 1 ms |
| Q2 | With a write-through in-process cache, what is the p99 binding check latency on a cache hit? | < 50 µs |
| Q3 | Can bindings be created and revoked at runtime without restarting any service? | Yes — define the cache invalidation strategy |
| Q4 | What is the correct API model for binding management (shape, scoping, error responses)? | Propose and validate OpenAPI spec |
| Q5 | Are Go-level structural rules sufficient for v1 type-level enforcement, or is a policy language (Cedar, OPA) required for the rule table? | Decision with rationale |

---

### Spike Scope

Time-box: **2 days maximum.**

**What to build in `spikes/resource-permissions/`:**

1. **`internal/model/`** — shared data types:
   - `ResourceKind`, `Permission` constants
   - `ResourceRef` struct (kind, name, tenant ID)
   - `Binding` struct
   - `AllowedBindings` table (Layer 1) as a compiled Go slice + lookup map

2. **`internal/store/`** — binding persistence:
   - `BindingStore` interface (Check, Create, Revoke, ListBySubject, ListByTarget)
   - `PostgresStore` implementation backed by `pgx/v5`
   - `CachedStore` write-through wrapper using `sync.Map` (or `ristretto` if eviction is needed)
   - Cache invalidation via NATS event `resource.binding.revoked` (demonstrates platform event bus reuse)

3. **`internal/checker/`** — the two-phase enforcement function:
   - `PermissionChecker` interface: `Check(ctx, subject, perm, target) (bool, error)`
   - `Checker` implementation: Layer 1 lookup → Layer 2 lookup → allow/deny
   - Strict cross-tenant guard: if `subject.TenantID != target.TenantID`, deny immediately

4. **`bench_test.go`** — Go benchmarks:
   - `BenchmarkCheck_CacheHit` — warm cache, repeated checks
   - `BenchmarkCheck_DBMiss` — cold cache, measures PostgreSQL query latency
   - `BenchmarkCheck_CacheInvalidation` — create + revoke cycle, measures propagation time
   - Use `testing.B` with `b.ReportMetric` for p50/p95/p99

5. **`api/openapi.yaml`** — proposed binding management API:
   ```
   POST   /v1/tenants/{tenant}/bindings          create a binding
   GET    /v1/tenants/{tenant}/bindings           list all bindings
   GET    /v1/tenants/{tenant}/bindings/{id}      get one binding
   DELETE /v1/tenants/{tenant}/bindings/{id}      revoke a binding
   GET    /v1/tenants/{tenant}/bindings?subject_kind=function&subject_name=X
   GET    /v1/tenants/{tenant}/bindings?target_kind=queue&target_name=Y
   ```

6. **`docker-compose.yaml`** — single-node PostgreSQL for the spike

---

### Core Go Interfaces to Design and Validate

```go
// pkg/authz/resource_checker.go  (proposed location in the monorepo)

// ResourceKind identifies a CloudForge resource type.
type ResourceKind string

const (
    KindFunction  ResourceKind = "function"
    KindQueue     ResourceKind = "queue"
    KindStorage   ResourceKind = "storage"
    KindDatabase  ResourceKind = "database"
    KindAIJob     ResourceKind = "ai_job"
    KindAIServing ResourceKind = "ai_serving"
)

// Permission names the capability being granted or checked.
type Permission string

const (
    PermConsume Permission = "consume"
    PermPublish Permission = "publish"
    PermRead    Permission = "read"
    PermWrite   Permission = "write"
    PermTrigger Permission = "trigger"
)

// ResourceRef uniquely identifies a single platform resource instance.
type ResourceRef struct {
    Kind     ResourceKind
    Name     string // unqualified name within the tenant
    TenantID string
}

// PermissionChecker is the single call-site interface used by CF-EventRouter,
// CF-FunctionTrigger, and any other service that enforces resource-to-resource
// permissions at runtime.
type PermissionChecker interface {
    Check(ctx context.Context, subject ResourceRef, perm Permission, target ResourceRef) (bool, error)
}
```

---

### Cache Invalidation Strategy to Validate

When a binding is revoked via the API:
1. CF-ResourceController deletes the row from PostgreSQL.
2. CF-ResourceController publishes `resource.binding.revoked` to NATS (CloudEvents envelope).
3. Every service instance that holds a `CachedStore` subscribes to this subject and removes the entry from its local `sync.Map`.
4. A fallback TTL (30 seconds) ensures stale entries are evicted even if the NATS event is missed.

The spike must confirm that step 3 propagates within an acceptable window (< 500 ms on a local cluster).

---

### Required Findings Document

After the spike, write `spikes/resource-permissions/FINDINGS.md` with:

- Benchmark results table (DB query latency, cache hit latency, invalidation propagation time)
- **Decision: is Go-level structural rules table sufficient for v1, or do we need Cedar/OPA for type rules?** Rationale required.
- **Confirmed API shape** for binding management — the exact OpenAPI spec that CF-ResourceController Task 1.x should implement
- **Cache invalidation approach confirmed** — NATS-based invalidation latency measured
- **Cross-tenant enforcement confirmed** — test that a binding where `subject.tenantID != target.tenantID` is rejected at both the API layer and the checker layer

---

### Output Files

```
spikes/resource-permissions/
├── internal/
│   ├── model/
│   │   ├── types.go           ← ResourceKind, Permission, ResourceRef, Binding
│   │   └── rules.go           ← AllowedBindings table (Layer 1)
│   ├── store/
│   │   ├── interface.go       ← BindingStore interface
│   │   ├── postgres.go        ← pgx/v5 implementation
│   │   └── cached.go          ← write-through sync.Map cache wrapper
│   └── checker/
│       ├── interface.go       ← PermissionChecker interface
│       └── checker.go         ← two-phase check implementation
├── api/
│   └── openapi.yaml           ← proposed binding management API
├── bench_test.go              ← Go benchmarks (DB, cache, invalidation)
├── docker-compose.yaml        ← single-node PostgreSQL
├── go.mod
├── go.sum
├── README.md
└── FINDINGS.md
```

### Spike Fails If
- p99 PostgreSQL binding check latency exceeds 1 ms under realistic load
- Cache invalidation via NATS event takes longer than 500 ms to propagate
- Cross-tenant binding cannot be reliably rejected at both the API and checker layers
- The Go structural rules table is judged insufficient for v1 and a policy language is required (this is not a failure of the spike, but it changes the Phase 1 implementation scope significantly)

### Go Libraries for This Spike
```
github.com/jackc/pgx/v5
github.com/google/uuid
github.com/nats-io/nats.go
github.com/stretchr/testify
```

---

## Task 0.8 — Spike: Knative Scale-to-Zero Cold Start

### Goal

Install Knative Serving on the local k3d dev cluster, deploy three function variants
(minimal / medium / heavy), force each to scale to zero, then send a cold request
and record time-to-first-byte. A Go measurement tool collects 10 samples per variant
and prints a terminal performance table.

The output drives one platform decision:
> **What is the default `minScale` value in CF-FunctionTrigger for each function
> weight class, and when must users explicitly set `minScale: 1`?**

---

### Context

CF-FunctionTrigger (Phase 6) wraps every consumer workload as a Knative `Service`.
`scale-to-zero` is on by default. A function that has been idle for 30 seconds is
terminated — the next request pays the full cold-start cost:

```
Request arrives
    → Autoscaler detects 0 ready pods
    → Kubernetes schedules a new pod
    → Container runtime pulls image (if not in node cache)
    → Process starts, HTTP server binds
    → Request is forwarded and answered
```

For AI-calling functions this chain can take 3–8 seconds depending on image size
and memory footprint. This spike produces the data needed to write accurate
platform documentation and set safe defaults.

---

### Prerequisites

| Requirement | Notes |
|---|---|
| Task 0.3 complete | `cloudforge-dev` k3d cluster running, port 9080→80 exposed on loadbalancer |
| `kubectl` context set to `cloudforge-dev` | `kubectl config current-context` must return `k3d-cloudforge-dev` |
| Docker daemon running | Knative images are ~600 MB total; image pull happens once |
| `ko` installed | `go install github.com/ko-build/ko/cmd/ko@latest` — used to build function images into the cluster |

---

### Outputs / Deliverables

| Deliverable | Path | Description |
|---|---|---|
| Knative Serving install manifests | `spikes/knative-coldstart/deploy/knative-serving.yaml` | Serving CRDs + core components |
| Kourier networking | `spikes/knative-coldstart/deploy/knative-net-kourier.yaml` | Lightweight ingress gateway |
| Domain config patch | `spikes/knative-coldstart/deploy/config-domain.yaml` | Sets `127.0.0.1.sslip.io` magic DNS |
| Minimal function | `spikes/knative-coldstart/functions/minimal/main.go` | Pure Go handler, distroless image |
| Medium function | `spikes/knative-coldstart/functions/medium/main.go` | Go handler + embedded 50 MB payload |
| Heavy function | `spikes/knative-coldstart/functions/heavy/main.go` | Go handler + embedded 200 MB payload |
| Knative service manifests | `spikes/knative-coldstart/deploy/service-*.yaml` | One per variant, scale-to-zero config |
| Measurement tool | `spikes/knative-coldstart/cmd/measure/main.go` | Collects samples, prints performance table |
| Makefile | `spikes/knative-coldstart/Makefile` | `deploy-knative`, `deploy-functions`, `measure`, `teardown` |
| Findings | `spikes/knative-coldstart/FINDINGS.md` | Results + min-replica recommendations |

---

### Step 1 — Install Knative Serving on k3d

Use the standalone YAML manifests (no Knative Operator) to minimise cluster
resource usage. Knative v1.15.x targets Kubernetes ≥ 1.27 which k3d easily satisfies.

```bash
# 1a. Install Serving CRDs
kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.15.0/serving-crds.yaml

# 1b. Install Serving core components (in knative-serving namespace)
kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.15.0/serving-core.yaml

# 1c. Install net-kourier (lightweight ingress — works with k3d, no Istio/Envoy gateway needed)
kubectl apply -f https://github.com/knative/net-kourier/releases/download/knative-v1.15.0/kourier.yaml

# 1d. Tell Knative Serving to use Kourier
kubectl patch configmap/config-network \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}'

# 1e. Configure magic DNS so service URLs resolve on localhost.
#     sslip.io resolves *.127.0.0.1.sslip.io → 127.0.0.1.
#     k3d already maps host:9080 → cluster:80 (loadbalancer).
kubectl apply -f deploy/config-domain.yaml
```

`deploy/config-domain.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-domain
  namespace: knative-serving
data:
  # Any service deployed to the cluster is accessible at:
  # http://<service>.<namespace>.127.0.0.1.sslip.io:9080
  # The :9080 port matches the k3d loadbalancer mapping port: "9080:80"
  127.0.0.1.sslip.io: ""
```

Download manifests once and vendor them into the spike directory so the spike
works offline and at a pinned version:

```bash
mkdir -p spikes/knative-coldstart/deploy
curl -sLo spikes/knative-coldstart/deploy/knative-serving.yaml \
  https://github.com/knative/serving/releases/download/knative-v1.15.0/serving-core.yaml
curl -sLo spikes/knative-coldstart/deploy/knative-net-kourier.yaml \
  https://github.com/knative/net-kourier/releases/download/knative-v1.15.0/kourier.yaml
```

Wait for all Knative pods to be ready before deploying functions:

```bash
kubectl wait deployment --all \
  --for=condition=Available \
  --timeout=120s \
  -n knative-serving
```

---

### Step 2 — Function Variants

Each variant is a standalone Go HTTP handler. The image size is controlled by
embedding a synthetic binary file of the target size using Go's `//go:embed` directive.
This simulates real-world functions that carry model weights, ML libraries, or
embedded assets.

**Minimal (`functions/minimal/main.go`)** — target image size < 10 MB:

```go
// Package main is the CloudForge spike "minimal" function variant.
// It serves a single HTTP endpoint that echoes the request method and path.
// Built with ko using gcr.io/distroless/static-debian12 as the base — no shell,
// no package manager, minimal attack surface.
package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
)

func main() {
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "variant=minimal method=%s path=%s\n", r.Method, r.URL.Path)
    })
    log.Printf("minimal function listening on :%s", port)
    log.Fatal(http.ListenAndServe(":"+port, nil))
}
```

**Medium (`functions/medium/main.go`)** — target image ~100 MB:

```go
// Package main is the CloudForge spike "medium" function variant.
// It embeds a 50 MB synthetic binary file to simulate a function that carries
// a small ML model or embedded asset. Image size is ~100 MB (ubuntu base).
package main

import (
    _ "embed"
    "fmt"
    "log"
    "net/http"
    "os"
)

// payload is a 50 MB file generated by `make gen-payloads`.
// The Go compiler embeds it at build time — no runtime file I/O needed.
//
//go:embed payload.bin
var payload []byte

func main() {
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        // Access one byte to prevent the compiler from optimising away the embed.
        fmt.Fprintf(w, "variant=medium payload_len=%d method=%s\n", len(payload), r.Method)
    })
    log.Printf("medium function listening on :%s (payload %d bytes)", port, len(payload))
    log.Fatal(http.ListenAndServe(":"+port, nil))
}
```

**Heavy (`functions/heavy/main.go`)** — target image ~500 MB:

```go
// Package main is the CloudForge spike "heavy" function variant.
// Embeds a 200 MB synthetic binary to simulate a function bundling a large model
// checkpoint or a native shared library. Image size is ~500 MB.
//
//go:embed payload.bin
var payload []byte
// … (same handler pattern as medium)
```

Generate the payload files in the Makefile:

```makefile
gen-payloads: ## Generate synthetic payload files for medium and heavy variants
    dd if=/dev/urandom of=functions/medium/payload.bin bs=1M count=50
    dd if=/dev/urandom of=functions/heavy/payload.bin  bs=1M count=200
```

---

### Step 3 — Knative Service Manifests

One `Knative Service` per variant. Critical settings for the spike:
- `autoscaling.knative.dev/min-scale: "0"` — forces scale-to-zero.
- `autoscaling.knative.dev/initial-scale: "0"` — starts at zero; no warm replica.
- `autoscaling.knative.dev/scale-to-zero-grace-period: "30s"` — idle window before termination.

`deploy/service-minimal.yaml`:

```yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: fn-minimal
  namespace: default
  annotations:
    # Force scale-to-zero so each measurement round starts from a cold pod.
    autoscaling.knative.dev/min-scale: "0"
    autoscaling.knative.dev/initial-scale: "0"
    autoscaling.knative.dev/scale-to-zero-grace-period: "30s"
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/min-scale: "0"
    spec:
      containers:
        # ko builds the image and pushes it into the k3d container registry.
        # Replace the image tag with the ko-built digest before applying.
        - image: ko://github.com/jtomasevic/cloud-forge/spikes/knative-coldstart/functions/minimal
          ports:
            - containerPort: 8080
          resources:
            requests:
              cpu: "50m"
              memory: "32Mi"
            limits:
              cpu: "200m"
              memory: "128Mi"
```

Repeat for `service-medium.yaml` and `service-heavy.yaml` with appropriate
memory limits (medium: 256 Mi, heavy: 768 Mi) and the matching `ko://` image path.

---

### Step 4 — Measurement Tool

`cmd/measure/main.go` drives the benchmark. It must:

1. Accept a `--service` flag (`minimal | medium | heavy | all`) and a `--samples` flag (default 10).
2. Before each sample — wait for the pod count to reach zero by polling the Knative
   service's `status.observedGeneration` and the underlying deployment's `readyReplicas`.
3. Send an HTTP `GET` to the Knative service URL and measure the duration from
   request start to reading the first byte of the response (time-to-first-byte, TTFB).
4. After all samples, compute p50 / p75 / p95 / p99 / min / max, then print the
   table (see expected terminal output below).
5. Exit non-zero if any p95 exceeds its threshold so `make measure` can fail CI.

Key implementation pattern for TTFB measurement:

```go
// measureTTFB sends a single GET request and returns the time elapsed from
// connection start to the moment the first byte of the response body is available.
// It does NOT read the full body — Knative counts the connection as active until
// the body is fully read, which would prevent scale-to-zero in subsequent rounds.
func measureTTFB(ctx context.Context, url string) (time.Duration, error) {
    start := time.Now()
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return 0, err
    }
    // Read exactly one byte — this is the moment the HTTP server has responded.
    buf := make([]byte, 1)
    _, err = resp.Body.Read(buf)
    resp.Body.Close()
    return time.Since(start), err
}
```

Scale-to-zero detection:

```go
// waitForScaleToZero polls the Knative Service's ready replica count every 5 s
// until zero pods are running or the context deadline is exceeded.
// It uses the Kubernetes client-go informer rather than kubectl to avoid shell
// dependencies inside the benchmark binary.
func waitForScaleToZero(ctx context.Context, svc, namespace string) error {
    // Poll deployment/<svc>-00001-deployment in the same namespace.
    // Knative names the underlying deployment <revision>-deployment.
    // In practice revision name is <svc>-NNNNN; the simplest probe is to check
    // the pod count directly via the label selector app=<svc>.
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(5 * time.Second):
            count, err := readyPodCount(ctx, svc, namespace)
            if err != nil {
                return err
            }
            if count == 0 {
                return nil
            }
        }
    }
}
```

---

### Step 5 — Expected Terminal Output

This is the canonical terminal output the `measure` command must produce.
The exact numbers will differ on each machine; the format is fixed.

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║  CloudForge — Knative Scale-to-Zero Cold Start Benchmark                            ║
║  Cluster : cloudforge-dev (k3d)    Knative Serving v1.15   net-kourier              ║
║  Samples : 10 per variant          Platform: darwin/arm64  Docker 27.x              ║
╠══════════════╦══════════╦══════════╦═════════╦═════════╦═════════╦═════════╦════════╣
║ Variant      ║ Image    ║  p50     ║  p75    ║  p95    ║  p99    ║  min    ║  max   ║
╠══════════════╬══════════╬══════════╬═════════╬═════════╬═════════╬═════════╬════════╣
║ minimal      ║    8 MB  ║  1.21s   ║  1.43s  ║  2.08s  ║  2.71s  ║  0.89s  ║  2.71s ║
║ medium       ║   98 MB  ║  2.84s   ║  3.21s  ║  4.12s  ║  4.67s  ║  2.14s  ║  4.67s ║
║ heavy        ║  512 MB  ║  5.31s   ║  6.14s  ║  7.44s  ║  8.23s  ║  4.84s  ║  8.23s ║
╚══════════════╩══════════╩══════════╩═════════╩═════════╩═════════╩═════════╩════════╝

Warm-path latency (min-replicas=1, no cold start):
  minimal  →   3ms p50
  medium   →   4ms p50
  heavy    →   6ms p50

─── Threshold Analysis ──────────────────────────────────────────────────────────────
  ✓ minimal : p95 2.08s — below 3.0s threshold. Scale-to-zero is safe.
  ⚠ medium  : p95 4.12s — below 5.0s threshold but > 3.0s. Recommend min-replicas=1
               for AI functions carrying embedded model weights.
  ✗ heavy   : p95 7.44s — EXCEEDS 5.0s threshold. min-replicas=1 REQUIRED.
               Platform documentation must set this as a hard requirement for
               functions with image size > 200 MB.

─── Recommendation ─────────────────────────────────────────────────────────────────
  CF-FunctionTrigger default  minScale=0  maxScale=10
  Override for medium/AI      minScale=1  (add to function manifest)
  Override for heavy/ML       minScale=1  (enforced by admission webhook)
```

The tool exits `1` if any variant's p95 breaches its threshold so `make ci-measure`
can gate a PR.

---

### Step 6 — Makefile Targets

`spikes/knative-coldstart/Makefile`:

```makefile
CLUSTER     := cloudforge-dev
KO_REGISTRY := k3d-cloudforge-registry:5001
NAMESPACE   := default
SAMPLES     ?= 10

.PHONY: help
help:
	@awk 'BEGIN {FS=":.*##"} /^[a-z].*:.*##/ {printf "  %-22s %s\n",$$1,$$2}' $(MAKEFILE_LIST)

# ── Knative installation ──────────────────────────────────────────────────────

.PHONY: deploy-knative
deploy-knative: ## Install Knative Serving + net-kourier on the k3d cluster
	kubectl apply -f deploy/knative-serving.yaml
	kubectl apply -f deploy/knative-net-kourier.yaml
	kubectl patch configmap/config-network -n knative-serving --type merge \
		--patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}'
	kubectl apply -f deploy/config-domain.yaml
	kubectl wait deployment --all --for=condition=Available --timeout=120s -n knative-serving
	@echo "✓ Knative Serving is ready."

.PHONY: undeploy-knative
undeploy-knative: ## Remove Knative Serving from the cluster
	kubectl delete -f deploy/knative-net-kourier.yaml --ignore-not-found
	kubectl delete -f deploy/knative-serving.yaml --ignore-not-found

# ── Function variants ─────────────────────────────────────────────────────────

.PHONY: gen-payloads
gen-payloads: ## Generate synthetic embedded payload files (run once)
	dd if=/dev/urandom of=functions/medium/payload.bin bs=1M count=50 status=progress
	dd if=/dev/urandom of=functions/heavy/payload.bin  bs=1M count=200 status=progress

.PHONY: deploy-functions
deploy-functions: gen-payloads ## Build function images with ko and apply Knative Services
	KO_DOCKER_REPO=$(KO_REGISTRY) ko apply -f deploy/service-minimal.yaml
	KO_DOCKER_REPO=$(KO_REGISTRY) ko apply -f deploy/service-medium.yaml
	KO_DOCKER_REPO=$(KO_REGISTRY) ko apply -f deploy/service-heavy.yaml
	@echo "✓ Functions deployed."

.PHONY: undeploy-functions
undeploy-functions: ## Delete all three Knative Services
	kubectl delete -f deploy/service-minimal.yaml --ignore-not-found
	kubectl delete -f deploy/service-medium.yaml  --ignore-not-found
	kubectl delete -f deploy/service-heavy.yaml   --ignore-not-found

# ── Benchmark ─────────────────────────────────────────────────────────────────

.PHONY: measure
measure: ## Run cold-start benchmark for all variants and print performance table
	go run ./cmd/measure --service=all --samples=$(SAMPLES) --namespace=$(NAMESPACE)

.PHONY: measure-minimal
measure-minimal: ## Benchmark the minimal variant only
	go run ./cmd/measure --service=minimal --samples=$(SAMPLES) --namespace=$(NAMESPACE)

.PHONY: measure-warm
measure-warm: ## Measure warm-path latency (min-replicas=1, no cold start)
	go run ./cmd/measure --service=all --samples=$(SAMPLES) --namespace=$(NAMESPACE) --warm

# ── Teardown ──────────────────────────────────────────────────────────────────

.PHONY: teardown
teardown: undeploy-functions undeploy-knative ## Full cleanup — remove functions and Knative
```

---

### Step 7 — Root Makefile Integration

Add the following to the root `Makefile` under `##@ Dev Cluster`:

```makefile
.PHONY: deploy-knative
deploy-knative: ## Install Knative Serving + net-kourier on the dev cluster
	$(MAKE) -C spikes/knative-coldstart deploy-knative

.PHONY: measure-coldstart
measure-coldstart: ## Run scale-to-zero cold-start benchmark (requires deploy-knative + deploy-functions)
	$(MAKE) -C spikes/knative-coldstart measure
```

---

### Acceptance Criteria

| # | Criterion | Pass condition |
|---|---|---|
| AC1 | Knative Serving installs without errors | All pods in `knative-serving` namespace reach `Running` within 2 min |
| AC2 | Functions build and deploy via `ko` | `kubectl get ksvc` shows `READY=True` for all three variants |
| AC3 | Scale-to-zero triggers | After 30 s idle, `kubectl get pods -n default` shows 0 pods for each function |
| AC4 | Measurement tool produces table | `make measure` prints the full performance table to stdout |
| AC5 | Minimal variant p95 < 3 s | Recorded in `FINDINGS.md` |
| AC6 | Go HTTP client invocation confirmed | `cmd/measure` successfully receives `200 OK` for all variants — no curl/kubectl exec |
| AC7 | FINDINGS.md written | Contains p50/p95/p99 per variant, min-replica recommendation, network backend choice |

---

### File Layout

```
spikes/knative-coldstart/
├── cmd/
│   └── measure/
│       └── main.go          # benchmark driver — polls scale-to-zero, measures TTFB, prints table
├── functions/
│   ├── minimal/
│   │   └── main.go
│   ├── medium/
│   │   ├── main.go
│   │   └── payload.bin      # generated by make gen-payloads (gitignored)
│   └── heavy/
│       ├── main.go
│       └── payload.bin      # generated by make gen-payloads (gitignored)
├── deploy/
│   ├── knative-serving.yaml    # vendored Knative Serving v1.15 core manifests
│   ├── knative-net-kourier.yaml
│   ├── config-domain.yaml      # 127.0.0.1.sslip.io magic DNS patch
│   ├── service-minimal.yaml
│   ├── service-medium.yaml
│   └── service-heavy.yaml
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── FINDINGS.md
```

`.gitignore` inside `spikes/knative-coldstart/`:
```
functions/medium/payload.bin
functions/heavy/payload.bin
```

---

### Go Libraries for This Spike

```
k8s.io/client-go           — pod count polling (readyReplicas)
knative.dev/serving/pkg    — Knative Service type (optional — can use unstructured)
github.com/olekukonko/tablewriter — terminal table rendering
github.com/spf13/pflag     — CLI flags (--service, --samples, --warm)
```


## Spike 0.8 — Knative Scale-to-Zero Cold Start

### Context and Motivation
CF-FunctionTrigger (Phase 6) deploys consumer workloads as Knative Serving services with scale-to-zero enabled. This is the default configuration for cost efficiency in SME deployments. The risk: if cold start latency after scale-to-zero exceeds an acceptable threshold, event-triggered AI workloads (e.g., a function that calls an inference endpoint) will have unacceptably long first-request latency.

Minimum replica guidance for the platform documentation depends on this data.

### Questions This Spike Must Answer

| # | Question | Acceptable Answer |
|---|---|---|
| Q1 | What is the cold start latency (time from first request to response) for a minimal Go function after scale-to-zero? | < 3s on k3d local cluster |
| Q2 | What is the cold start latency for a medium function (100MB image, 10MB memory footprint)? | < 5s |
| Q3 | What is the cold start latency for a heavy function (500MB image, 100MB memory footprint)? | Document — may require min-replicas guidance |
| Q4 | Does direct HTTP invocation from a Go HTTP client work correctly against a Knative service URL? | Yes — confirm end-to-end |
| Q5 | What minimum replica setting eliminates cold start? | 1 replica (always on) |

### Spike Scope
Time-box: **2 days maximum.**

**What to build in `spikes/knative-coldstart/`:**
1. Deploy Knative Serving on the local k3d cluster (Task 0.3 must be complete first)
   - Use Knative Operator or direct manifests
   - Configure with k3d-compatible networking (net-kourier or net-contour)
2. Three test Knative services:
   - **Minimal:** Simple Go HTTP handler, `gcr.io/distroless/static` base, < 10MB image
   - **Medium:** Go HTTP handler + embedded 50MB data file, `ubuntu` base, ~100MB image
   - **Heavy:** Go HTTP handler + embedded 200MB data file, ~500MB image
3. A Go measurement script (`cmd/measure/main.go`) that:
   - Waits for a service to scale to zero (polls Knative service status)
   - Sends a request and measures time-to-first-byte
   - Repeats 10 times per function size
   - Prints results table

### Required Findings Document
After the spike, write `spikes/knative-coldstart/FINDINGS.md` with:
- Cold start latency results (p50/p95/p99 per function size)
- **Minimum replica recommendation:** For AI-calling functions, document whether min-replicas=1 should be the platform default
- **Confirmed:** Go HTTP client calling a Knative service URL works (include the exact call pattern used)
- Network backend recommendation for CloudForge (net-kourier vs net-contour)
- Any Knative configuration tweaks that reduce cold start (e.g., container pre-warming)

### Output Files

```
spikes/knative-coldstart/
├── functions/
│   ├── minimal/main.go
│   ├── medium/main.go
│   └── heavy/main.go
├── cmd/
│   └── measure/main.go
├── deploy/
│   ├── knative-serving.yaml
│   ├── service-minimal.yaml
│   ├── service-medium.yaml
│   └── service-heavy.yaml
├── go.mod
├── README.md
└── FINDINGS.md
```

### Spike Fails If
- Cold start for the minimal function exceeds 5 seconds
- Direct Go HTTP invocation does not work against a Knative service URL
- Knative Serving cannot be installed on k3d without significant workarounds

---

## Spike 0.9 — GPU Scheduling and vLLM Deployment Validation

### Context and Motivation
CF-AIRuntime (Phase 6, Task 6.4) is **on the MVP critical path**. The entire AI serving infrastructure depends on KServe and vLLM working correctly on Kubernetes with GPU scheduling. This is the highest-risk spike in Phase 0 — if vLLM cannot be deployed with the KServe `ServingRuntime` abstraction, or if GPU scheduling has problems, the Phase 6 design must change before it is committed to.

Additionally: most early self-hosted deployments will not have GPU nodes. The Ollama CPU-mode fallback must also be validated here — it is the path that makes AI serving available to everyone.

### Questions This Spike Must Answer

| # | Question | Acceptable Answer |
|---|---|---|
| Q1 | Can the NVIDIA device plugin be installed on a Kubernetes node and GPU resources correctly scheduled? | Yes — `nvidia.com/gpu: 1` resource request is fulfilled |
| Q2 | Can a KServe `ServingRuntime` manifest configure vLLM as the inference backend? | Yes — provide working manifest |
| Q3 | Can a small model (Qwen2.5-1.5B or similar) be loaded from a path and served via KServe `InferenceService`? | Yes — model serves `POST /v1/chat/completions` |
| Q4 | Does a Go HTTP client correctly receive a streamed (chunked-transfer) response from the vLLM endpoint? | Yes — tokens arrive before response completes |
| Q5 | Can Ollama (CPU mode) serve the same OpenAI-compatible API as a drop-in substitute? | Yes — same Go client code works against both |
| Q6 | **Decision: KServe + vLLM vs bare vLLM Deployment?** | Document recommendation with trade-offs |

### Spike Scope
Time-box: **3 days maximum.**

**Infrastructure required:**
- For GPU path: a cloud ephemeral GPU instance (e.g., AWS `g4dn.xlarge`, GCP `n1-standard-4 + T4`, or a local workstation with NVIDIA GPU). This does **not** need to be the production k3d cluster.
- For Ollama CPU path: local k3d cluster from Task 0.3 is sufficient.

**What to build in `spikes/ai-runtime/`:**

**Part 1 — GPU Path (on cloud GPU node):**
1. NVIDIA device plugin Helm values file that installs correctly on the target Kubernetes version
2. A `ServingRuntime` CRD manifest for vLLM that:
   - Uses vLLM container image `vllm/vllm-openai:latest`
   - Sets GPU resource request/limit `nvidia.com/gpu: 1`
   - Exposes port 8000 with path `/v1`
3. An `InferenceService` manifest that:
   - References the vLLM `ServingRuntime`
   - Pulls model from a MinIO path (or uses `huggingface://Qwen/Qwen2.5-1.5B-Instruct`)
   - Configures autoscaling: min=1, max=3
4. A Go HTTP client (`cmd/infer/main.go`) that:
   - Calls `POST /v1/chat/completions` with streaming enabled
   - Reads the SSE stream chunk-by-chunk
   - Prints each token as it arrives (confirms streaming works end-to-end)
   - Records time-to-first-token and total request duration

**Part 2 — Ollama CPU Fallback (on local k3d):**
1. A `ServingRuntime` CRD manifest for Ollama (CPU mode) that:
   - Uses Ollama container image `ollama/ollama:latest`
   - No GPU resource request
   - Serves OpenAI-compatible API on port 11434 at `/v1`
2. An `InferenceService` manifest that uses the Ollama `ServingRuntime` with a small model (e.g., `qwen2.5:1.5b`)
3. The **same Go HTTP client** from Part 1 pointed at the Ollama endpoint — must work without code changes

### Required Findings Document
After the spike, write `spikes/ai-runtime/FINDINGS.md` with:
- NVIDIA device plugin installation notes and any version-specific gotchas
- KServe `ServingRuntime` manifest (copy-paste ready for Phase 6)
- vLLM `InferenceService` manifest (copy-paste ready for Phase 6)
- Ollama `ServingRuntime` manifest (copy-paste ready for Phase 6)
- **Streaming validation result:** Confirm that the Go client receives tokens before the full response completes; include the exact `http.Flusher` pattern used
- **Decision: KServe + vLLM vs bare vLLM Deployment** — with rationale. Consider: CRD-based lifecycle management, multi-runtime support (vLLM and Ollama under same abstraction), upgrade path
- Time-to-first-token measured for the deployed model (record hardware spec alongside)
- Any surprises or blockers encountered

### Output Files

```
spikes/ai-runtime/
├── cmd/
│   └── infer/
│       └── main.go          # Go streaming inference client
├── deploy/
│   ├── nvidia-device-plugin-values.yaml
│   ├── serving-runtime-vllm.yaml
│   ├── serving-runtime-ollama.yaml
│   ├── inference-service-sample.yaml
│   └── kserve-install.yaml
├── go.mod
├── go.sum
├── README.md
└── FINDINGS.md
```

### Go Streaming Client Pattern
The Go client must use this pattern (validates the approach CF-AIRuntime's proxy will use in Phase 6):

```go
// Do NOT use ioutil.ReadAll or json.Decoder on the response body for streaming.
// Use bufio.Scanner to read SSE chunks as they arrive.
resp, err := http.Post(url, "application/json", body)
scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
    line := scanner.Text()
    if strings.HasPrefix(line, "data: ") {
        // parse and print token
    }
}
```

### Spike Fails If
- GPU resource requests cannot be scheduled on the test node
- vLLM fails to start inside a KServe `InferenceService` (image compatibility issues)
- The Go streaming client receives the entire response buffered rather than streamed
- Ollama CPU mode does not serve the OpenAI-compatible API correctly

---

## Phase 0 Completion Checklist

Before Phase 1 begins, **all** of the following must be true:

### Infrastructure Track
- [ ] **0.1** Repository structure matches the documented layout; `go work sync` and `go build ./...` pass
- [ ] **0.2** CI pipeline is green; a failing test blocks a PR; a passing merge to `main` publishes images
- [ ] **0.3** `task dev:up` brings up a k3d cluster with all namespaces; `task dev:down` cleans it up
- [ ] **0.4** All `internal/` packages have implementations and passing unit tests
- [ ] **0.5** `task gen:api storage` generates compilable Go stubs and a typed client from the sample spec

### Spike Track
- [ ] **0.6** NATS routing spike: `FINDINGS.md` filed; dynamic account provisioning confirmed or alternative documented; Phase 5 design decision recorded
- [ ] **0.7** OPA embedded spike: `FINDINGS.md` filed; benchmark results documented; embedded vs daemon decision recorded; initial Rego structure defined
- [ ] **0.8** Knative cold start spike: `FINDINGS.md` filed; latency measurements documented; minimum replica guidance recorded
- [ ] **0.9** GPU/vLLM spike: `FINDINGS.md` filed; KServe + vLLM manifests confirmed; Ollama CPU fallback confirmed; streaming Go client working; KServe vs bare Deployment decision recorded

### Gate
**Phase 1 does not start until all nine tasks above are checked off.**

---

## Dependency Graph

```
Task 0.1 ──────────────────────────────────────────┐
    │                                               │
    ▼                                               ▼
Task 0.2                                        Task 0.4 (parallel with 0.5)
    │                                               │
    ▼                                               ▼
Task 0.3 ──────┬──────────────────────────────> Task 0.5
               │
               ├──> Spike 0.6 (NATS)       ─┐
               ├──> Spike 0.7 (OPA)         │ All parallel
               ├──> Spike 0.8 (Knative)     │
               └──> Spike 0.9 (GPU/vLLM)  ──┘
```

**Notes:**
- Tasks 0.4 and 0.5 can start as soon as 0.1 is done; they do not need 0.2 or 0.3
- All four spikes can run in parallel once 0.3 is running
- Spike 0.7 (OPA) only needs 0.1, not 0.3 — it can start even earlier

---

## Staffing Suggestion (4-Engineer Team)

| Week | E1 | E2 | E3 | E4 |
|---|---|---|---|---|
| 1 | Task 0.1 — Repo setup | Task 0.1 (pair) | Task 0.2 — CI/CD | Task 0.2 (pair) |
| 2 | Task 0.3 — Local cluster | Task 0.4 — Shared libs | Task 0.5 — OpenAPI toolchain | Spike 0.7 — OPA (starts early, only needs 0.1) |
| 3 | Spike 0.6 — NATS | Spike 0.8 — Knative | Spike 0.9 — GPU/vLLM | Spike 0.7 — OPA complete; assist 0.9 |
| 4 | Spike findings review + documentation | Phase 1 prep | Phase 1 prep | Phase 1 prep |

---

*Document generated from `docs/2-cloud-forge-implementation-plan.v0.1.md` Phase 0 section.*  
*Intended for use as AI agent task specifications and engineering sprint planning.*

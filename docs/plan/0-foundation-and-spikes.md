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
- Container registry: GitHub Container Registry (`ghcr.io/cloud-forge/<service>`)

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
   - Jobs: build images via `ko` and push to `ghcr.io/cloud-forge/`
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
// Chain returns a chi-compatible middleware chain with all standard middlewares applied.
func Chain(tracer trace.Tracer, logger *slog.Logger, registry *prometheus.Registry) chi.Middlewares

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
github.com/go-chi/chi/v5
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
Establish the API-first development pattern. Every CloudForge REST API is defined in an OpenAPI 3.1 spec first, and server stubs + client SDKs are generated from it. Validate the entire toolchain end-to-end with one sample service before any real service is built.

### Context
This task depends on Task 0.1 only. It defines the toolchain that all subsequent service tasks will use. The pattern must be working and documented before Phase 1 begins.

### Inputs / Prerequisites
- Task 0.1 complete
- `oapi-codegen` installed (`go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest`)

### Outputs / Deliverables

| Deliverable | Path | Description |
|---|---|---|
| oapi-codegen config (server) | `api/<service>/v1/oapi-server.cfg.yaml` | Config for server stub generation |
| oapi-codegen config (client) | `api/<service>/v1/oapi-client.cfg.yaml` | Config for client SDK generation |
| Taskfile gen target | `Taskfile.yml` | `gen:api SERVICE=<name>` target |
| Sample spec | `api/storage/v1/openapi.yaml` | Example CloudForge Storage API spec |
| Generated stubs | `services/storage/generated/` | Generated server interface and types |
| Generated client | `pkg/client/storage/` | Generated typed Go client |
| Pattern document | `docs/plan/api-first-pattern.md` | How to add a new service API |

**oapi-codegen server config template:**
```yaml
# api/<service>/v1/oapi-server.cfg.yaml
package: generated
generate:
  chi-server: true
  strict-server: true
  models: true
  embedded-spec: true
output: ../../../services/<service>/generated/server.gen.go
output-options:
  skip-prune: false
```

**oapi-codegen client config template:**
```yaml
# api/<service>/v1/oapi-client.cfg.yaml
package: <service>client
generate:
  client: true
  models: true
output: ../../../pkg/client/<service>/client.gen.go
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
- Standard error response schemas
- Request/response schemas with proper validation constraints

**Validation service stub `services/storage/`:**
- Wire the generated `StrictServerInterface` to a `chi.Router`
- Implement placeholder handler that returns `501 Not Implemented`
- Show that the server compiles and the router correctly routes to generated handlers

### Acceptance Criteria
- [ ] `task gen:api storage` generates valid, compilable Go code
- [ ] The generated server stub `StrictServerInterface` is wired to a chi router in `services/storage/`
- [ ] The generated client in `pkg/client/storage/` compiles and has typed methods matching the spec
- [ ] Adding a new endpoint to the spec and re-running `gen:api` updates the generated code correctly
- [ ] `docs/plan/api-first-pattern.md` documents the full workflow for a new engineer

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

## Spike 0.7 — OPA Embedded Policy Evaluation

### Context and Motivation
CF-IAM (Phase 1) will use OPA (Open Policy Agent) for authorization policy evaluation. Every API call to every CloudForge service will ultimately result in a call to `pkg/authz/checker.go`, which calls OPA to evaluate a policy.

The critical question: **can OPA embedded in a Go process evaluate CloudForge IAM policies fast enough?** If p99 authorization latency adds more than 5ms to every API call, it becomes a bottleneck.

Two deployment options exist:
1. **OPA embedded** — OPA compiled and run inside the CF-IAM Go process via `github.com/open-policy-agent/opa/v1/rego`. No network hop. Policies loaded from disk or memory.
2. **OPA daemon** — OPA runs as a separate Kubernetes pod. CF-IAM calls the OPA HTTP API (`POST /v1/data/...`). Network hop on every authz check.

This spike must produce benchmark data to make the decision.

### Questions This Spike Must Answer

| # | Question | Acceptable Answer |
|---|---|---|
| Q1 | What is the p99 policy evaluation latency for embedded OPA with a 50-policy bundle? | < 5ms |
| Q2 | What is the p99 policy evaluation latency for embedded OPA with a 500-policy bundle? | < 10ms |
| Q3 | What is memory overhead of embedded OPA in a Go process with a 50-policy bundle? | Document number |
| Q4 | Can a Rego policy module be compiled and loaded incrementally (add one policy without reloading all)? | Yes or No; if No, document the reload strategy |
| Q5 | Is the OPA embedded mode sufficient, or is the OPA daemon required? | Make a clear recommendation with justification |

### Spike Scope
Time-box: **2 days maximum.**

**What to build in `spikes/opa-embedded/`:**
1. A realistic CloudForge IAM Rego policy module covering at minimum:
   - `allow` rule for `iam:read`, `iam:write`, `storage:read`, `storage:write`
   - Tenant and project scoping (principal must be in the correct tenant to access resources)
   - Service account principals with scoped permissions
   - A `deny` rule that rejects cross-tenant access
2. A Go benchmark program (`bench_test.go`) that:
   - Loads the policy bundle into OPA embedded
   - Benchmarks `rego.New(...).Eval(ctx, rego.EvalInput(input))` for single and multi-policy evaluation
   - Tests at 1, 50, and 500 policy bundles (generate synthetic policies to reach target counts)
   - Prints p50/p95/p99 latency using `testing.B`

### Required Findings Document
After the spike, write `spikes/opa-embedded/FINDINGS.md` with:
- Benchmark results table (1, 50, 500 policies × embedded latency)
- Memory overhead measurement
- **Decision: embedded OPA vs OPA daemon** — with rationale
- **Initial Rego module structure for CloudForge IAM policies** — the policy file layout that CF-IAM Task 1.3 should implement
- Recommended bundle compilation and hot-reload strategy

### Output Files

```
spikes/opa-embedded/
├── policies/
│   ├── iam.rego
│   ├── storage.rego
│   └── common.rego
├── bench_test.go
├── go.mod
├── go.sum
├── README.md
└── FINDINGS.md
```

### Rego Policy Structure to Test
The Rego module should use this package structure (so CF-IAM Task 1.3 knows what to implement):

```rego
package cloudforge.iam.v1

import future.keywords.if
import future.keywords.in

# Principal types: user, service_account, ai_serving_endpoint, ai_training_job
default allow = false

allow if {
    principal := input.principal
    action    := input.action
    resource  := input.resource

    tenant_match(principal, resource)
    policy_allows(principal, action, resource)
}

tenant_match(principal, resource) if {
    principal.tenant == resource.tenant
}
```

### Spike Fails If
- p99 embedded evaluation latency exceeds 10ms for a 50-policy bundle
- OPA cannot load policy bundles incrementally (every policy change requires full restart)

### Go Libraries for This Spike
```
github.com/open-policy-agent/opa/v1/rego
```

---

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

# [IN PROGRESS...........]

# Main documents to read:

[Architecture proposal](docs/1-cloud-forge-architecture-proposal.v0.1.md)
[Implementation plan](docs/2-cloud-forge-implementation-plan.v0.1.md)
[UPGRADE - CF VPC](docs/3-Introduce-CF-VPC.md)

# Status

The code is not doing anything useful yet. The project is still in the early spike phase, where we are exploring different infrastructure challenges and evaluating the right open-source technologies to solve them.

At this stage, the only principle we are strictly following is that everything must be based on open-source solutions. The goal is not to build quickly, but to make the right architectural and technology choices before moving into a more concrete implementation.

# CloudForge

CloudForge is an open-source, self-hosted cloud platform that gives SME engineering teams the infrastructure primitives they need to build and run modern applications — including AI-powered workloads — without depending on a hyperscaler.

It provides identity, secrets management, API gateway, object storage, managed databases, eventing, serverless functions, and AI model serving, all through a consistent API and CLI. When the platform is deployed, it is already AI-capable: model serving, vector search, and inference observability are built into the standard layers, not bolted on later.

## What CloudForge provides

| Layer | Capability |
|---|---|
| **Identity & Auth** | Keycloak-backed IAM, OPA policy engine, API keys for inference endpoints |
| **Secrets & Config** | OpenBao-backed secret management with Kubernetes injection |
| **Tenancy** | Multi-tenant resource hierarchy with quotas (including GPU quotas) |
| **API Gateway** | Apache APISIX with route management, JWT auth, rate limiting, streaming support |
| **Storage** | MinIO S3-compatible object storage with model artifact conventions |
| **Databases** | CloudNativePG (PostgreSQL + pgvector) and ScyllaDB, provisioned via API |
| **Eventing** | NATS JetStream with content-based routing rules and AI workflow event patterns |
| **Functions** | Knative scale-to-zero serverless functions with event and cron triggers |
| **AI Serving** | KServe + vLLM (GPU) / Ollama (CPU), OpenAI-compatible inference API |
| **Observability** | OpenTelemetry, Prometheus, Grafana, OpenSearch — with GPU and token-usage metrics |

## Requirements

| Tool | Min version | Purpose |
|---|---|---|
| Go | 1.26+ | Primary language |
| Docker Desktop (or Colima) | 24.x | Container runtime for local cluster and integration tests |
| k3d | 5.7+ | Kubernetes-in-Docker for local dev cluster |
| kubectl | 1.29+ | Cluster management |
| Helm | 3.14+ | Kubernetes package manager |
| Task | 3.x | Task runner (`Taskfile.yml`) |
| golangci-lint | 2.x | Go linter (matches CI) |

On macOS with Homebrew, `make tools-check` installs any missing tools automatically.

## Testing prerequisites

The project uses two testing tiers:

**Unit tests** — no external dependencies; run entirely in-process:

```bash
# Install all Go test dependencies (one-time, after cloning)
go mod download

# Optional: install the mock generator (only needed when adding new mocks)
go install go.uber.org/mock/mockgen@latest
```

**Integration tests** — spin up real services in Docker via [testcontainers-go](https://testcontainers.com/):

```bash
# Docker must be running
docker info

# The following images are pulled automatically on first run:
#   postgres:16-alpine   (internal/testutil.NewPostgresContainer)
#   nats:2-alpine        (internal/testutil.NewNATSContainer)
#   minio/minio          (internal/testutil.NewMinIOContainer)
#   openbao/openbao      (internal/testutil.NewOpenBaoContainer)
#   openpolicyagent/opa  (internal/testutil.NewOPAContainer)
make test-integration
```

> Integration tests are tagged `//go:build integration` and are skipped by `make test-unit`.

## Formatting and linting

The project enforces consistent formatting and zero linter warnings on every pull request. All rules mirror the CI configuration in `.github/workflows/ci.yml` and are configured in `.golangci.yml`.

### Commands

| Command | What it does |
|---|---|
| `make fmt` | Format all Go files with `gofmt` and `goimports` |
| `make lint` | Run `golangci-lint` with the project config (same as CI) |
| `make lint-fix` | Run `golangci-lint --fix` to auto-fix fixable issues |
| `make vet` | Run `go vet` only |
| `make check` | Run `fmt` + `vet` + `lint` in sequence |

```bash
# Quick check before pushing
make check

# Auto-fix what golangci-lint can fix, then review the rest
make lint-fix
make lint

# Format only
make fmt
```

### Linters enabled (`.golangci.yml`)

The notable linters active on this project:

| Linter | What it catches |
|---|---|
| `govet` | Suspicious constructs including struct field alignment |
| `gocritic` | Code style issues (octal literals, huge value parameters, …) |
| `revive` | Exported symbol documentation, unused parameters |
| `gofmt` / `goimports` | Formatting and import ordering |
| `gosec` | Security anti-patterns (file inclusion, path traversal) |
| `noctx` | HTTP requests created without a `context.Context` |
| `contextcheck` | Context propagation in goroutines and closures |
| `exhaustive` | Missing cases in `switch` statements over enums |

Run `golangci-lint linters` to see the full list.

### Pre-commit hooks

The project ships a `.pre-commit-config.yaml` that runs `gofmt` and `golangci-lint` automatically before every commit. To enable:

```bash
# Install the pre-commit framework (one-time)
pip install pre-commit       # or: brew install pre-commit

# Install the hooks into this repo (one-time per clone)
pre-commit install

# Run manually against all files
pre-commit run --all-files
```

Once installed, `git commit` will fail fast if there are format or lint errors, preventing them from ever reaching CI.

## Local development setup

```bash
# 1. Clone the repository
git clone git@github.com:jtomasevic/cloud-forge.git
cd cloud-forge

# 2. Verify and install required tools (macOS: installs missing tools via Homebrew)
make tools-check

# 3. Start the local k3d cluster and bootstrap the environment
make dev-up

# 4. Point kubectl at the dev cluster
export KUBECONFIG=$(pwd)/.dev/kubeconfig

# 5. Verify all platform namespaces are present
kubectl get ns
```

`make dev-up` creates a single-node Kubernetes cluster (k3d), installs Cilium as CNI, applies all platform namespaces and network policies, deploys cert-manager, ScyllaDB, and OpenBao — all in one command.

At the end of `make dev-up` you will see a summary box:

```
╔══════════════════════════════════════════════════════════════════════╗
║  CloudForge dev cluster is ready                                     ║
║                                                                      ║
║  Services:                                                           ║
║    Cilium CNI     + Hubble Relay  →  make cilium-status             ║
║    ScyllaDB       (cf-data)       →  make scylladb-status           ║
║    OpenBao        (cf-security)   →  make openbao-status            ║
║                                                                      ║
║  ⚠  OpenBao is running in DEV MODE:                                 ║
║       • In-memory storage  (secrets lost on pod restart)            ║
║       • Root token         (not Kubernetes auth)                    ║
║       • No TLS             (plaintext HTTP within cluster)          ║
║     This is intentional for local development. Production uses      ║
║     auto-unseal, persistent storage, mTLS, and Kubernetes auth.     ║
║                                                                      ║
║  Quick access:                                                        ║
║    make openbao-port-forward   →  http://localhost:8200              ║
║    token: dev-root-token                                             ║
╚══════════════════════════════════════════════════════════════════════╝
```

### OpenBao in the dev cluster (DEV MODE)

OpenBao runs in `cf-security` in **dev mode** to give every developer a real secrets-management API that mirrors the production service address, without requiring a production-grade setup.

| Feature | Dev cluster | Production |
|---------|-------------|------------|
| Real OpenBao API at the same cluster DNS | ✅ | ✅ |
| KV v2 path structure (same as prod) | ✅ | ✅ |
| CiliumNetworkPolicy (cf-system → 8200 only) | ✅ | ✅ |
| Persistent storage | ❌ In-memory | ✅ Raft |
| Auth method | ❌ Root token | ✅ Kubernetes auth |
| TLS | ❌ Plain HTTP | ✅ mTLS |
| High availability | ❌ Single pod | ✅ 3-node HA |

```bash
# Forward OpenBao API to your machine (keep this terminal open)
make openbao-port-forward

# In another terminal:
export VAULT_ADDR=http://localhost:8200
export VAULT_TOKEN=dev-root-token
vault kv put secret/cf/tenants/acme/kubeconfig value=test
vault kv get secret/cf/tenants/acme/kubeconfig

# Check OpenBao pod status and CNP:
make openbao-status
```

See [internal/provisioner/README.md](internal/provisioner/README.md) for the provisioner API and how kubeconfigs are stored and retrieved.

## Common commands

```bash
make dev-up             # Start local cluster (Cilium + ScyllaDB + OpenBao)
make dev-down           # Stop and delete cluster
make dev-reset          # Full reset (destroy + recreate)
make dev-status         # Show cluster and pod status

make openbao-status         # OpenBao pod, health, CNP
make openbao-port-forward   # Forward OpenBao API → localhost:8200

make build              # Build all service binaries to ./bin/
make test-unit          # Run unit tests (no Docker required)
make test-integration   # Run integration tests (requires Docker)
make test-coverage      # Run unit tests with HTML coverage report

make fmt                # Format all Go files
make lint               # Run golangci-lint (same config as CI)
make lint-fix           # Auto-fix lint issues where possible
make vet                # Run go vet
make check              # fmt + vet + lint in sequence

make gen-api SERVICE=storage   # Regenerate OpenAPI stubs for a service
make gen-all                   # Regenerate all service stubs

make image-build SERVICE=cf-iam   # Build container image locally
make image-push  SERVICE=cf-iam   # Push to ghcr.io/jtomasevic/cloud-forge
```

Run `make help` for the full list.

## Project structure

```
cloud-forge/
├── cmd/            # Service and CLI entrypoints (cf, cf-iam, cf-secrets, ...)
├── internal/       # Shared internal libraries (logging, tracing, metrics, ...)
├── pkg/            # Shared client packages (keycloak, openbao, minio, ...)
├── services/       # Business logic per service
├── controllers/    # Kubernetes controller reconcilers
├── api/            # OpenAPI 3.1 specs per service
├── deploy/         # Helm charts, Kustomize manifests, Dockerfiles, k3d config
├── spikes/         # Time-boxed prototypes (NATS, OPA, Knative, GPU/vLLM)
├── examples/       # Runnable consumer examples (RAG, event-driven inference, ...)
├── tests/e2e/      # End-to-end integration test suite
└── docs/           # Architecture, implementation plan, local dev guide
```

## Documentation

- [Architecture Proposal](docs/1-cloud-forge-architecture-proposal.v0.1.md)
- [Implementation Plan](docs/2-cloud-forge-implementation-plan.v0.1.md)
- [Local Development Guide](docs/local-dev.md)
- [Image Tagging Strategy](docs/plan/image-tagging.md)

## CI/CD

Every pull request runs lint → unit tests → integration tests → build (all services) via GitHub Actions. Merges to `main` and semver tags publish container images to `ghcr.io/jtomasevic/cloud-forge/<service>`.

## License

[Apache 2.0](LICENSE)

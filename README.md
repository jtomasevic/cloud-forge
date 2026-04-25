# [IN PROGRESS...........]

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

| Tool | Min version |
|---|---|
| Go | 1.26+ |
| Docker Desktop (or Colima) | 24.x |
| k3d | 5.7+ |
| kubectl | 1.29+ |
| Helm | 3.14+ |
| Task | 3.x |

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

`make dev-up` creates a 3-node Kubernetes cluster (k3d), applies all platform namespaces, generates a self-signed TLS certificate for `*.cloudforge.local`, and stores an initial admin password — all in one command.

See [docs/local-dev.md](docs/local-dev.md) for the full setup guide including troubleshooting, GPU/AI development notes, and day-to-day commands.

## Common commands

```bash
make dev-up             # Start local cluster
make dev-down           # Stop and delete cluster
make dev-reset          # Full reset (destroy + recreate)
make dev-status         # Show cluster and pod status

make build              # Build all service binaries to ./bin/
make test-unit          # Run unit tests
make test-integration   # Run integration tests (requires Docker)
make lint               # Run golangci-lint
make check              # fmt + vet + lint

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

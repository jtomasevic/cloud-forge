# CloudForge: Implementation Plan
## Execution-Oriented Delivery Plan — v1.0

**Status:** Engineering Draft  
**Language:** Go (primary), with noted exceptions  
**Supersedes:** `Plan.md` (v0.1)  
**Prerequisite:** Read `CloudForge-Architecture-Proposal.md` before this document  
**Audience:** Engineering teams, tech leads, project management

---

## Table of Contents

1. [Implementation Strategy](#1-implementation-strategy)
2. [Recommended Workstreams](#2-recommended-workstreams)
3. [Phased Delivery Plan](#3-phased-delivery-plan)
4. [Detailed Task List by Phase](#4-detailed-task-list-by-phase)
5. [AI Platform Capabilities and Consumer Use Cases](#5-ai-platform-capabilities-and-consumer-use-cases)
6. [OSS Integration Plan](#6-oss-integration-plan)
7. [Recommended Go Frameworks, Libraries, and SDKs](#7-recommended-go-frameworks-libraries-and-sdks)
8. [Dependency and Sequencing Map](#8-dependency-and-sequencing-map)
9. [MVP vs Later Phases](#9-mvp-vs-later-phases)
10. [Risks, Spikes, and Validation Checkpoints](#10-risks-spikes-and-validation-checkpoints)
11. [Final Recommended Execution Order](#11-final-recommended-execution-order)

---

## 1. Implementation Strategy

### Guiding Principles for Execution

**Build the backbone before the services.** CF-IAM, CF-ResourceController, and CF-SecretsConfig are not just services — they are the backbone that every other service depends on for authorization, tenant context, and credentials. Any service built before these exist will require a retrofit that is always more expensive than doing it right first.

**Spike before you commit on integration-heavy components.** CF-EventRouter (routing rules engine on NATS JetStream), CF-FunctionTrigger (Knative bridge), and the OPA policy evaluation integration each carry real technical risk. Before full implementation, each should be validated via a focused prototype that proves the core mechanism works.

**Generate API clients and server stubs from OpenAPI specs.** Every CloudForge service exposes a versioned REST API. Define the API contract in OpenAPI 3.1 first, then generate server stubs and client SDKs using `oapi-codegen`. This decouples API design from implementation and enables parallel development of consumer and provider.

**Every service is observable from day one.** Platform services must not ship without OpenTelemetry instrumentation, structured logging, and Prometheus metrics. Observability is a build requirement, not a phase two item. The observability infrastructure (collector, Prometheus, Grafana) is stood up in Phase 1 for the platform's own use before it is offered to tenants.

**Monorepo, single Go module.** All custom CloudForge services live in one repository with a single `go.work` workspace or a root `go.mod`. This prevents package version drift across services and makes shared library changes visible immediately.

**OSS components are deployed, not modified.** CloudForge integrates with OSS projects through their official APIs, CRDs, and client libraries. No forks. No patches to upstream code. When upstream behavior is insufficient, the gap is closed with an adapter layer in CloudForge code, never by modifying the upstream project.

**AI capabilities are platform infrastructure, not a platform product.** This is the most important framing principle. CloudForge does not build an AI application. It builds the infrastructure that allows consumers to build their own AI applications, agents, workflows, and models. This means:

- AI inference serving (vLLM, KServe) is a **compute workload type**, deployed in the same phase as the rest of the compute layer — not a late feature.
- Vector search capability (pgvector) is a **database capability**, deployed when databases are deployed — not a separate service.
- Model artifact storage is an **object storage concern**, addressed when MinIO and the Storage API are built.
- AI workload telemetry (GPU metrics, token counts, model latency) is an **observability concern**, built into CF-Observability alongside standard application telemetry.
- AI workload identity (model serving endpoints, training jobs) is an **IAM concern**, designed into CF-IAM from the first version.
- Platform-native eventing (NATS JetStream) is the natural backbone for AI workflow orchestration — no separate AI event infrastructure is needed.

The result is that when the compute, storage, database, eventing, and observability layers are complete, the platform is already AI-capable. No separate AI phase is required to enable it.

---

## 2. Recommended Workstreams

Each workstream maps to a team or sub-team. Note that AI capabilities are distributed across workstreams rather than isolated in a dedicated AI workstream. The AI-enabling column identifies which tasks in each workstream unlock consumer AI use cases.

| # | Workstream | Primary Deliverables | AI-Enabling Contributions |
|---|---|---|---|
| WS-1 | **Foundation & Infrastructure** | Repo structure, CI/CD, local dev cluster, shared Go libraries | GPU node labeling, GPU scheduling validation spike |
| WS-2 | **Identity & Security (CF-IAM)** | Keycloak deployment, OPA integration, CF-IAM service, auth middleware | AI workload identity patterns; API key model for inference endpoints |
| WS-3 | **Secrets & Config (CF-SecretsConfig)** | OpenBao deployment, CF-SecretsConfig service, secret injection model | HuggingFace tokens, model API keys, training dataset credentials |
| WS-4 | **Control Plane & Tenancy (CF-ResourceController)** | Tenant/project model, quota management, resource inventory | GPU resource quota type; AI serving deployment quota |
| WS-5 | **API Gateway (CF-GatewayControl)** | APISIX deployment, CF-GatewayControl service, platform API routing, CloudForge CLI | Consumer AI endpoint exposure; rate limiting for inference APIs |
| WS-6 | **Eventing (CF-EventRouter)** | NATS JetStream deployment, routing rules engine, DLQ, retry, CloudForge Events API | AI workflow orchestration events; training job lifecycle events; inference completion events |
| WS-7 | **Compute Layer (CF-FunctionTrigger + CF-AIRuntime)** | Knative deployment, trigger bridge, function packaging; vLLM/KServe deployment, CF-AIRuntime service, model registry | Core AI serving infrastructure; consumer model deployment API |
| WS-8 | **Storage** | MinIO deployment, CloudForge Storage API, bucket provisioning | Model artifact storage; training dataset storage; ONNX/checkpoint exports |
| WS-9 | **Databases (CF-DBController)** | CloudNativePG + ScyllaDB deployment, CF-DBController, provisioning API | pgvector extension on PostgreSQL for vector similarity search; embedding store |
| WS-10 | **Networking & Ingress** | Cilium, Contour, cert-manager, tenant ingress isolation | GPU node network policies; high-bandwidth networking for model serving |
| WS-11 | **Observability (CF-Observability)** | OTel Collector, Prometheus, Grafana, OpenSearch, CF-Observability service | GPU utilization metrics; token usage per tenant; model latency histograms; inference request traces |
| WS-12 | **Deployment & Operations** | Helm charts, bootstrap CLI (`cf-install`), platform operator, upgrade tooling | GPU node prerequisite detection in `cf-install preflight` |

---

## 3. Phased Delivery Plan

### Phase 0 — Foundation and Spikes (Weeks 1–4)

**Why it comes first:** Nothing can be built without a repository, a working local development environment, a shared Go module structure, shared libraries, and a confirmed CI pipeline. The spikes de-risk the highest-uncertainty integration points before full implementation begins.

**What it unlocks:** All subsequent phases. Engineers can write, test, and deploy code. Spikes generate confidence or early course corrections on the most critical integration points — including AI runtime and GPU scheduling, which are validated here before the compute layer is built.

**Dependencies:** None.

**AI-enabling work in this phase:** GPU scheduling spike validates that the cluster can schedule and serve GPU workloads before the compute phase commits to a specific AI runtime deployment model.

---

### Phase 1 — Identity, Secrets, and Tenancy Core (Weeks 3–10)

**Why it comes first (among services):** CF-IAM and CF-SecretsConfig are the backbone of every other service. Any service that cannot answer "who is calling and do they have permission?" is incomplete. Building them first means every subsequent service — including AI serving endpoints — is built with auth baked in from the start.

**What it unlocks:** A working identity plane. Tokens can be issued, tenants and projects created, service-to-service calls authenticated. Critically, the IAM model designed here includes **AI workload identity patterns**: model serving deployments, training jobs, and inference pipelines all receive platform identities with scoped permissions, the same way database connections and function invocations do.

**Dependencies:** Phase 0.

**AI-enabling work in this phase:** CF-IAM is designed to issue API keys for AI inference endpoints. CF-SecretsConfig is designed to store AI-specific secrets (HuggingFace access tokens, remote model provider API keys, training dataset credentials) using the same secret management model as all other platform secrets.

---

### Phase 2 — API Gateway and Platform API Surface (Weeks 8–14)

**Why it comes here:** The platform needs a single consistent entry point before any tenant-facing APIs are built. APISIX and CF-GatewayControl establish the route publication model that all services — including consumer-deployed AI inference endpoints — will use to expose themselves.

**What it unlocks:** A stable API entry point. CF-GatewayControl's route model is designed to support AI-specific traffic patterns (streaming responses from inference APIs, large request payloads for vision/multimodal models, rate limiting per-token rather than per-request). Consumers can expose their own AI endpoints through this same mechanism.

**Dependencies:** Phase 1 (CF-IAM for JWT validation at gateway).

**AI-enabling work in this phase:** CF-GatewayControl route model includes first-class support for streaming HTTP responses (Server-Sent Events, chunked transfer for LLM token streaming). Rate limiting plugin configuration supports token-budget-based limiting as an alternative to request-count limiting.

---

### Phase 3 — Storage Layer (Weeks 10–16)

**Why it comes here:** Object storage is foundational to every other service and to every AI workload. It must be available before compute, eventing, or AI capabilities are built.

**What it unlocks:** Tenant-provisioned storage buckets. Database backup storage. Function artifact storage. And critically — **model artifact storage**: consumers can store model weights, training datasets, ONNX exports, adapter checkpoints, and evaluation artifacts in MinIO from the moment the Storage API ships. The model registry backing store is MinIO; when the AI serving infrastructure lands in Phase 6, it reads model weights directly from MinIO.

**Dependencies:** Phase 1 (CF-IAM, CF-SecretsConfig).

**AI-enabling work in this phase:** The CloudForge Storage API provisioning model explicitly supports `cf:purpose=model-artifacts` and `cf:purpose=training-data` bucket tags in the resource model. These are no different from other buckets technically, but the tagging model and quota type allow the platform to track and enforce AI-workload storage quotas separately from general-purpose storage.

---

### Phase 4 — Database Layer (Weeks 12–18)

**Why it comes here:** Databases serve both general-purpose application workloads and AI-specific workloads. PostgreSQL with the pgvector extension is a first-class vector store, suitable for the majority of RAG (retrieval-augmented generation) use cases without requiring a separate vector database. This capability is deployed as part of the standard CloudNativePG setup — not as a separate AI feature.

**What it unlocks:** Managed PostgreSQL and NoSQL (ScyllaDB). pgvector is available on every PostgreSQL instance by default, meaning consumers can store and query embeddings using standard SQL the moment they provision a database. This is sufficient for most SME-scale RAG, semantic search, and recommendation workloads without additional infrastructure.

**Dependencies:** Phase 1 (CF-IAM, CF-SecretsConfig). Phase 3 (MinIO for backups).

**AI-enabling work in this phase:** CloudNativePG clusters are provisioned with the pgvector extension pre-installed by default. The CF-DBController database creation API accepts a `pgvector: true` parameter (on by default) that ensures the extension is enabled on provisioning. Consumers can immediately create vector columns, build HNSW indexes, and run cosine similarity queries — no additional setup required. This eliminates the need for a dedicated vector store service for the vast majority of consumer AI use cases.

---

### Phase 5 — Eventing Layer (Weeks 14–22)

**Why it comes here:** Eventing is foundational to functions and event-driven architectures. It is also the natural backbone for AI workflow orchestration. NATS JetStream provides the messaging semantics that AI pipelines need: durable delivery, fan-out routing, and the ability to chain processing steps — without requiring a separate AI workflow framework.

**What it unlocks:** A working CloudForge Events service. Training job lifecycle events (job submitted → started → completed/failed). Inference pipeline orchestration (request received → model invoked → result stored → downstream notified). Event-triggered AI workloads (new document uploaded to storage → embedding job triggered → vector index updated). All of these are native NATS JetStream consumers and producers — no AI-specific event infrastructure is needed.

**Dependencies:** Phase 1 (CF-IAM). Phase 0 NATS routing spike.

**AI-enabling work in this phase:** The CF-EventRouter routing rule schema includes first-class event patterns for AI workload events: `cf.ai.inference.completed`, `cf.ai.training.job.finished`, `cf.ai.model.deployed`. These are standard CloudEvents published by the AI serving infrastructure in Phase 6 and by consumer workloads. Consumers can wire routing rules against these events the same way they wire rules against storage events or database change events.

---

### Phase 6 — Compute Layer: Functions and AI Serving (Weeks 20–28)

**Why this phase combines functions and AI serving:** Knative Serving and the AI inference runtime (vLLM, KServe) are two compute workload types that belong in the same platform layer. Both are Kubernetes-native scaled workloads. Both are behind the API gateway. Both use platform IAM for access control. Both emit telemetry to the observability layer. Separating them into different phases would leave the platform with a compute gap: a phase where you can run event-driven functions but cannot run the inference workload that those functions need to call.

**What it unlocks:** The complete compute layer of the platform. Consumers can deploy:
- **Serverless functions** via Knative (event-triggered, HTTP-triggered, cron-triggered)
- **AI inference endpoints** via KServe/vLLM (deploy any open model, get an OpenAI-compatible API back)

These two capabilities are peer workload types. A consumer's order-processing function calling a consumer's deployed sentiment classifier is a first-class platform use case that is enabled the moment this phase completes.

**Dependencies:** Phase 5 (CF-EventRouter). Phase 3 (Storage for function artifacts and model weights). Phase 1 (CF-IAM for invocation context and API key enforcement). Phase 0 GPU spike (for AI serving deployment).

---

### Phase 7 — Observability Layer (Weeks 22–30)

**Why it comes here:** The minimal observability stack (OTel Collector, Prometheus, Grafana) is bootstrapped in Phase 1 for platform-internal use. This phase completes it: OpenSearch is deployed, CF-Observability is built, and the tenant-facing telemetry layer is ready. AI-specific telemetry is built into this layer from the start — not added later.

**What it unlocks:** Full platform and tenant telemetry. AI workload telemetry is available immediately: GPU utilization, inference request latency, token throughput per tenant, model serving error rates. Consumers can query their AI workload logs and traces through the same CloudForge Logs API they use for their standard application workloads.

**Dependencies:** Phase 1 and Phase 2 (platform services to instrument). Phase 6 (AI serving runtime emitting GPU and inference metrics to scrape).

**AI-enabling work in this phase:** CF-Observability ingests and exposes AI-specific telemetry as a native concern: vLLM's Prometheus metrics family (`vllm:*`) is scraped and available in Grafana; token usage per tenant/project is tracked and queryable via the CloudForge Usage API; inference request traces (model name, prompt token count, completion token count, latency, status) are written to OpenSearch and queryable through CF-Observability's structured query API.

---

### Phase 8 — MVP Hardening, Deployment, and Release (Weeks 26–30)

**Why it comes here:** After Phase 7 completes, the platform has all MVP capabilities: identity, secrets, tenancy, API gateway, storage, databases (including vector search), eventing, compute (functions + AI serving), and observability. This phase focuses on integration testing, the Helm chart, the bootstrap CLI, documentation, and the first release.

**What it unlocks:** A deployable, validated, documented platform that a real SME engineering team can install and use to build applications — including AI-powered applications — from day one.

**Dependencies:** All previous phases.

---

### Phase 9 — Hardening, Advanced Capabilities, and Managed Offering Readiness (Weeks 30–48)

Phase 9 is not a single milestone but a continuous hardening and capability-expansion track that runs after the MVP release. The key expansions in this phase are:

- **IAM hardening:** Resource-based policies, permission boundaries, cross-project role assumption
- **Eventing hardening:** DLQ with retry policy, ScyllaDB CDC → NATS bridge for change data capture
- **Database expansion:** MySQL support, automated point-in-time recovery
- **Data pipeline service:** Apache Airflow adapter (CF-DataPipeline) for consumer ETL and training data processing workflows
- **Advanced AI capabilities:** GPU MIG partitioning for multi-tenant inference isolation; model fine-tuning pipeline (Kubernetes GPU Job management via the compute API); distributed training job submission
- **Consumer AI reference materials:** Starter SDK examples, LangGraph integration guide, reference architectures for RAG, fine-tuning, and agent workloads on the platform
- **Managed offering readiness:** Billing hooks, multi-cluster architecture, NOC tooling

---

## 4. Detailed Task List by Phase

---

### Phase 0: Foundation and Spikes

#### Task 0.1 — Monorepo and Go Module Setup

- **Purpose:** Establish the canonical repository structure and Go module configuration before any code is written.
- **Scope:** Create the monorepo layout, configure `go.work` (Go workspace), set up root `go.mod`, define module boundaries.
- **Key deliverables:**
  - Repository structure (see Section 7 for layout)
  - `go.work` with modules for `cmd/*`, `internal/`, `pkg/`, `services/`
  - Linter configuration (`golangci-lint` with project-standard rules)
  - Pre-commit hooks (lint, vet, test on staged files)
- **Dependencies:** None
- **Type:** Infrastructure + Go code structure

#### Task 0.2 — CI/CD Pipeline

- **Purpose:** Automated build, lint, test, and container image build for all CloudForge services.
- **Scope:** GitHub Actions workflows for: lint → unit test → build → image push. Per-service image build using multi-stage Dockerfiles.
- **Key deliverables:**
  - CI workflow files
  - Multi-stage Dockerfiles for each service using `gcr.io/distroless/static` base image
  - Container registry configuration (GitHub Container Registry `ghcr.io/cloud-forge/<service>`)
  - Version tagging strategy (semver + git SHA for pre-release)
- **Dependencies:** Task 0.1
- **Type:** Infrastructure + operational tooling
- **Go tools:** `github.com/google/ko` recommended for building Go container images without Dockerfiles.

#### Task 0.3 — Local Development Cluster

- **Purpose:** Every engineer must be able to run the full (or partial) platform locally.
- **Scope:** `k3d` configuration for spinning up a local Kubernetes cluster with preconfigured namespaces, storage class, and load balancer simulation. `Taskfile` with commands: `dev:up`, `dev:down`, `dev:reset`, `deploy:component <name>`.
- **Key deliverables:**
  - `k3d` cluster config file
  - `Taskfile.yml` with dev lifecycle commands
  - Base namespace manifests (`cf-system`, `cf-identity`, `cf-data`, `cf-compute`, `cf-tenant-*`)
  - Development secret bootstrapping script (generates self-signed certs, initial admin credentials)
  - GPU simulation note: local dev does not require a GPU; CPU-mode Ollama is used in the local cluster as a substitute for vLLM
- **Dependencies:** Tasks 0.1, 0.2
- **Type:** Infrastructure + operational tooling

#### Task 0.4 — Shared Internal Libraries

- **Purpose:** Establish shared Go libraries that all CloudForge services use.
- **Scope:** `internal/` packages shared across all services.
- **Key deliverables:**
  - `internal/logging`: structured logging using `log/slog` with OTel log bridge
  - `internal/tracing`: OpenTelemetry tracer initialization (OTLP exporter, resource attributes)
  - `internal/metrics`: Prometheus registry setup, standard HTTP middleware metrics
  - `internal/config`: Viper-based config loading (YAML + environment variable override + Kubernetes secret mounting)
  - `internal/errors`: Platform error types with HTTP status mapping
  - `internal/middleware`: HTTP middleware chain (request ID, structured access logging, OTel span, panic recovery)
  - `internal/testutil`: testcontainers helpers for NATS, PostgreSQL, OpenBao, MinIO
- **Dependencies:** Task 0.1
- **Type:** Custom Go code

#### Task 0.5 — OpenAPI-First API Scaffolding

- **Purpose:** Establish the pattern for API-first development.
- **Scope:** Configure `oapi-codegen` for server stub and client SDK generation. Validate with one sample service.
- **Key deliverables:**
  - `oapi-codegen` configuration files per service (in `api/` directory)
  - Generator Taskfile targets: `gen:api <service>`
  - Example: CloudForge Storage API spec with generated server stubs and client
- **Dependencies:** Task 0.1
- **Type:** Infrastructure + Go tooling

#### Task 0.6 — Spike: NATS JetStream Multi-Tenant Routing

- **Purpose:** Validate that NATS JetStream accounts provide the tenant isolation and routing semantics needed by CF-EventRouter.
- **Scope:** Prototype: two NATS accounts (tenants), per-account streams, CloudEvents payloads, content-based routing rule in Go, dispatch to two targets.
- **Key deliverables:**
  - Spike code in `spikes/nats-routing/`
  - Written findings: confirmed routing semantics, throughput, gaps that CF-EventRouter must close
  - Decision: dynamic NATS account provisioning model (CRDs vs config API)
- **Dependencies:** Task 0.3
- **Type:** Spike / prototype

#### Task 0.7 — Spike: OPA Embedded Policy Evaluation

- **Purpose:** Validate OPA embedded mode performance for CF-IAM authorization checks.
- **Scope:** Prototype: sample CloudForge IAM policy in Rego, compiled and evaluated in a Go process, benchmarked at 100 and 1,000 policy bundles.
- **Key deliverables:**
  - Spike code in `spikes/opa-embedded/`
  - Benchmark results: evaluation latency at various policy set sizes
  - Decision: embedded OPA vs OPA daemon for runtime use
  - Initial Rego module structure for CloudForge IAM policies
- **Dependencies:** Task 0.1
- **Type:** Spike / prototype

#### Task 0.8 — Spike: Knative Scale-to-Zero Cold Start

- **Purpose:** Measure Knative cold start latency in the local cluster to determine minimum-replica guidance.
- **Scope:** Deploy Knative Serving on k3d. Measure cold start for simple, medium, and heavy function variants after scale-to-zero.
- **Key deliverables:**
  - Spike code and results in `spikes/knative-coldstart/`
  - Recommended minimum replica settings
  - Confirmed: direct HTTP invocation from Go works
- **Dependencies:** Task 0.3
- **Type:** Spike / prototype

#### Task 0.9 — Spike: GPU Scheduling and vLLM Deployment Validation

- **Purpose:** Validate GPU-accelerated workload scheduling on Kubernetes and confirm that vLLM can be deployed and queried before the full compute layer is designed around it.
- **Scope:** This spike does not require production GPU hardware; use a GPU node in a cloud environment or a workstation with an NVIDIA card. Validate: NVIDIA device plugin installation and GPU resource scheduling in Kubernetes; KServe `ServingRuntime` CRD with a vLLM backend; a small model (Qwen2.5-1.5B or similar) deployed and serving the OpenAI `/v1/chat/completions` endpoint; a Go HTTP client calling the endpoint with streaming enabled. Separately, validate Ollama (CPU mode) as a drop-in substitute for local development when no GPU is available.
- **Key deliverables:**
  - Spike code in `spikes/ai-runtime/`
  - NVIDIA device plugin Helm values
  - KServe `ServingRuntime` manifest for vLLM
  - Go HTTP client for OpenAI-compatible streaming API (reused later in `pkg/inference/`)
  - Decision: KServe vs bare vLLM Deployment for Phase 6 implementation
  - Local dev substitution confirmed: Ollama on CPU can serve the same OpenAI-compatible API for development purposes
- **Dependencies:** Task 0.3 (for Ollama validation); GPU node access (can be cloud ephemeral node for the spike)
- **Type:** Spike / prototype

---

### Phase 1: Identity, Secrets, and Tenancy Core

#### Task 1.1 — Deploy Keycloak

- **Purpose:** Establish the identity provider.
- **Scope:** Deploy Keycloak via its Operator. Configure `cf-platform` realm, initial admin user, master OIDC client.
- **Key deliverables:**
  - Helm/Kustomize manifests for Keycloak
  - Bootstrap script: creates `cf-platform` realm, disables public client registration, sets token lifetimes
  - Keycloak backed by temporary embedded PostgreSQL (migrated to CloudNativePG in Phase 4)
  - Health checks and readiness probes configured
- **Dependencies:** Task 0.3
- **Type:** OSS integration + infrastructure

#### Task 1.2 — Deploy OPA

- **Purpose:** Establish the policy evaluation engine.
- **Scope:** Deploy OPA as a cluster-wide policy daemon. Configure bundle loading. Validate OPA API from Go.
- **Key deliverables:**
  - OPA Helm chart configuration
  - ConfigMap-based policy bundles for Phase 1 (migrated to MinIO-backed bundles in Phase 3)
  - OPA health check validated from Go
  - Initial Rego policy test harness (`opa test`)
- **Dependencies:** Task 1.1
- **Type:** OSS integration + infrastructure

#### Task 1.3 — Build CF-IAM Service (Core)

- **Purpose:** The central identity and authorization service. Everything depends on it.
- **Scope:** Principal management (users, service accounts), identity-based policy CRUD, Keycloak realm provisioning for new tenants, OPA bundle compilation and push, authorization check endpoint.

- **Key deliverables:**
  - OpenAPI spec: `api/iam/v1/openapi.yaml`
  - Service: `services/iam/`
  - Endpoints:
    - `POST /iam/v1/tenants/{tenant}/users`
    - `POST /iam/v1/tenants/{tenant}/service-accounts`
    - `POST /iam/v1/tenants/{tenant}/api-keys` — issue long-lived API keys for inference endpoint access
    - `PUT /iam/v1/tenants/{tenant}/policies/{name}`
    - `DELETE /iam/v1/tenants/{tenant}/policies/{name}`
    - `GET /iam/v1/tenants/{tenant}/policies`
    - `POST /iam/v1/authz/check` (internal gRPC endpoint)
  - Keycloak client wrapper: `pkg/keycloak/` (realm creation, user management, service account credentials)
  - OPA bundle builder: compiles CF IAM policies to Rego, pushes bundle to OPA
  - JWT validation middleware: `internal/middleware/jwt.go`
  - **AI workload identity design:** Policy model explicitly includes AI-typed principals: `cf:ai:serving-endpoint`, `cf:ai:training-job`. These are service account types with scoped default permissions. The API key model (above) issues bearer tokens usable against AI inference endpoints without requiring full OIDC flows — matching the pattern consumers expect for calling inference APIs.
  - Integration tests: real Keycloak + real OPA in testcontainers

- **Dependencies:** Tasks 1.1, 1.2, 0.4, 0.5, 0.7
- **Type:** Custom Go code + platform API

**Go libraries:**
- `github.com/coreos/go-oidc/v3` — OIDC token validation
- `golang.org/x/oauth2` — OAuth2 client credentials flow
- `github.com/open-policy-agent/opa/v1/rego` — embedded OPA for bundle compilation
- `github.com/go-chi/chi/v5` — HTTP router
- `github.com/jackc/pgx/v5` — PostgreSQL policy store

#### Task 1.4 — Deploy OpenBao

- **Purpose:** Establish the secrets backend.
- **Scope:** Deploy OpenBao. Configure Kubernetes auth, KV v2 engine, Transit engine.
- **Key deliverables:**
  - OpenBao Helm chart with auto-unseal
  - Kubernetes auth method configured
  - KV v2 mounts: `cf/secrets/`, `cf/config/`
  - Transit engine mount: `cf/transit/`
  - OpenBao policy for CF-SecretsConfig service account
  - Health validation from Go
- **Dependencies:** Task 0.3
- **Type:** OSS integration + infrastructure

#### Task 1.5 — Build CF-SecretsConfig Service

- **Purpose:** Tenant-aware secrets and configuration API backed by OpenBao.
- **Scope:** Tenant-scoped secret CRUD, parameter CRUD, versioning, IAM-authorized access, audit log emission.

- **Key deliverables:**
  - OpenAPI spec: `api/secrets/v1/openapi.yaml`
  - Service: `services/secrets/`
  - Endpoints:
    - `PUT /secrets/v1/{tenant}/{project}/{name}`
    - `GET /secrets/v1/{tenant}/{project}/{name}`
    - `DELETE /secrets/v1/{tenant}/{project}/{name}`
    - `GET /secrets/v1/{tenant}/{project}/{name}/versions`
    - `PUT /config/v1/{tenant}/{project}/{path}`
    - `GET /config/v1/{tenant}/{project}/{path}`
  - OpenBao client wrapper: `pkg/openbao/`
  - IAM integration: every request calls CF-IAM `authz/check`
  - Audit log emission via OTel
  - Kubernetes secret injection CRD model (completed in Phase 6 when controllers land)
  - **AI-specific secret types supported from v1:** `cf:secret-type=hf-token` (HuggingFace access token for private model download), `cf:secret-type=model-api-key` (external model provider API key). These are standard KV secrets with a type tag; the CF-AIRuntime service in Phase 6 will use this type tag to inject the correct secrets into model serving deployments automatically.
  - Integration tests: real OpenBao in testcontainers

- **Dependencies:** Tasks 1.3, 1.4
- **Type:** Custom Go code + platform API

**Go libraries:**
- `github.com/openbao/openbao/api/v2`

#### Task 1.6 — Build CF-ResourceController (Tenant and Project Model)

- **Purpose:** Top-level resource hierarchy: tenant, project, resource inventory, quotas.
- **Scope:** Tenant lifecycle, project lifecycle, resource quota model, resource inventory, resource identifier generation, tenant onboarding orchestration.

- **Key deliverables:**
  - OpenAPI spec: `api/resource/v1/openapi.yaml`
  - Service: `services/resource/`
  - Endpoints:
    - `POST /resource/v1/tenants`
    - `GET /resource/v1/tenants/{tenant}`
    - `POST /resource/v1/tenants/{tenant}/projects`
    - `GET /resource/v1/tenants/{tenant}/projects/{project}/resources`
    - `PUT /resource/v1/tenants/{tenant}/projects/{project}/quotas`
  - Provisioning state machine: `PENDING → PROVISIONING → READY → FAILED`
  - Quota model includes AI resource types from v1:
    - `ai.serving.deployments` — max concurrent model serving deployments per project
    - `ai.serving.gpu_millicores` — GPU compute quota
    - `ai.training.concurrent_jobs` — max concurrent training/fine-tuning jobs
    - `storage.model_artifacts_gb` — dedicated quota bucket for model artifact storage
  - Resource identifier library: `pkg/resource/id.go` (`cf://` URI parsing and construction)
  - Tenant onboarding: creates Keycloak realm, OpenBao namespace, NATS account
  - Integration tests: testcontainers

- **Dependencies:** Tasks 1.3, 1.5
- **Type:** Custom Go code + platform API

---

### Phase 2: API Gateway and Platform API Surface

#### Task 2.1 — Deploy Apache APISIX

- **Purpose:** The platform's single API entry point.
- **Scope:** Deploy APISIX. Configure JWT auth plugin globally. Configure route isolation between platform API and tenant-facing paths. Enable streaming response proxying.
- **Key deliverables:**
  - APISIX Helm values
  - JWT plugin configured against Keycloak JWKS endpoint
  - API key authentication plugin configured (for inference API keys issued by CF-IAM)
  - Rate limiting plugin configured at global level
  - **Streaming proxy configuration:** Enable chunked transfer encoding and SSE (Server-Sent Events) passthrough. This is required for LLM token streaming from inference endpoints. Validate that a chunked HTTP response proxied through APISIX reaches the client correctly.
  - Prometheus and OTel access log forwarding configured
- **Dependencies:** Task 0.3, Task 1.3
- **Type:** OSS integration + infrastructure

#### Task 2.2 — Build CF-GatewayControl Service

- **Purpose:** Manages APISIX configuration for platform and tenant route publication.
- **Scope:** Route CRUD, plugin configuration, upstream management, TLS certificate attachment via cert-manager.

- **Key deliverables:**
  - OpenAPI spec: `api/gateway/v1/openapi.yaml`
  - Service: `services/gateway/`
  - Endpoints:
    - `POST /gateway/v1/{tenant}/{project}/routes`
    - `PUT /gateway/v1/{tenant}/{project}/routes/{id}`
    - `DELETE /gateway/v1/{tenant}/{project}/routes/{id}`
    - `GET /gateway/v1/{tenant}/{project}/routes`
  - APISIX admin API client: `pkg/apisix/`
  - Tenant namespace enforcement on route paths
  - cert-manager integration for TLS
  - **AI endpoint route type:** The route model includes an `ai-proxy` route type that pre-configures: API key authentication (not JWT), token-budget rate limiting, request size limits for large prompts, streaming response passthrough, and usage event emission to CF-EventRouter on request completion. Consumers register their AI serving endpoints using this route type via the standard route API.
  - Integration tests

- **Dependencies:** Tasks 2.1, 1.3, 1.6
- **Type:** Custom Go code + platform API + adapter

#### Task 2.3 — Wire All CF Services Through APISIX

- **Purpose:** All CloudForge service APIs routed through APISIX for unified authentication and observability.
- **Scope:** Routes for CF-IAM, CF-SecretsConfig, CF-ResourceController. JWT validation on all routes. OTel trace header propagation.
- **Key deliverables:**
  - Route manifests for all existing CF services
  - Validated: CLI → APISIX → CF-IAM with valid JWT; unauthorized returns 401
  - OTel trace propagation through APISIX validated (`traceparent` forwarded)
- **Dependencies:** Tasks 2.1, 2.2, 1.3, 1.5, 1.6
- **Type:** Infrastructure + integration

#### Task 2.4 — CloudForge CLI Scaffolding (`cf`)

- **Purpose:** Primary developer tool for interacting with the platform.
- **Scope:** Scaffold `cf` CLI with Cobra. Implement login, context, tenant/project management, and resource listing. Generate typed API clients from OpenAPI specs.

- **Key deliverables:**
  - `cmd/cf/` — CLI entrypoint
  - `cf login` — OIDC device authorization flow
  - `cf context use/list/set`
  - `cf tenant create/list/get`
  - `cf project create/list/get`
  - `cf resource list --project <project>`
  - Generated API clients in `pkg/client/`
  - Shell completion scripts (bash, zsh, fish)

- **Dependencies:** Task 2.3
- **Type:** Custom Go code

---

### Phase 3: Storage Layer

#### Task 3.1 — Deploy MinIO

- **Purpose:** S3-compatible object storage backend.
- **Scope:** Deploy MinIO via MinIO Operator. Configure distributed mode. Enable server-side encryption via OpenBao Transit.
- **Key deliverables:**
  - MinIO Operator Helm chart
  - Distributed MinIO tenant with erasure coding
  - Platform-internal buckets pre-provisioned:
    - `cf-platform-backups` — database backups, observability archives
    - `cf-platform-artifacts` — function deployment packages
    - `cf-platform-models` — platform-managed model weights for AI serving
    - `cf-platform-opa-bundles` — OPA policy bundles (migrated from ConfigMap)
  - Server-side encryption via OpenBao Transit
  - MinIO Prometheus metrics scraping configured
- **Dependencies:** Tasks 0.3, 1.4 (OpenBao for encryption keys)
- **Type:** OSS integration + infrastructure

#### Task 3.2 — Build CloudForge Storage API

- **Purpose:** Tenant-facing object storage API with IAM-governed access.
- **Scope:** Bucket provisioning, IAM authorization on bucket/object access, pre-signed URLs, access events to event bus.

- **Key deliverables:**
  - OpenAPI spec: `api/storage/v1/openapi.yaml`
  - Service: `services/storage/`
  - Endpoints:
    - `POST /storage/v1/{tenant}/{project}/buckets`
    - `DELETE /storage/v1/{tenant}/{project}/buckets/{name}`
    - `POST /storage/v1/{tenant}/{project}/buckets/{name}/presigned`
    - `GET /storage/v1/{tenant}/{project}/buckets`
    - `POST /storage/v1/{tenant}/{project}/buckets/{name}/policy` — set bucket IAM policy (allows read access to AI serving workloads, etc.)
  - MinIO Go client wrapper: `pkg/minio/`
  - MinIO IAM policy generation from CloudForge IAM grants
  - `cf storage bucket create/list/presign`
  - **Model artifact storage guidance:** No separate "model registry" service is built in Phase 3. Model weights and datasets are stored in standard MinIO buckets with recommended naming conventions (`models/{name}/{version}/`, `datasets/{name}/{version}/`). The CF-AIRuntime service in Phase 6 reads from these buckets; the storage API built here is sufficient as the model artifact store without additional software.
  - Migrate OPA policy bundles from ConfigMap to MinIO `cf-platform-opa-bundles` bucket

- **Dependencies:** Tasks 3.1, 1.3, 1.6
- **Type:** Custom Go code + platform API (thin wrapper)

**Go libraries:**
- `github.com/minio/minio-go/v7`

---

### Phase 4: Database Layer

#### Task 4.1 — Deploy CloudNativePG Operator with pgvector

- **Purpose:** Kubernetes-native PostgreSQL lifecycle management with vector search capability pre-installed.
- **Scope:** Deploy CloudNativePG operator. Validate HA cluster creation, backup to MinIO, PgBouncer pooling. Critically: enable pgvector extension by default on all managed clusters.
- **Key deliverables:**
  - CloudNativePG Helm chart
  - Sample `Cluster` manifest with PgBouncer pooler and pgvector pre-installed
  - Backup validation: `ScheduledBackup` → MinIO `cf-platform-backups`
  - pgvector validation: create a table with a `vector(1536)` column, insert embeddings, run cosine similarity query, verify results
  - Prometheus metrics configured
  - **pgvector is enabled by default.** The CF-DBController (Task 4.3) provisions all PostgreSQL clusters with `shared_preload_libraries = 'vector'` and runs `CREATE EXTENSION IF NOT EXISTS vector` post-provision. Consumers get vector search with no additional configuration. This is sufficient for HNSW-indexed vector similarity workloads at SME scale. A separate vector database service is not planned for the platform.
- **Dependencies:** Tasks 3.1 (MinIO for backups), 0.3
- **Type:** OSS integration + infrastructure

#### Task 4.2 — Deploy ScyllaDB Operator and Alternator

- **Purpose:** DynamoDB-compatible NoSQL backend.
- **Scope:** Deploy Scylla Operator. Create `ScyllaCluster` with Alternator enabled. Validate DynamoDB SDK from Go against Alternator endpoint.
- **Key deliverables:**
  - Scylla Operator Helm chart
  - `ScyllaCluster` manifest with Alternator enabled
  - Validation: Go test using `aws-sdk-go-v2` DynamoDB client against Alternator
  - Prometheus metrics configured
- **Dependencies:** Task 0.3
- **Type:** OSS integration + infrastructure

#### Task 4.3 — Build CF-DBController

- **Purpose:** Translates CloudForge database provisioning requests into operator CRDs. Manages full lifecycle.
- **Scope:** Kubernetes controller + REST API for tenant-facing database management.

- **Key deliverables:**
  - OpenAPI spec: `api/database/v1/openapi.yaml`
  - Service: `services/db/` + `controllers/db/`
  - Kubernetes CRD: `CloudForgeDatabase`
  - Reconciler: `cnpg.Cluster` or `ScyllaCluster` based on `spec.engine`
  - Endpoints:
    - `POST /database/v1/{tenant}/{project}/instances`
    - `GET /database/v1/{tenant}/{project}/instances/{id}`
    - `DELETE /database/v1/{tenant}/{project}/instances/{id}`
    - `POST /database/v1/{tenant}/{project}/instances/{id}/restore`
    - `GET /database/v1/{tenant}/{project}/instances/{id}/connection`
  - pgvector enabled by default on all PostgreSQL instances (via `CloudForgeDatabase` spec default `spec.extensions.pgvector: true`)
  - Credential management: generate credentials → store in OpenBao via CF-SecretsConfig
  - Backup policy management via `ScheduledBackup` CRDs
  - `cf db create/list/get/delete/connect`
  - Integration tests: real CloudNativePG

- **Dependencies:** Tasks 4.1, 4.2, 1.3, 1.5, 1.6, 3.1
- **Type:** Custom Go code + Kubernetes controller + platform API

**Go libraries:**
- `sigs.k8s.io/controller-runtime`
- `github.com/cloudnative-pg/cloudnative-pg/api/v1`
- `github.com/jackc/pgx/v5`
- `github.com/scylladb/gocqlx/v3`

---

### Phase 5: Eventing Layer

#### Task 5.1 — Deploy NATS JetStream with Multi-Tenant Accounts

- **Purpose:** Messaging and eventing backbone.
- **Scope:** Deploy NATS with JetStream in cluster mode. Configure multi-tenancy via NATS accounts model. Validate account isolation.
- **Key deliverables:**
  - NATS Helm chart with JetStream, 3-node cluster
  - Dynamic NATS account provisioning model (CRD or config API, per spike findings)
  - Account isolation validated
  - NATS Prometheus exporter
  - NATS system account monitoring stream
- **Dependencies:** Tasks 0.3, 0.6 (spike)
- **Type:** OSS integration + infrastructure

#### Task 5.2 — Build CF-EventRouter Service (Rules Engine Core)

- **Purpose:** EventBridge-like routing semantics over NATS JetStream.
- **Scope:** Management API for event buses, rules, and targets; runtime engine consuming NATS streams, evaluating routing rules, dispatching to targets.

- **Key deliverables:**
  - OpenAPI spec: `api/events/v1/openapi.yaml`
  - Service: `services/events/` with `events-api` and `events-router` components
  - **Management API endpoints:**
    - `POST /events/v1/{tenant}/{project}/buses`
    - `PUT /events/v1/{tenant}/{project}/buses/{bus}/rules/{name}`
    - `DELETE /events/v1/{tenant}/{project}/buses/{bus}/rules/{name}`
    - `GET /events/v1/{tenant}/{project}/buses/{bus}/rules`
    - `POST /events/v1/{tenant}/{project}/buses/{bus}/publish`
    - `POST /events/v1/{tenant}/{project}/buses/{bus}/rules/{name}/simulate` — dry-run: returns which targets would match for a given test payload
  - Rule model: event pattern (JSON field match), target list (NATS subject, HTTP endpoint, function ARN), priority
  - Router runtime: pull consumer per event bus, pattern evaluation engine, fan-out dispatch, retry with backoff
  - **AI workflow event patterns built into the rule schema:** The event pattern language supports matching on `type` field values in the CloudEvents envelope. First-class type patterns documented: `cf.ai.inference.request.completed`, `cf.ai.model.deployed`, `cf.ai.training.job.finished`, `cf.storage.object.created` (for triggering embedding jobs when documents are uploaded). These are standard CloudEvents; CF-EventRouter has no special knowledge of AI — it matches on the `type` field the same as any other pattern. But having them documented from Phase 5 ensures consumers can immediately wire AI workflow rules when Phase 6 lands.
  - `cf events bus create/list`, `cf events rule create/list/delete`, `cf events publish`
  - Integration tests: real NATS in testcontainers

- **Dependencies:** Tasks 5.1, 1.3, 1.6, 0.6
- **Type:** Custom Go code + platform API + adapter

**Go libraries:**
- `github.com/nats-io/nats.go` (JetStream API)
- `github.com/cloudevents/sdk-go/v2`

#### Task 5.3 — NATS Account Provisioner

- **Purpose:** Automated NATS account creation on tenant onboarding.
- **Scope:** Extend CF-ResourceController tenant onboarding to create NATS account per tenant, stream per project default event bus.
- **Key deliverables:**
  - NATS account provisioning in CF-ResourceController tenant creation flow
  - `pkg/nats/` account management client
  - Account credentials in CF-SecretsConfig
  - Validated end-to-end: new tenant → NATS account created → Events API can publish
- **Dependencies:** Tasks 5.1, 5.2, 1.6
- **Type:** Custom Go code + integration

---

### Phase 6: Compute Layer — Functions and AI Serving

This phase builds the complete compute layer of CloudForge. Knative (event-driven functions) and the AI serving runtime (vLLM/KServe) are deployed as peer compute workload types under a unified compute API. They share the same IAM authorization model, the same API gateway routing, the same observability pipeline, and the same quota enforcement.

The reason they belong in the same phase is not incidental — it is architectural. An AI serving endpoint is a compute workload that scales with demand, responds to HTTP requests, emits telemetry, and consumes platform secrets. The runtime is different (vLLM instead of a Go HTTP handler), but the platform relationship is identical. Building them together avoids designing Knative-only abstractions that would need retrofitting when AI serving is introduced later.

#### Task 6.1 — Deploy Knative Serving and Eventing

- **Purpose:** Function execution runtime.
- **Scope:** Deploy Knative Serving via Knative Operator. Deploy Knative Eventing with NATS-backed channels. Validate scale-to-zero. Validate HTTP invocation from Go.
- **Key deliverables:**
  - Knative Operator Helm chart
  - Knative Serving: scale-to-zero validated
  - Knative NATS channel: `NatssChannel` provisioner backed by Phase 5 NATS cluster
  - Prometheus metrics from Knative scraped
- **Dependencies:** Tasks 5.1, 0.8 (spike)
- **Type:** OSS integration + infrastructure

#### Task 6.2 — Deploy KServe and vLLM Serving Runtime

- **Purpose:** AI inference serving infrastructure.
- **Scope:** Deploy KServe. Configure vLLM as a `ServingRuntime`. Deploy Ollama as an alternative `ServingRuntime` for CPU-only environments. Validate an `InferenceService` serving a small model from MinIO.

- **Key deliverables:**
  - KServe Helm chart
  - vLLM `ServingRuntime` CRD manifest
  - Ollama `ServingRuntime` CRD manifest (CPU-mode, for dev and non-GPU deployments)
  - `InferenceService` sample: load a small model from MinIO `cf-platform-models` bucket, serve via OpenAI-compatible API
  - GPU resource requests and limits on `InferenceService` pods (via NVIDIA device plugin from Task 0.9 spike)
  - Validation: Go client calling `/v1/chat/completions` and receiving a streamed response
  - Prometheus metrics: vLLM `vllm:*` metrics family scraped by Prometheus
  - **No-GPU path:** When cluster has no GPU nodes, KServe falls back to Ollama-backed `ServingRuntime` for compatible smaller models. This is documented clearly; GPU is required for production inference at meaningful throughput but is not required to boot the platform or deploy the AI serving infrastructure.

- **Dependencies:** Tasks 3.1 (MinIO for model weights), 0.9 (GPU spike), 0.3
- **Type:** OSS integration + infrastructure

#### Task 6.3 — Build CF-FunctionTrigger Service

- **Purpose:** Bridges CF-EventRouter, NATS consumers, MinIO events, and cron schedules to Knative function invocations.
- **Scope:** Kubernetes controller (CRD-driven) plus management API.

- **Key deliverables:**
  - OpenAPI spec: `api/functions/v1/openapi.yaml`
  - Service: `services/functions/` + `controllers/functions/`
  - CRDs: `CloudForgeFunction`, `FunctionTrigger`
  - Reconciler for `CloudForgeFunction`: creates `Ksvc`, injects workload identity token, sets resource limits
  - Reconciler for `FunctionTrigger`: NATS push consumer → function HTTP invocation
  - IAM context injection: signed `X-CF-Principal` header per invocation
  - Cron trigger: Kubernetes `CronJob` per scheduled trigger
  - MinIO event trigger: MinIO webhook → CF-FunctionTrigger dispatch
  - **AI-relevant function pattern:** A function triggered by `cf.storage.object.created` events on a documents bucket can call the CF-AIRuntime inference API (via the CloudForge inference client in `pkg/inference/`) to generate embeddings, then store them in PostgreSQL via pgvector. This is a fully native platform pattern using Phase 3 (MinIO), Phase 4 (PostgreSQL+pgvector), Phase 5 (events), Phase 6 (functions + inference) — no additional software required.
  - Management API endpoints (see Plan.md v0.1 for complete list)
  - `cf fn deploy/invoke/list/logs/triggers`
  - Integration tests

- **Dependencies:** Tasks 6.1, 5.2, 3.2, 1.3, 1.6, 0.8
- **Type:** Custom Go code + Kubernetes controller + platform API + adapter

#### Task 6.4 — Build CF-AIRuntime Service

- **Purpose:** Tenant-facing AI serving API. Manages model deployment lifecycle, exposes inference endpoints, enforces IAM and quotas, emits usage telemetry. This is the compute management API for AI workloads, parallel to how CF-FunctionTrigger is the compute management API for function workloads.

- **Scope:** Model registry, deployment lifecycle via KServe CRDs, OpenAI-compatible inference proxy with IAM enforcement and usage metering, usage reporting to CF-ResourceController.

- **Key deliverables:**
  - OpenAPI spec: `api/ai/v1/openapi.yaml`
  - Service: `services/ai/`
  - **Model Registry endpoints:**
    - `POST /ai/v1/{tenant}/{project}/models` — register a model (specify MinIO path or HuggingFace model ID)
    - `GET /ai/v1/{tenant}/{project}/models`
    - `DELETE /ai/v1/{tenant}/{project}/models/{name}`
  - **Serving Deployment endpoints:**
    - `POST /ai/v1/{tenant}/{project}/deployments` — create `InferenceService` CRD via KServe; specify model, runtime (vLLM or Ollama), resource profile, autoscaling config
    - `GET /ai/v1/{tenant}/{project}/deployments/{id}`
    - `DELETE /ai/v1/{tenant}/{project}/deployments/{id}`
    - `GET /ai/v1/{tenant}/{project}/deployments/{id}/status`
  - **Inference Proxy endpoint:**
    - `POST /ai/v1/{tenant}/{project}/infer/{deployment}/v1/chat/completions` — OpenAI-compatible; validates API key via CF-IAM; proxies to correct `InferenceService` endpoint; intercepts response to count tokens; emits `cf.ai.inference.request.completed` CloudEvent to CF-EventRouter; records usage in CF-ResourceController
    - `POST /ai/v1/{tenant}/{project}/infer/{deployment}/v1/embeddings` — embeddings endpoint; same proxy and metering logic
  - **HuggingFace model download:** `POST /ai/v1/{tenant}/{project}/models/{name}/pull` — triggers a Kubernetes Job that downloads a model from HuggingFace using the tenant's `hf-token` secret from CF-SecretsConfig, stores weights in MinIO `cf-{tenant}-models` bucket, updates model registry status to `ready`
  - KServe client: `pkg/kserve/` (manages `InferenceService` CRDs via controller-runtime)
  - Inference proxy client: `pkg/inference/` (Go HTTP client for OpenAI-compatible streaming API; handles chunked transfer encoding correctly)
  - Usage event: emits structured CloudEvent per inference request including `token_count_prompt`, `token_count_completion`, `model_name`, `deployment_id`, `latency_ms`
  - Route registration: on deployment creation, calls CF-GatewayControl to create an `ai-proxy` route exposing the inference endpoint at `/{tenant}/{project}/ai/{deployment}/v1/...`
  - `cf ai model register/list`, `cf ai deploy/undeploy/status`, `cf ai infer`
  - Integration tests: Ollama in testcontainers (CPU-mode, no GPU needed for tests)

- **Dependencies:** Tasks 6.2, 3.2, 1.3, 1.5, 1.6, 5.2 (for usage events), 2.2 (for route registration)
- **Type:** Custom Go code + Kubernetes controller + platform API + adapter

**Go libraries:**
- `sigs.k8s.io/controller-runtime`
- `github.com/kserve/kserve/pkg/apis`
- `github.com/sashabaranov/go-openai` — for response type validation in tests
- Standard `net/http` for the streaming proxy (do not buffer streaming responses)

---

### Phase 7: Observability Layer

#### Task 7.1 — Deploy OTel Collector, Prometheus, and Grafana (Platform Tier)

- **Purpose:** Platform-internal observability. Bootstrapped in minimal form in Phase 1; completed here.
- **Scope:** OTel Collector (DaemonSet + gateway). Prometheus with Alertmanager. Grafana with pre-configured datasources.
- **Key deliverables:**
  - OTel Collector Helm chart (DaemonSet + gateway)
  - Pipeline: logs → OpenSearch; traces → Tempo; metrics → Prometheus
  - Prometheus with storage PVC
  - Alertmanager with webhook channel
  - Grafana with datasources: Prometheus, Tempo, OpenSearch
  - Initial platform dashboards: NATS queue depth, Knative invocation rate, APISIX request latency, CloudNativePG replication lag
  - **AI serving dashboards:** vLLM metrics family scraped and displayed: requests/sec per model, p50/p95/p99 time-to-first-token, token throughput (tokens/sec), queue length, GPU memory utilization (when GPU is present). These are standard Prometheus metrics from vLLM and KServe; no additional instrumentation required.
- **Dependencies:** Task 0.3, Task 1.1 (Keycloak for Grafana OIDC login), Task 6.2 (vLLM metrics available)
- **Type:** OSS integration + infrastructure

#### Task 7.2 — Deploy OpenSearch

- **Purpose:** Centralized log store, search, and analytics.
- **Scope:** Deploy OpenSearch via OpenSearch Operator. Configure index templates for platform and tenant log indices. Configure ISM for lifecycle management.
- **Key deliverables:**
  - OpenSearch Operator Helm chart
  - Index templates:
    - `cf-platform-*` — platform operational logs
    - `cf-{tenant}-{project}-app-*` — tenant application logs
    - `cf-{tenant}-{project}-ai-infer-*` — AI inference request logs (one document per inference request: model, token counts, latency, status)
    - `cf-{tenant}-{project}-ai-agent-*` — consumer AI agent execution traces (for consumers who use the agent trace emission pattern)
  - ISM policy: hot 7 days → warm 30 days → archive to MinIO → delete after 90 days
  - AI inference index validated: publish a sample inference log document, query it back
- **Dependencies:** Tasks 3.1 (MinIO for archival), 0.3
- **Type:** OSS integration + infrastructure

#### Task 7.3 — Build CF-Observability Service

- **Purpose:** Tenant-scoped observability API with AI workload telemetry as a first-class concern.
- **Scope:** Tenant log query API, alert CRUD, Grafana tenant dashboard provisioning, OTel Collector config management.

- **Key deliverables:**
  - OpenAPI spec: `api/observability/v1/openapi.yaml`
  - Service: `services/observe/`
  - Endpoints:
    - `POST /observe/v1/{tenant}/{project}/logs/query`
    - `GET /observe/v1/{tenant}/{project}/logs/stream` (SSE tail)
    - `POST /observe/v1/{tenant}/{project}/alerts`
    - `GET /observe/v1/{tenant}/{project}/metrics`
    - `GET /observe/v1/{tenant}/{project}/ai/usage` — returns token usage summary (total tokens by model, by day) for the project; backed by aggregation query against `cf-{tenant}-{project}-ai-infer-*` OpenSearch index
    - `POST /observe/v1/{tenant}/{project}/ai/traces/query` — structured query against AI inference and agent trace indices; tenant-scoped; does not expose raw Lucene/DSL
  - OpenSearch client: `pkg/opensearch/` (tenant-scoped index prefix enforcement)
  - Grafana API client: `pkg/grafana/` (per-tenant org, datasource provisioning)
  - AI usage aggregation: CF-Observability aggregates token usage from OpenSearch `ai-infer` indices and exposes a structured usage summary. This is also consumed by CF-ResourceController for quota enforcement and, in Phase 9, by the billing layer.
  - `cf logs tail`, `cf ai usage` CLI commands
  - Full instrumentation of all CF services with OTel spans, structured logs, Prometheus metrics

- **Dependencies:** Tasks 7.1, 7.2, 1.3, 1.6, 6.4 (AI inference events flowing into OpenSearch)
- **Type:** Custom Go code + platform API + adapter

**Go libraries:**
- `github.com/opensearch-project/opensearch-go/v4`
- Standard `net/http` for Alertmanager and Grafana APIs

#### Task 7.4 — Full Platform Instrumentation

- **Purpose:** Audit and complete OTel instrumentation across all CF services.
- **Scope:** Review all services built to date. Ensure: every HTTP handler emits a trace span, all outgoing calls propagate trace context, structured logs include trace ID, Prometheus metrics exist for key operations, resource attributes include `cf.tenant` and `cf.project`.
- **Key deliverables:**
  - OTel middleware applied consistently
  - Platform Grafana dashboard: all services request rates, error rates on one screen
  - AI-specific SLOs defined: p99 time-to-first-token < 2s for typical models on recommended hardware; inference request error rate < 0.1%
- **Dependencies:** All service builds, Task 7.1
- **Type:** Custom Go code + operational

---

### Phase 8: MVP Hardening, Deployment, and Release

#### Task 8.1 — End-to-End Integration Test Suite

- **Purpose:** Validate the complete platform behaves correctly end-to-end before release.
- **Scope:** Build an e2e test suite in `tests/e2e/` that covers the main platform scenarios including an AI workload scenario.
- **Key deliverables:**
  - Scenario 1: Tenant onboarding → project creation → resource listing
  - Scenario 2: Storage → bucket create → upload object → pre-signed download
  - Scenario 3: Database → provision PostgreSQL → connect → create table → insert row
  - Scenario 4: Eventing → create event bus → define rule → publish event → verify dispatch
  - Scenario 5: Functions → deploy function → trigger via event → verify invocation
  - Scenario 6: AI serving → register model → deploy to KServe/Ollama → call inference endpoint → verify response → verify usage recorded in CF-Observability
  - Scenario 7: AI + database → deploy embedding function → trigger on object upload → generate embeddings via inference → store in pgvector → run similarity query
  - Each scenario runs against a real (test) cluster via `cf-install validate`
- **Dependencies:** All previous phases complete
- **Type:** Custom Go code + operational tooling

#### Task 8.2 — CloudForge Helm Chart

- **Purpose:** Single-command platform installation.
- **Scope:** Parent Helm chart (`charts/cloudforge`) with sub-charts for each component. Deployment profiles: `dev`, `small` (3 nodes), `production` (5+ nodes). GPU node profile optional.
- **Key deliverables:**
  - `charts/cloudforge/Chart.yaml` with all component dependencies
  - `charts/cloudforge/values.yaml` with profile-based configuration
  - `values-dev.yaml`: no GPU, Ollama for AI serving, reduced replicas
  - `values-small.yaml`: optional GPU, vLLM available if GPU node present, production HA
  - `values-production.yaml`: GPU required, vLLM with autoscaling, full HA
  - Helm hook for bootstrap: post-install job runs `cf-install init`
- **Dependencies:** All service implementations complete
- **Type:** Infrastructure + operational tooling

#### Task 8.3 — Bootstrap CLI (`cf-install`)

- **Purpose:** Guided installation and first-run configuration.
- **Scope:** `cmd/cf-install/` Go binary with preflight, init, validate, and upgrade commands.
- **Key deliverables:**
  - `cf-install preflight` — validates: Kubernetes version, CPU/memory, storage class, GPU nodes (optional), NVIDIA device plugin if GPU present
  - `cf-install init` — bootstraps namespaces, RBAC, admin credentials, `cf-admin` tenant
  - `cf-install validate` — runs e2e smoke test including AI inference scenario (using Ollama if no GPU, vLLM if GPU)
  - `cf-install upgrade` — pre-upgrade CRD migration check and ordering validation
- **Dependencies:** Tasks 8.2, all services
- **Type:** Custom Go code + operational tooling

#### Task 8.4 — Documentation and Consumer Guides

- **Purpose:** Enable consumers to understand and use the platform's capabilities.
- **Key deliverables:**
  - `docs/getting-started.md` — install, create tenant, provision first resources
  - `docs/storage.md` — bucket management, pre-signed URLs, model artifact conventions
  - `docs/database.md` — PostgreSQL provisioning, pgvector usage guide
  - `docs/eventing.md` — event buses, routing rules, AI workflow event patterns
  - `docs/functions.md` — function deployment, triggers, IAM context
  - `docs/ai/` directory:
    - `ai/serving.md` — deploy a model, expose inference endpoint, manage usage
    - `ai/rag.md` — building a RAG pipeline using pgvector + MinIO + CF-AIRuntime
    - `ai/event-driven-agents.md` — building an event-triggered agent using NATS + CF-AIRuntime + OpenBao
    - `ai/fine-tuning.md` — running a fine-tuning job on a GPU node using Kubernetes Jobs + MinIO
  - `examples/` directory with runnable examples for each AI use case

---

### Phase 9: Hardening, Advanced Capabilities, and Managed Offering Readiness

Phase 9 is a continuous track that runs after the MVP release. Items are listed in priority order.

#### Task 9.1 — IAM Hardening

- Resource-based IAM policies (in addition to identity-based)
- Permission boundaries
- Cross-project role assumption

#### Task 9.2 — Eventing Hardening

- DLQ with configurable retry policy and backoff
- ScyllaDB CDC → NATS bridge (DynamoDB Streams equivalent)
- Event bus dead-letter monitoring in CF-Observability

#### Task 9.3 — Database Expansion

- MySQL support via Percona Operator in CF-DBController
- Automated point-in-time recovery testing
- Connection pool tuning API (PgBouncer parameters via CF-DBController)

#### Task 9.4 — Data Pipeline Service (CF-DataPipeline)

- Apache Airflow adapter for tenant-scoped workflow orchestration
- DAG namespace isolation
- Training data pipeline reference implementation (Airflow DAG → MinIO data fetch → Kubernetes GPU Job → MinIO output)
- `cf pipeline` CLI subcommand

#### Task 9.5 — Advanced AI Infrastructure

- **GPU MIG partitioning:** Multi-Instance GPU slicing for multi-tenant inference isolation. Requires NVIDIA A100/H100 hardware and MIG-capable scheduling configuration in Kubernetes. Allows multiple tenants to share a single GPU with hard isolation boundaries.
- **Distributed training job submission:** `POST /ai/v1/{tenant}/{project}/training-jobs` — submit a training or fine-tuning job as a Kubernetes `Job` (or `PyTorchJob` via Kubeflow Training Operator). Job pulls base model from MinIO, pulls training data from MinIO, runs on GPU node, outputs fine-tuned weights to MinIO. Full IAM, quota, and observability integration.
- **Model fine-tuning API:** Higher-level API over training jobs for LoRA/QLoRA fine-tuning with preset configurations. Consumer specifies: base model, training data bucket path, output bucket path, LoRA rank, epochs. Platform generates the training job.
- **Batch inference jobs:** `POST /ai/v1/{tenant}/{project}/batch-infer` — run inference over a dataset in MinIO, store results back to MinIO. Implemented as a Kubernetes Job using the vLLM offline inference API.

#### Task 9.6 — Consumer AI Reference Implementations

- Go Agent SDK: `pkg/agent/` — lightweight library providing: NATS trigger subscription, CF-AIRuntime inference client, CF-SecretsConfig secret access, MinIO artifact I/O, OTel trace emission for agent execution steps. Not a framework — a set of typed wrappers over platform APIs.
- Python integration: example LangGraph agent using CloudForge REST APIs (generated Python client from OpenAPI specs). Documented in `docs/ai/event-driven-agents.md`.
- Reference architecture: complete RAG system (`examples/ai/rag/`) demonstrating document ingest → embedding → storage → retrieval → generation using only platform primitives.

#### Task 9.7 — Managed Offering Readiness

- Billing hooks in CF-ResourceController (usage metering export)
- Multi-cluster architecture for the managed offering control plane
- Platform SRE operational runbooks
- Tenant isolation hardening at GPU node level (dedicated node pool assignment via resource profiles)

---

## 5. AI Platform Capabilities and Consumer Use Cases

This section documents the AI-enabling platform capabilities introduced across the phases above, and describes the consumer AI workloads they enable. It is not a feature spec — it is a guide for evaluating whether the implementation plan produces a platform that is genuinely AI-capable.

### AI Infrastructure Provided by Layer

| Platform Layer | Capability Delivered | Consumer AI Use |
|---|---|---|
| **Identity (Phase 1)** | API keys for inference endpoints; AI workload identity types; `hf-token` and `model-api-key` secret types | Consumers authenticate against deployed models with API keys; training jobs have platform identity; model downloads use stored HuggingFace tokens |
| **Storage (Phase 3)** | MinIO with model artifact bucket conventions; large object pre-signed URLs; server-side encryption | Store model weights, training datasets, ONNX exports, LoRA adapters, evaluation results; share large artifacts between teams via pre-signed URLs |
| **Databases (Phase 4)** | pgvector pre-installed on all PostgreSQL instances; HNSW index support; cosine/dot product similarity queries | Vector store for RAG embeddings; semantic search index; recommendation system feature store; no separate vector database needed |
| **Eventing (Phase 5)** | NATS JetStream with AI workflow event type patterns; fan-out routing to multiple consumers | Trigger embedding jobs on document upload; chain inference pipeline steps; fire-and-forget async inference with result notification; training job lifecycle events |
| **Functions (Phase 6)** | Knative scale-to-zero functions with event triggers | Deploy lightweight AI processing steps as serverless functions (tokenization, classification, routing) without managing containers |
| **AI Serving (Phase 6)** | KServe + vLLM (GPU) + Ollama (CPU); OpenAI-compatible API; model registry; per-tenant deployment management; streaming support | Deploy any open model; get a production inference API back; autoscale based on request volume; expose the endpoint via the API gateway |
| **Observability (Phase 7)** | GPU utilization metrics; token usage per tenant/project; model latency histograms; inference request traces; AI agent execution trace index | Monitor AI workload cost and performance; set alerts on token budget overrun; debug slow inference; audit AI system behavior |
| **API Gateway (Phase 2+)** | `ai-proxy` route type with token-budget rate limiting, streaming passthrough, API key auth | Expose consumer-deployed models to end users or external systems via managed, rate-limited endpoints |

### Consumer AI Workloads Enabled at MVP

When Phase 8 (MVP) ships, consumers can immediately build:

**1. RAG (Retrieval-Augmented Generation) pipeline**
- Store documents in MinIO (Storage API)
- Upload event triggers an embedding function (Functions → CF-FunctionTrigger)
- Function calls consumer's deployed embedding model (CF-AIRuntime inference endpoint)
- Embeddings stored in PostgreSQL pgvector (CF-DBController)
- Query time: retrieve top-k embeddings, pass context to LLM, return response
- All platform-native, no external services

**2. Event-driven inference pipeline**
- Consumer application publishes event to NATS bus (CF-EventRouter)
- Routing rule matches event, triggers inference function
- Function calls deployed LLM, returns result to event bus
- Downstream consumer receives result event
- Fully async, fully observable via CF-Observability

**3. Custom model serving endpoint**
- Upload model weights to MinIO
- Register model in CF-AIRuntime model registry
- Deploy with vLLM or Ollama runtime
- Expose via API gateway with API key authentication and rate limiting
- Monitor usage and latency in Grafana

**4. Secrets-safe AI application**
- Store remote model provider API keys, HuggingFace tokens, dataset credentials in CF-SecretsConfig
- Functions and model serving workloads receive secrets via injection
- No credentials in code or environment variables
- Full audit trail of secret access in OpenSearch

**5. Multi-step AI workflow**
- String together: document fetch (MinIO) → OCR/preprocessing (function) → embedding (inference endpoint) → vector store (pgvector) → retrieval query → generation (LLM) → result storage (MinIO) → notification event (NATS)
- Each step is a CloudForge resource; the whole pipeline is observable from a single Grafana dashboard

### Consumer AI Workloads Available in Phase 9

After Phase 9 hardening, consumers can additionally build:

- **Fine-tuning pipelines:** submit training jobs against consumer training data in MinIO, using GPU nodes, with platform quota and billing enforcement
- **Batch inference jobs:** run offline inference over large datasets stored in MinIO
- **Agent workflows with LangGraph:** Python-based LangGraph agents using CloudForge REST APIs for platform integration
- **Multi-tenant inference sharing:** GPU MIG-isolated inference endpoints for multi-tenant SaaS products

---

## 6. OSS Integration Plan

### NATS JetStream

- **Introduced in:** Phase 5
- **Go integration:** `github.com/nats-io/nats.go` JetStream API. All platform services publish events through `pkg/events/publisher.go`. CF-EventRouter is the only service that uses the JetStream consumer API directly.
- **Abstraction level:** `pkg/events/` wraps connection lifecycle. Other services use `pkg/events/publisher.go` and do not touch JetStream APIs directly.

### Knative Serving

- **Introduced in:** Phase 6
- **Go integration:** `sigs.k8s.io/controller-runtime` with `knative.dev/serving/pkg/apis/serving/v1` CRD types. Function invocation is plain HTTP POST to the Knative service URL.
- **Abstraction level:** Hidden from tenants behind `CloudForgeFunction` CRD and CF-FunctionTrigger API.

### KServe + vLLM / Ollama

- **Introduced in:** Phase 6 (deployed), Phase 6 Task 6.4 (CF-AIRuntime API)
- **Go integration:** KServe CRD management via `sigs.k8s.io/controller-runtime` with `github.com/kserve/kserve/pkg/apis` types. vLLM and Ollama inference is proxied via standard `net/http` — they both serve the OpenAI-compatible REST API. Wrap in `pkg/inference/` for the proxy client.
- **Abstraction level:** KServe `InferenceService` CRDs are managed by CF-AIRuntime. Consumers interact with the CloudForge AI API; they never configure KServe directly.

### MinIO

- **Introduced in:** Phase 3
- **Go integration:** `github.com/minio/minio-go/v7`. Wrapped in `pkg/minio/` with credential injection from CF-SecretsConfig.
- **Abstraction level:** `pkg/minio/` used by CloudForge Storage API and internally by CF-FunctionTrigger and CF-AIRuntime. Tenants use the Storage API.

### CloudNativePG + pgvector

- **Introduced in:** Phase 4
- **Go integration:** `github.com/cloudnative-pg/cloudnative-pg/api/v1` types, managed via `controller-runtime`. pgvector is a PostgreSQL extension — no Go SDK needed; consumers use standard `pgx/v5` with vector type support via `pgvector-go`.
- **Abstraction level:** CloudNativePG CRDs managed by CF-DBController. Tenants use the CloudForge Database API. pgvector queries are made directly against the provisioned PostgreSQL endpoint using standard SQL.

**Go library for pgvector queries in consumer applications:**
- `github.com/pgvector/pgvector-go` — provides `pgvector.Vector` type that integrates with `pgx/v5` for reading/writing vector columns. Consumers import this in their application code, not in CloudForge platform code.

### ScyllaDB

- **Introduced in:** Phase 4
- **Go integration:** Scylla Operator CRDs via `controller-runtime`. Consumer workloads use `aws-sdk-go-v2/service/dynamodb` against the Alternator endpoint.
- **Abstraction level:** CF-DBController manages `ScyllaCluster` CRDs. Alternator DynamoDB API is exposed directly to consumers via CF-GatewayControl routing.

### Keycloak

- **Introduced in:** Phase 1
- **Go integration:** Keycloak Admin REST API via standard `net/http` in `pkg/keycloak/admin.go`. Token validation via `github.com/coreos/go-oidc/v3`.
- **Abstraction level:** `pkg/keycloak/` used exclusively by CF-IAM. Tenants interact with CloudForge IAM API.

### Open Policy Agent

- **Introduced in:** Phase 1
- **Go integration:** Embedded OPA (`github.com/open-policy-agent/opa/v1/rego`) for policy bundle compilation in CF-IAM. OPA daemon HTTP API for runtime authorization checks. `pkg/authz/checker.go` provides `CanDo(ctx, principal, action, resource) (bool, error)` interface used by all CF services.
- **Abstraction level:** `pkg/authz/` used by all CF services. No service calls OPA directly.

### OpenBao

- **Introduced in:** Phase 1
- **Go integration:** `github.com/openbao/openbao/api/v2`. Wrapped in `pkg/openbao/` with Kubernetes auth and token renewal.
- **Abstraction level:** `pkg/openbao/` used only by CF-SecretsConfig. All other services call CF-SecretsConfig via HTTP.

### Apache APISIX

- **Introduced in:** Phase 2
- **Go integration:** APISIX Admin REST API via standard `net/http` in `pkg/apisix/admin.go`.
- **Abstraction level:** `pkg/apisix/` used only by CF-GatewayControl.

### OpenSearch

- **Introduced in:** Phase 7
- **Go integration:** `github.com/opensearch-project/opensearch-go/v4`. Wrapped in `pkg/opensearch/` with tenant index prefix enforcement.
- **Abstraction level:** `pkg/opensearch/` used only by CF-Observability.

---

## 7. Recommended Go Frameworks, Libraries, and SDKs

### HTTP Server and Routing

**`github.com/go-chi/chi/v5`** for all CloudForge service HTTP servers. Lightweight, composable middleware. Does not impose application structure. Avoid gin (testing ergonomics), avoid full frameworks.

Every service registers routes against a `chi.Router` mounted on a standard `net/http` server.

### Generated API Layer

**`github.com/oapi-codegen/oapi-codegen`** — all CF service REST APIs defined in OpenAPI 3.1 first. Use strict server generation mode: handlers return typed structs, not raw `http.ResponseWriter` calls. Generate client SDKs from the same specs for the CLI and inter-service calls.

### Kubernetes Controllers

**`sigs.k8s.io/controller-runtime`** for all Kubernetes controllers (CF-DBController, CF-FunctionTrigger, CF-AIRuntime's KServe management). Use `envtest` for controller integration tests (spins up real Kubernetes API server and etcd).

### Database Access

**`github.com/jackc/pgx/v5` + `pgxpool`** for PostgreSQL — preferred over `database/sql` for full PostgreSQL feature support. Schema migrations via `github.com/golang-migrate/migrate/v4` with embedded SQL files.

**`github.com/scylladb/gocqlx/v3`** for ScyllaDB.

**`github.com/pgvector/pgvector-go`** for consumer applications using pgvector — provides the `Vector` type for `pgx/v5` integration. This is a consumer-facing recommendation, not a platform service dependency.

### Observability

**`go.opentelemetry.io/otel`** full SDK. Use `otelhttp` for automatic HTTP server/client instrumentation. `log/slog` with OTel log handler for log-trace correlation. `github.com/prometheus/client_golang/prometheus` for metrics.

### Configuration

**`github.com/spf13/viper`** for all service configuration. Validate with `github.com/go-playground/validator/v10` at startup.

### CLI

**`github.com/spf13/cobra`** with generated API clients from `oapi-codegen`. Token management via `golang.org/x/oauth2` with OIDC device flow.

### Testing

**`github.com/stretchr/testify`** + **`github.com/testcontainers/testcontainers-go`**. Mocks via `github.com/vektra/mockery`. Integration tests tagged with `//go:build integration`. Do not mock everything — the most important tests talk to real backends.

### AI Inference Client (Go)

**Standard `net/http`** for the streaming inference proxy in CF-AIRuntime — do not buffer the response body; pipe the chunked response directly to the client. For testing and CLI usage: **`github.com/sashabaranov/go-openai`** (OpenAI-compatible Go client) for constructing typed request/response structures.

### Module Structure

```
cloud-forge/
├── go.work
├── cmd/
│   ├── cf/                        # CLI
│   ├── cf-install/                # Bootstrap CLI
│   ├── cf-iam/                    # CF-IAM service
│   ├── cf-secrets/                # CF-SecretsConfig
│   ├── cf-resource/               # CF-ResourceController
│   ├── cf-events/                 # CF-EventRouter
│   ├── cf-functions/              # CF-FunctionTrigger
│   ├── cf-db/                     # CF-DBController
│   ├── cf-gateway/                # CF-GatewayControl
│   ├── cf-observe/                # CF-Observability
│   └── cf-ai/                     # CF-AIRuntime
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
│   ├── authz/                     # CF-IAM authz checker (used by all services)
│   ├── client/                    # Generated API clients for all CF services
│   ├── events/                    # NATS publisher + CloudEvents builder
│   ├── grafana/
│   ├── inference/                 # OpenAI-compatible streaming proxy client
│   ├── keycloak/
│   ├── kserve/                    # KServe InferenceService CRD management
│   ├── minio/
│   ├── openbao/
│   ├── opensearch/
│   └── resource/                  # cf:// URI types, tenant/project identifiers
├── services/
│   ├── ai/                        # CF-AIRuntime business logic
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
│   ├── ai/                        # InferenceService reconciler
│   ├── db/
│   ├── functions/
│   └── platform/
├── api/
│   ├── ai/v1/openapi.yaml
│   ├── database/v1/openapi.yaml
│   ├── events/v1/openapi.yaml
│   ├── functions/v1/openapi.yaml
│   ├── gateway/v1/openapi.yaml
│   ├── iam/v1/openapi.yaml
│   ├── observability/v1/openapi.yaml
│   ├── resource/v1/openapi.yaml
│   ├── secrets/v1/openapi.yaml
│   └── storage/v1/openapi.yaml
├── deploy/
│   ├── helm/
│   │   ├── cloudforge/
│   │   └── components/
│   ├── crds/
│   └── kustomize/
├── spikes/
│   ├── ai-runtime/                # Task 0.9 spike
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
└── docs/
    └── ai/
```

---

## 8. Dependency and Sequencing Map

```
Phase 0: Foundation (Weeks 1–4)
  Tasks: 0.1 → 0.2 → 0.3 → 0.4, 0.5 (parallel)
  Spikes (parallel): 0.6 (NATS), 0.7 (OPA), 0.8 (Knative), 0.9 (GPU/vLLM)
        │
        ▼
Phase 1: Identity, Secrets, Tenancy (Weeks 3–10)
  Tasks: 1.1 (Keycloak), 1.2 (OPA) → 1.3 (CF-IAM)
         1.4 (OpenBao) → 1.5 (CF-SecretsConfig)
         1.3 + 1.5 → 1.6 (CF-ResourceController)
         ┌─────────────────────────────────────┐
         │ Parallel: 7.1 minimal bootstrap     │
         │ (Prometheus + OTel Collector only)  │
         └─────────────────────────────────────┘
        │
        ▼
Phase 2: API Gateway (Weeks 8–14)           Phase 3: Storage (Weeks 10–16)
  2.1 (APISIX) → 2.2 (CF-GatewayControl)    3.1 (MinIO) → 3.2 (Storage API)
  → 2.3 (Wire APIs) → 2.4 (CLI)             [Parallel with Phase 2]
        │                        │
        └────────────┬───────────┘
                     │
        ┌────────────▼────────────┐
        │  Phase 4: Databases     │  Phase 5: Eventing (Weeks 14–22)
        │  (Weeks 12–18)          │  [Can start once Phase 1 done]
        │  4.1 (CNPG+pgvector)    │  5.1 (NATS) → 5.2 (CF-EventRouter)
        │  4.2 (ScyllaDB)         │  → 5.3 (NATS Account Provisioner)
        │  → 4.3 (CF-DBController)│
        └────────────┬────────────┘
                     │
                     ▼
        Phase 6: Compute Layer (Weeks 20–28)
          [Requires: Phase 5 + Phase 3 + Phase 1 + Spikes 0.8 + 0.9]
          6.1 (Knative) ──────────────────────────────────────────┐
          6.2 (KServe + vLLM + Ollama) ──────────────────────────┤
          6.3 (CF-FunctionTrigger) [depends on 6.1, 5.2, 3.2]    │
          6.4 (CF-AIRuntime) [depends on 6.2, 3.2, 1.3, 5.2, 2.2]┘
                     │
                     ▼
        Phase 7: Observability (Weeks 22–30)
          7.1 (OTel + Prometheus + Grafana — COMPLETE; minimal in Phase 1)
          7.2 (OpenSearch) → 7.3 (CF-Observability) → 7.4 (Instrumentation)
                     │
                     ▼
        Phase 8: MVP Hardening + Release (Weeks 26–32)
          8.1 (E2E tests) → 8.2 (Helm chart) → 8.3 (cf-install) → 8.4 (Docs)
                     │
                     ▼
        Phase 9: Hardening + Advanced AI (Weeks 30+)
          9.1 IAM hardening, 9.2 Eventing hardening, 9.3 DB expansion
          9.4 Data pipeline (Airflow), 9.5 Advanced AI (GPU MIG, training jobs)
          9.6 Consumer AI reference SDK, 9.7 Managed offering readiness
```

### Parallel Execution Opportunities

- **WS-10 (Networking/Ingress):** Cilium and Contour can be deployed in parallel with Phase 1 — no dependency on any CF service.
- **Phase 3 (Storage) and Phase 2 (API Gateway):** MinIO deployment (Task 3.1) can start in parallel with Phase 2 since it only depends on the cluster and OpenBao.
- **Phase 4 (Databases) and Phase 5 (Eventing):** Can both start once Phase 1 is complete. They have no dependency on each other.
- **Phase 6 Tasks 6.1–6.4:** 6.1 (Knative) and 6.2 (KServe) can be deployed in parallel. 6.3 (CF-FunctionTrigger) and 6.4 (CF-AIRuntime) can be built in parallel after their respective runtimes are deployed.
- **Phase 7 minimal bootstrap:** OTel Collector and Prometheus should be deployed alongside Phase 1 so that Phase 1 services can be instrumented. OpenSearch comes later in Phase 7 proper.

---

## 9. MVP vs Later Phases

### MVP (Phases 0–8)

The MVP is reached when the following is true: a consumer can install CloudForge on a Kubernetes cluster, create a tenant and project, provision standard application infrastructure (storage, database, eventing, functions), **and deploy and call their own AI model** — all through a consistent CloudForge API and CLI, with IAM enforcement, secrets management, and unified observability.

| Capability | MVP Status |
|---|---|
| CF-IAM (identity-based policies, API keys for AI endpoints) | Required |
| CF-SecretsConfig (secrets + AI credential types) | Required |
| CF-ResourceController (tenant, project, AI quotas) | Required |
| CF-GatewayControl (routes + AI proxy route type + streaming) | Required |
| CloudForge CLI | Required |
| Storage API + MinIO (model artifact bucket conventions) | Required |
| CF-DBController PostgreSQL + pgvector by default | Required |
| CF-EventRouter (routing rules + AI workflow event patterns) | Required |
| CF-FunctionTrigger (NATS trigger + cron) | Required |
| **CF-AIRuntime (model registry + KServe deployment + inference proxy)** | **Required** |
| **KServe + vLLM (GPU) + Ollama (CPU fallback)** | **Required** |
| OTel + Prometheus + Grafana (with vLLM metrics dashboard) | Required |
| OpenSearch + CF-Observability (with AI usage API) | Required |
| Helm chart + `cf-install` (with GPU node detection) | Required |
| E2E test including AI inference scenario | Required |
| Cilium + Contour networking | Required |
| cert-manager TLS management | Required |

### Important but Deferred (Phase 9)

| Capability | Reason for Deferral |
|---|---|
| ScyllaDB in CF-DBController | PostgreSQL + pgvector covers most SME AI and application needs first |
| DLQ and retry in CF-EventRouter | Adds eventing reliability; not required for MVP validation |
| GPU MIG partitioning | Requires specific GPU hardware; Phase 9 |
| Training job submission API | Complex; MinIO + manual Kubernetes Job is sufficient for early consumers |
| Model fine-tuning API | High-value but complex; deferred until base AI serving is validated |
| CF-DataPipeline (Airflow) | Specialist workload; Phase 9 |
| Consumer AI Agent SDK | Reference implementation; Phase 9 after platform is stable |
| Resource-based IAM policies | Identity-based covers MVP |
| Billing and metering hooks | Not needed until managed offering |
| Multi-cluster managed offering | Phase 9 |

---

## 10. Risks, Spikes, and Validation Checkpoints

### Risk 1: CF-IAM Complexity and Performance

**Risk:** CF-IAM is the most critical service. Underestimating policy model complexity or OPA latency blocks all other services.

**Mitigation:** Task 0.7 (OPA spike) mandatory before CF-IAM implementation. Implement against minimal policy model first; add advanced features in Phase 9.

**Validation checkpoint:** 500 req/s against `POST /iam/v1/authz/check` with 50-policy bundle; p99 < 5ms.

---

### Risk 2: NATS Account Provisioning Model

**Risk:** Dynamic NATS account provisioning may not support target tenant scale without cluster restarts.

**Mitigation:** Task 0.6 (NATS spike) validates dynamic account provisioning before Phase 5.

**Validation checkpoint:** Create 50 tenant accounts in sequence; each under 2 seconds; streams isolated.

---

### Risk 3: Knative Cold Start Under Resource Pressure

**Risk:** Scale-to-zero cold start > 5s in constrained clusters, unacceptable for event-triggered AI workloads.

**Mitigation:** Task 0.8 (Knative spike). If cold start is unacceptable, set minimum replicas = 1 for production AI-calling functions.

**Validation checkpoint:** Cold start after scale-to-zero < 3s on recommended hardware.

---

### Risk 4: vLLM / GPU Availability in Self-Hosted Deployments

**Risk:** Most self-hosted SME clusters will not have GPU nodes in v1. If the AI serving infrastructure only works with GPUs, it is useless for the majority of early adopters.

**Mitigation:** Ollama CPU-mode is deployed as a drop-in substitute when no GPU is present. The CF-AIRuntime model deployment API defaults to the Ollama `ServingRuntime` when no GPU node is detected. Consumers get a working AI inference endpoint on CPU; throughput is limited but functional for development and light production loads. Task 0.9 (GPU spike) validates the Ollama CPU substitution path explicitly, so it is confirmed before Phase 6.

**Validation checkpoint:** `cf-install validate` on a no-GPU cluster: deploy Ollama, run inference call, receive valid response. Pass before MVP release.

---

### Risk 5: OpenSearch Memory Footprint

**Risk:** OpenSearch is memory-hungry. A constrained self-hosted cluster may not sustain it alongside the rest of the platform.

**Mitigation:** Single-node OpenSearch with reduced JVM heap (4 GB) for `dev` and `small` deployment profiles. Document production observability cluster sizing clearly. Provide an alternative minimal logging path (Loki) for clusters that cannot afford OpenSearch.

**Validation checkpoint:** Measure OpenSearch memory under 1,000 log lines/sec on recommended hardware. Ensure it fits within the `small` profile budget.

---

### Risk 6: CF-EventRouter Rule Evaluation Correctness

**Risk:** The content-based routing rules engine is custom Go code; edge cases in pattern matching produce incorrect dispatch.

**Mitigation:** Formal pattern syntax specification in `docs/event-routing-patterns.md`. 100+ unit test cases for the matcher. `simulate` endpoint for tenant debugging.

**Validation checkpoint:** All documented pattern syntax test cases pass before Phase 5 is declared complete.

---

### Risk 7: Inference Proxy Streaming Correctness

**Risk:** The CF-AIRuntime inference proxy must correctly handle chunked-transfer / SSE streaming from vLLM to the consumer. Buffering the response body breaks the streaming experience.

**Mitigation:** Task 0.9 spike explicitly validates streaming end-to-end (vLLM → Go proxy → test client). Use `http.Flusher` interface correctly in the proxy handler; verify chunks arrive at the client without buffering.

**Validation checkpoint:** Stream 1,000 tokens from a deployed model through the CF-AIRuntime proxy to a Go test client. First token must arrive in < 200ms after request; subsequent tokens must arrive with < 50ms inter-token delay.

---

### Spikes Summary

| Spike | Task | Validates | Blocks if Failed |
|---|---|---|---|
| NATS multi-tenant routing | 0.6 | Account isolation, dynamic provisioning, routing feasibility | Phase 5 design |
| OPA embedded evaluation | 0.7 | Authz check latency, policy compilation model | Phase 1 CF-IAM design |
| Knative cold start | 0.8 | Function latency, minimum replica guidance | Phase 6 design |
| GPU scheduling + vLLM serving | 0.9 | GPU workload scheduling, vLLM serving, Ollama CPU fallback | Phase 6 AI runtime design |

All four spikes must complete and findings documented before the services they validate are designed and implemented.

---

## 11. Final Recommended Execution Order

A week-by-week staffing guide for a four-engineer team (E1–E4). AI-enabling tasks are marked with **[AI]**.

| Weeks | E1 + E2 Focus | E3 + E4 Focus | Key Milestones |
|---|---|---|---|
| 1–2 | Repo setup, CI/CD, shared libs (0.1–0.4) | OpenAPI toolchain, local cluster (0.3, 0.5) | Cluster running, CI green |
| 2–4 | OPA spike (0.7) + CF-IAM design | NATS spike (0.6) + Knative spike (0.8) + **GPU/vLLM spike (0.9) [AI]** | All spikes complete |
| 3–6 | Keycloak + OPA deploy + CF-IAM core (1.1–1.3) | OpenBao deploy + Prometheus/OTel bootstrap (1.4, 7.1 partial) | Identity plane working |
| 6–9 | CF-IAM complete + CF-SecretsConfig (1.3, 1.5) | CF-ResourceController with AI quota types (1.6) **[AI]** | Full identity/tenancy core |
| 8–11 | APISIX + CF-GatewayControl with streaming + AI proxy route type (2.1, 2.2) **[AI]** | MinIO + model artifact buckets + Storage API (3.1, 3.2) **[AI]** | API gateway up; storage with model buckets |
| 10–14 | CloudNativePG + pgvector + ScyllaDB (4.1, 4.2) **[AI]** | CF-DBController with pgvector default (4.3) **[AI]** + CLI (2.4) | Databases with vector search |
| 14–20 | NATS + CF-EventRouter with AI event patterns (5.1, 5.2, 5.3) **[AI]** | Knative + KServe/vLLM/Ollama deploy (6.1, 6.2) **[AI]** | Eventing + AI serving runtime |
| 20–26 | CF-FunctionTrigger (6.3) | CF-AIRuntime service (6.4) **[AI]** | Full compute layer: functions + AI serving |
| 22–28 | OpenSearch + CF-Observability with AI usage API (7.2, 7.3) **[AI]** | Full instrumentation + AI dashboards (7.4) **[AI]** | Full observability + AI telemetry |
| 26–30 | E2E tests with AI scenarios (8.1) **[AI]** + Helm chart (8.2) | `cf-install` with GPU detection (8.3) **[AI]** + Docs (8.4) **[AI]** | **MVP release** |
| 30+ | IAM hardening (9.1) + Eventing hardening (9.2) | Advanced AI: training jobs + GPU MIG (9.5) **[AI]** | Phase 9 hardening |

### Critical Path to MVP

```
0.1 → 0.7(spike) → 1.1 → 1.2 → 1.3 → 1.5 → 1.6
              ↓
         0.6(spike) → 5.1 → 5.2
              ↓
         0.8(spike) → 6.1 → 6.3
              ↓
         0.9(spike) → 6.2 → 6.4  ← CF-AIRuntime is ON the critical path to MVP
              ↓
    2.1 → 2.2 (streaming + AI proxy route)
    3.1 → 3.2 (model artifact buckets)
    4.1 (pgvector) → 4.3
    7.1 (minimal bootstrap)
    7.2 → 7.3 (AI usage API)
    8.1 → 8.2 → 8.3 → 8.4
         ↓
    MVP Release
```

CF-AIRuntime (Task 6.4) is on the MVP critical path. AI serving is not an afterthought — it is a delivery commitment for v1.

---

*End of Plan*

**Revision history:**  
v0.1 — Initial implementation plan, April 2026  
v1.0 — AI capabilities integrated as cross-cutting platform infrastructure throughout all phases; removed standalone Phase 8/9 AI phases; CF-AIRuntime promoted to Phase 6 alongside compute layer; pgvector integrated into Phase 4 database layer; model artifact storage integrated into Phase 3; AI workload identity integrated into Phase 1 CF-IAM; AI observability integrated into Phase 7 CF-Observability; AI serving placed on MVP critical path

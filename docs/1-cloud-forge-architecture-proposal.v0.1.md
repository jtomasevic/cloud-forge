# CloudForge: Open-Source Cloud Platform
## Architecture Proposal — v1.0 Internal Draft

**Status:** Draft for Architecture Review  
**Date:** April 2026  
**Audience:** Engineering Leadership, Platform Architects, Senior Engineers

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Problem Statement](#2-problem-statement)
3. [Goals and Non-Goals](#3-goals-and-non-goals)
4. [Platform Vision](#4-platform-vision)
5. [Service-by-Service Technology Recommendations](#5-service-by-service-technology-recommendations)
6. [Required Adapters, Plugins, and Control Plane Components](#6-required-adapters-plugins-and-control-plane-components)
7. [High-Level Architecture](#7-high-level-architecture)
8. [Control Plane and Data Plane Design](#8-control-plane-and-data-plane-design)
9. [IAM, Security, Secrets, and Tenancy](#9-iam-security-secrets-and-tenancy)
10. [ESS / Observability / Telemetry Design](#10-ess--observability--telemetry-design)
11. [AI Infrastructure Layer](#11-ai-infrastructure-layer)
12. [Self-Hosted vs Managed Offering Model](#12-self-hosted-vs-managed-offering-model)
13. [Phased Implementation Roadmap](#13-phased-implementation-roadmap)
14. [Risks, Tradeoffs, and Open Questions](#14-risks-tradeoffs-and-open-questions)
15. [Final Recommendation](#15-final-recommendation)

---

## 1. Executive Summary

CloudForge is an open-source cloud platform designed to serve small and medium enterprises that need production-grade managed infrastructure without depending on a hyperscaler. It operates as both a self-hosted on-premises platform and as a foundation for a future managed commercial offering.

The platform assembles a coherent set of cloud services — compute, messaging, storage, databases, identity, observability, and AI infrastructure — using strong open-source components as backends, unified under a shared control plane, consistent API model, and common identity and tenancy layer.

The platform's defining principle is that wherever open-source components do not natively provide the required behavior, CloudForge introduces purpose-built adapters, controllers, and platform APIs to close the gap. CloudForge is not a collection of installed tools. It is a platform with its own identity, API surface, and operational model, built on top of high-quality open-source foundations.

**On AI:** CloudForge provides AI infrastructure — not an AI product. The platform gives consumers the building blocks they need to build their own AI-powered applications, agents, workflows, and models: inference serving infrastructure, vector search in the database layer, model artifact storage, event-driven workflow integration, AI workload identity and secrets management, and AI-specific observability. Consumers assemble these into their own AI systems. The platform does not ship a centralized AI agent or AI application on their behalf.

**Recommended core stack for v1:**
- **Runtime:** Kubernetes (K3s for entry-level; full K8s for production)
- **Messaging/Eventing:** NATS JetStream + CF-EventRouter adapter
- **Functions:** Knative Serving + CF-FunctionTrigger adapter
- **AI Serving Runtime:** KServe + vLLM (GPU) + Ollama (CPU/dev) + CF-AIRuntime service
- **Object Storage:** MinIO
- **Vector Search:** pgvector extension on CloudNativePG (no separate vector database required)
- **NoSQL / KV:** ScyllaDB with DynamoDB-compatible API (Alternator)
- **Relational DB:** PostgreSQL via CloudNativePG operator
- **DB Proxy:** PgBouncer (platform-managed)
- **API Gateway:** Apache APISIX
- **Load Balancing / Ingress:** Cilium + Contour (Envoy-backed)
- **IAM:** Keycloak (identity/OIDC) + Open Policy Agent (authorization)
- **Secrets:** OpenBao (community fork of HashiCorp Vault)
- **Config Store:** OpenBao KV (unified with secrets layer)
- **Observability / ESS:** OpenSearch + OpenTelemetry Collector + Prometheus + Grafana

The platform requires nine custom adapters and services to be built as first-party components. These are the engineering investment that transforms a collection of tools into a coherent cloud platform.

---

## 2. Problem Statement

SMEs face a structural disadvantage when building modern software infrastructure. Hyperscalers provide excellent managed services, but their cost model, vendor lock-in, compliance constraints, and data sovereignty requirements make them inappropriate or unaffordable for many organizations — particularly those in regulated industries, sovereign infrastructure contexts, or cost-sensitive product stages.

The alternative — assembling open-source tools independently — requires deep specialist knowledge of a dozen different systems, each with its own operational model, API surface, security model, and upgrade cadence. There is no unifying control plane, no consistent tenant model, no shared identity layer, and no coherent developer experience. The result is bespoke infrastructure that becomes a liability rather than an asset.

The same problem applies to AI infrastructure. Organizations that want to build AI-powered products on open models face an additional fragmentation problem: inference servers, vector databases, training pipelines, model registries, and agent frameworks all need to be assembled, secured, and connected independently. The result is that the engineering cost of building AI infrastructure often exceeds the cost of the AI application itself.

CloudForge addresses both gaps: it provides managed cloud services with a coherent platform experience, and it provides the AI infrastructure primitives that consumers need to build their own AI systems — all within the same platform, using the same IAM model, the same observability layer, and the same provisioning interface.

---

## 3. Goals and Non-Goals

### Goals

- Provide a coherent, API-driven cloud platform experience for SMEs using open-source components as the service layer
- Support self-hosted on-prem deployment as a first-class operating model
- Design the platform to evolve into a managed commercial offering without architectural rewrites
- Build a consistent control plane, shared identity model, and unified tenancy/project model across all services
- Treat AI infrastructure as a first-class platform capability woven throughout the service layer — not as a standalone add-on
- Provide the infrastructure primitives that consumers need to build their own AI-powered applications, agents, workflows, and models
- Introduce custom adapters and controllers where OSS components do not natively provide the required behavior
- Maintain reasonable operational complexity for SME platform teams (three to five engineers can operate the platform)

### Non-Goals

- Building a centralized AI agent or AI application on behalf of consumers — CloudForge provides infrastructure, not AI products
- Achieving feature parity with AWS across all service categories
- Supporting AWS CLI or SDK compatibility as a primary interface
- Providing a Kubernetes distribution or managing the underlying OS layer
- Targeting hyperscaler-scale workloads in v1
- Building a full MLOps platform (model experiment tracking, A/B testing, feature stores) — these are consumer concerns, not platform primitives

---

## 4. Platform Vision

CloudForge is designed around five structural beliefs.

**1. The control plane is the product.**
The underlying OSS components are execution engines. The platform's value — tenant isolation, consistent IAM, unified observability, API uniformity, provisioning semantics — lives in the control plane. Building a good control plane is the primary engineering challenge.

**2. Adapters are first-class engineering artifacts.**
AWS-like behavior does not emerge naturally from any OSS project. The EventBridge-like routing model, the Lambda-like trigger semantics, the OpenAI-compatible inference proxy with metering — all require deliberate adapter and integration work. CloudForge treats these adapters as core platform components with defined API contracts, versioning, and independent test suites.

**3. The platform must have a coherent developer experience.**
Users interact with CloudForge through a single CLI, a single API gateway, and a single identity model — regardless of which underlying service they are using. They should not need to know that object storage is MinIO, that functions are Knative, or that inference is vLLM.

**4. AI infrastructure is platform infrastructure.**
AI compute (model serving), AI storage (model artifacts, training datasets), AI databases (vector search via pgvector), AI eventing (workflow orchestration via NATS), AI secrets (HuggingFace tokens, model API keys), and AI observability (token usage, GPU metrics, inference traces) are not separate AI features layered on top of the platform. They are the same compute, storage, database, eventing, secrets, and observability layers, extended to support AI workload types. When the platform's core layers are complete, it is already AI-capable. No separate AI phase is required.

**5. Consumers build AI systems; the platform provides the building blocks.**
CloudForge does not build an AI agent, an AI assistant, or an AI application. It provides the infrastructure primitives that enable consumers to build their own: an inference API their code can call, a vector store their RAG pipeline can write to, an event bus their AI workflow can publish on, a secrets store their model serving deployment can read from. The platform's job is to make all of these available, secured, observable, and properly isolated by tenant.

### Architectural Tenets

- **API-first:** Every platform capability is exposed through a versioned REST API. No service is accessible only via `kubectl`.
- **Kubernetes-native:** The platform runs on Kubernetes and uses Kubernetes primitives where appropriate — but does not expose raw Kubernetes to tenants.
- **Tenant isolation by default:** Every resource is owned by a tenant and project. Cross-tenant access requires explicit IAM policy grants.
- **Observable by default:** All platform services and all AI serving workloads emit structured logs, metrics, and traces. This is enforced, not optional.
- **Extension without forking:** Platform behavior can be extended through a defined plugin model without modifying core components.

---

## 5. Service-by-Service Technology Recommendations

### 5.1 Messaging and Eventing (SQS + EventBridge)

**Recommendation: NATS JetStream with CF-EventRouter adapter**

NATS JetStream is the correct foundation for both the SQS-equivalent (durable queue) and the EventBridge-equivalent (event bus with routing rules). It is a single system that eliminates the need for a separate message queue and event bus stack.

NATS JetStream provides persistent, durable, at-least-once delivery; consumer groups with competing consumer semantics equivalent to SQS standard queues; ordering guarantees equivalent to SQS FIFO; and a push/pull consumer model that supports both queue-like and pub/sub patterns.

NATS JetStream does not natively provide EventBridge-like behavior: content-based routing rules that evaluate message payload fields, dead-letter queues with retry policy, scheduled event sources, or rule-based dispatch to heterogeneous targets. These require the CF-EventRouter adapter (Section 6.1).

For AI workloads, NATS JetStream is the natural backbone for AI workflow orchestration: inference pipeline steps, training job lifecycle notifications, embedding pipeline triggers, and result fan-out are all native NATS patterns. No separate AI workflow messaging infrastructure is needed.

**Why not Kafka?** Operationally too heavy for SME contexts. Kafka is a reasonable future extension for data pipeline workloads but not the right foundation for a platform messaging layer.

**Why not RabbitMQ?** Weaker durability guarantees and operational model compared to JetStream for a platform messaging backbone.

| Criterion | NATS JetStream | Apache Kafka | RabbitMQ |
|---|---|---|---|
| Maturity | High | Very High | Very High |
| Kubernetes fit | Excellent | Good (Strimzi) | Good |
| Operational complexity | Low | High | Medium |
| SQS-like semantics | Native | Requires work | Partial |
| EventBridge-like routing | Requires adapter | Requires adapter | Partial |
| Multi-tenancy | Accounts model | Requires careful design | vHosts (coarse) |
| AI workflow orchestration | Native pub/sub | Native but heavy | Limited |
| Recommended for v1 | **Yes** | No | No |

---

### 5.2 Functions / Serverless Compute (Lambda)

**Recommendation: Knative Serving with CF-FunctionTrigger adapter**

Knative Serving runs natively on Kubernetes, supports scale-to-zero, handles cold start well, supports HTTP and gRPC function surfaces, and is actively maintained with strong commercial backing. Knative Eventing provides the trigger model that maps events to function invocations.

The gap between Knative and Lambda is real but manageable: Lambda provides a broader trigger ecosystem, richer function packaging, and tighter IAM integration. These are addressed by the CF-FunctionTrigger adapter (Section 6.2).

Apache OpenWhisk (the initial candidate in the brief) is not recommended. Its Kubernetes-native story is weak, community velocity has declined significantly, and its internal architecture (CouchDB-backed activation store) is operationally non-trivial. Knative is the correct choice.

| Criterion | Knative Serving | Apache OpenWhisk | OpenFaaS | Fission |
|---|---|---|---|---|
| Kubernetes fit | Native | Poor-Medium | Good | Good |
| Scale to zero | Yes | Yes | Yes | Yes |
| Event-driven triggers | Knative Eventing | Built-in | Limited | Limited |
| Community health | Strong | Declining | Moderate | Small |
| Recommended for v1 | **Yes** | No | No | No |

---

### 5.3 Object Storage (S3)

**Recommendation: MinIO**

MinIO is the unambiguous choice for S3-compatible object storage in a self-hosted context. It provides a complete, high-fidelity S3 API implementation, supports multi-tenancy via namespaced buckets and policies, scales from single-node to distributed mode, and has excellent Kubernetes operator support.

Beyond standard application storage, MinIO serves as the model artifact store for AI workloads: model weights, training datasets, adapter checkpoints, ONNX exports, and evaluation artifacts are all stored in MinIO buckets. The naming convention `models/{name}/{version}/` is recommended for consumer model storage. No separate model artifact store is required.

---

### 5.4 Data Processing / ETL / Analytics Orchestration (Glue)

**Recommendation: Apache Airflow (orchestration) + dbt (transformation) for v1; Apache Spark deferred to Phase 2**

For v1, the platform provides Apache Airflow for DAG-based workflow orchestration and dbt Core for SQL-based transformation pipelines. These cover the primary data pipeline use cases for SMEs.

Airflow is also the recommended orchestrator for consumer-built AI training data pipelines: DAGs that fetch data from MinIO, preprocess it, trigger training jobs, and archive outputs are native Airflow use cases.

Spark is deferred to Phase 2 due to the operational complexity of Spark-on-Kubernetes. The CloudForge Data Pipeline service wraps Airflow with platform-native IAM, tenant isolation, secret injection, and observability.

---

### 5.5 NoSQL / Key-Value / Document Database (DynamoDB)

**Recommendation: ScyllaDB with DynamoDB-compatible API (Alternator)**

ScyllaDB's Alternator module implements the DynamoDB HTTP API with high fidelity, including single-item operations, batch writes, GSIs, and TTL. ScyllaDB's performance profile (C++, shard-per-core architecture) significantly exceeds Cassandra and matches or exceeds DynamoDB at similar hardware levels. Its Kubernetes operator (Scylla Operator) is production-grade.

The primary gap is ScyllaDB DynamoDB Streams — Alternator does not fully implement Streams behavior natively. CF-EventRouter provides an equivalent by bridging ScyllaDB's CDC (Change Data Capture) to NATS JetStream in Phase 2.

| Criterion | ScyllaDB (Alternator) | Cassandra | MongoDB |
|---|---|---|---|
| DynamoDB API compatibility | High (Alternator) | None | None |
| Kubernetes operator | Yes (production) | Yes (K8ssandra) | Yes (Percona) |
| Performance | Exceptional | Good | Good |
| Operational complexity | Medium | High | Medium |
| Recommended for v1 | **Yes** | No | As Phase 2 alternative |

---

### 5.6 Relational Database (RDS) — Including Vector Search

**Recommendation: PostgreSQL via CloudNativePG operator, with pgvector pre-installed by default**

CloudNativePG is the best Kubernetes-native PostgreSQL operator available. It provides primary/replica HA configurations, automated failover, backup to S3-compatible storage (MinIO), PgBouncer connection pooling, and a clean CRD-based API.

A critical AI capability is built into this layer: **pgvector**. The pgvector PostgreSQL extension provides vector similarity search directly within PostgreSQL — supporting HNSW and IVFFlat indexes, cosine and dot-product distance functions, and integration with standard SQL. All CloudNativePG clusters provisioned by CF-DBController have pgvector pre-installed and enabled by default.

This design decision eliminates the need for a separate vector database service for the vast majority of consumer AI use cases. A consumer building a RAG pipeline, a semantic search system, or an embedding-based recommendation engine can use their already-provisioned PostgreSQL instance — with a `vector(1536)` column and an HNSW index — rather than provisioning and operating a separate Qdrant, Weaviate, or Milvus cluster. For SME-scale AI workloads, PostgreSQL with pgvector is sufficient and dramatically simpler.

A dedicated vector database service is not planned for the platform. If a consumer's workload outgrows pgvector (very high-dimensionality at very large scale), they can deploy Qdrant or Weaviate as a standard Kubernetes workload on the platform. The platform does not need to manage it.

MySQL support is recommended as a secondary option via Percona XtraDB Cluster Operator.

---

### 5.7 Database Proxy / Connection Pooling (RDS Proxy)

**Recommendation: PgBouncer (platform-managed)**

PgBouncer is the standard, battle-tested connection pooler for PostgreSQL. CloudForge manages PgBouncer instances as components co-located with database clusters, managed by the CloudNativePG integration layer. For MySQL, ProxySQL fills the equivalent role. This is transparent to tenants — the connection string they receive already points to the pooler.

---

### 5.8 Identity and Access Management (IAM)

**Recommendation: Keycloak (identity) + Open Policy Agent (authorization policy)**

IAM is an architecture, not a component choice. Two distinct systems are required:

1. **Identity and authentication:** Keycloak handles OIDC, OAuth2, SAML, LDAP integration, user management, service accounts, tenant/organization model, token issuance, and MFA. The API key model — long-lived bearer tokens for programmatic access to inference endpoints and storage APIs — is also managed through Keycloak client credentials mapped to API key identifiers.

2. **Authorization and policy:** Open Policy Agent evaluates fine-grained authorization decisions. Platform services call OPA's policy evaluation API to determine whether an authenticated principal has permission to perform an action on a resource. Policies are authored in Rego and compiled by CF-IAM.

The CF-IAM adapter (Section 6.3) wraps Keycloak and OPA behind a unified IAM API with AWS-like semantics: principals, policies, roles, permission boundaries, and service account delegation.

**AI workload identity** is a first-class IAM concern, not a special case. Model serving deployments, training jobs, embedding pipelines, and inference proxies all receive platform service accounts with scoped permissions. The IAM policy model includes AI-typed principal categories (`cf:ai:serving-endpoint`, `cf:ai:training-job`) with appropriate default permission scopes. Consumers use these identities to enforce least-privilege access for their AI workloads without writing custom authorization logic.

---

### 5.9 Secrets Management (Secrets Manager)

**Recommendation: OpenBao**

OpenBao is the community continuation of HashiCorp Vault following the BSL license change, under MPL 2.0. Its API is fully compatible with Vault, enabling use of existing Vault clients, the External Secrets Operator, and all standard Vault integrations.

OpenBao stores not only standard application secrets but also AI-specific credential types: HuggingFace access tokens for private model downloads, remote model provider API keys, and training dataset credentials for external data sources. These are standard KV secrets tagged with a type label (`cf:secret-type=hf-token`); CF-AIRuntime reads this label to inject the correct credential into model download jobs automatically.

---

### 5.10 Parameter / Configuration Store (SSM Parameter Store)

**Recommendation: OpenBao KV engine (unified with secrets layer)**

Rather than operating a separate parameter store service, CloudForge routes configuration parameters through OpenBao's KV v2 engine in a separate namespace from sensitive secrets. This eliminates operational duplication while providing versioning, access logging, and IAM integration. The CF-SecretsConfig adapter presents a higher-level API that distinguishes between parameters and secrets at the concept level, even though both are stored in OpenBao.

---

### 5.11 API Gateway (API Gateway)

**Recommendation: Apache APISIX**

Apache APISIX is Kubernetes-native (APISIX Ingress Controller), provides a rich plugin ecosystem, has a declarative configuration model that integrates cleanly with a programmatic control plane, and supports gRPC proxying.

A critical capability for AI workloads is **streaming response proxying**. LLM inference APIs return tokens progressively as Server-Sent Events or chunked HTTP transfer. APISIX correctly proxies these without buffering, preserving the streaming experience end-to-end. The CF-GatewayControl adapter provides a dedicated `ai-proxy` route type that pre-configures APISIX for AI inference traffic: streaming passthrough, API key authentication (not JWT, for programmatic clients), token-budget-based rate limiting, and per-request usage event emission.

| Criterion | Apache APISIX | Kong OSS | Envoy Gateway | Traefik |
|---|---|---|---|---|
| Kubernetes integration | Native | Good | Native | Native |
| Streaming proxy support | Yes | Yes | Yes | Yes |
| Control plane API | Excellent | Good | Moderate | Limited |
| Plugin ecosystem | Rich | Very rich | Growing | Good |
| Multi-tenancy | Route-level | Route-level | Route-level | Limited |
| Recommended for v1 | **Yes** | Alternative | Future | No |

---

### 5.12 Network Edge and Traffic Distribution (ALB / Load Balancer)

**Recommendation: Cilium (networking + L4 load balancing) + Contour (L7 ingress / Envoy-backed)**

Cilium's eBPF-based networking handles L4 load balancing natively and provides network policy enforcement with fine-grained control. It also delivers eBPF-level telemetry integrated with the observability stack. Contour (backed by Envoy Proxy) provides L7 ingress with the HTTPProxy model — a clean multi-tenant ingress model suitable for both application traffic and AI serving endpoint traffic.

GPU nodes require high-bandwidth network access for model weight loading and inter-node communication during distributed inference. Cilium's eBPF networking path reduces latency compared to kernel-based networking for I/O-intensive workloads.

---

### 5.13 Container Orchestration and Compute Runtime (EKS/Kubernetes)

**Recommendation: Kubernetes (K3s for small deployments; upstream Kubernetes for production); NVIDIA GPU Plugin for GPU scheduling**

Kubernetes is the compute foundation of the platform. For SME self-hosted deployments, K3s provides production-grade Kubernetes with reduced operational overhead. For larger or more demanding deployments, full upstream Kubernetes is appropriate. The platform is Kubernetes-distribution-agnostic.

**GPU compute scheduling** is a platform-level concern, not an AI afterthought. The NVIDIA GPU Device Plugin must be deployed on any node that will run AI inference or training workloads. Node labeling (`cloudforge.io/compute-class=gpu`) and Kubernetes node affinity rules on all AI serving and training workloads ensure GPU pods are scheduled on appropriate nodes without manual intervention.

The platform's resource model explicitly includes GPU resource types in the quota system: `ai.serving.gpu_millicores` and `ai.training.concurrent_jobs` are first-class quota dimensions in CF-ResourceController, the same as storage GB and database instance count.

---

### 5.14 Search, Logging, and Observability (ESS)

**Recommendation: OpenSearch + OpenTelemetry Collector + Prometheus + Grafana + Tempo**

OpenSearch (Apache 2.0, actively maintained) is the platform's centralized log store, search index, and event analytics backend. OpenTelemetry Collector is the universal telemetry ingestion layer in OTLP format. Prometheus handles metrics with alerting via Alertmanager. Grafana is the unified visualization layer. Tempo handles distributed trace storage.

OpenSearch is exposed as both an internal platform dependency and a managed platform service for tenant workloads. The CF-Observability adapter enforces tenant isolation in telemetry through index namespacing and IAM-governed query authorization.

**AI-specific telemetry** is part of the observability layer from v1, not an extension. OpenSearch index templates include `cf-{tenant}-{project}-ai-infer-*` for per-inference-request records (model, token counts, latency, status) and `cf-{tenant}-{project}-ai-agent-*` for consumer agent execution traces. GPU utilization metrics are scraped from vLLM's Prometheus metrics family and displayed in standard Grafana dashboards alongside other platform metrics.

This is addressed in depth in Section 10.

---

### 5.15 AI Serving Runtime (Inference Infrastructure)

**Recommendation: KServe as the orchestration layer; vLLM as the GPU inference runtime; Ollama as the CPU/development runtime**

**This is a compute infrastructure capability, not a product feature.** The AI serving runtime is deployed as part of the compute layer alongside Knative Serving. It provides the execution environment for consumer-deployed models, the same way Knative provides the execution environment for consumer-deployed functions.

**KServe** is the Kubernetes-native model lifecycle manager. It manages `InferenceService` CRDs that describe a model's serving configuration: the model source (MinIO path or HuggingFace model ID), the serving runtime (vLLM or Ollama), resource requirements (GPU, memory), autoscaling configuration, and routing. CF-AIRuntime (Section 6.9) manages KServe `InferenceService` objects on behalf of tenants, the same way CF-FunctionTrigger manages Knative `Service` objects.

**vLLM** is the production GPU inference engine. It provides an OpenAI-compatible REST API, continuous batching via PagedAttention, tensor parallelism for multi-GPU serving, and support for all major open model families (Llama, Mistral, Qwen, Phi, Gemma, etc.). Consumers get an OpenAI-compatible `/v1/chat/completions` and `/v1/embeddings` endpoint for any model they deploy.

**Ollama** is the CPU/development runtime. Consumers without GPU hardware, and the local development environment for all engineers, use Ollama. It supports the same OpenAI-compatible API surface. Throughput is limited compared to vLLM on GPU, but it is fully functional and enables development, testing, and low-traffic production workloads.

**No-GPU path:** When the cluster has no GPU nodes, CF-AIRuntime defaults all model deployments to the Ollama runtime. The API surface is identical. Consumers write code against the OpenAI-compatible endpoint and the runtime underneath (vLLM vs Ollama) is an infrastructure concern they should not need to care about.

| Criterion | vLLM | Ollama | Triton | Text Generation Inference |
|---|---|---|---|---|
| OpenAI API compatibility | Full | Full | Partial | Full |
| GPU utilization | Exceptional | N/A (CPU) | Excellent | Excellent |
| Kubernetes fit | Good (KServe) | Adequate | Good | Good |
| Operational complexity | Medium | Low | High | Medium |
| Streaming support | Yes | Yes | Partial | Yes |
| No-GPU path | No | Yes | No | No |
| Recommended for prod/GPU | **Yes** | Dev/no-GPU | Phase 2 | Alternative |

---

### 5.16 Consumer AI Patterns and Reference Materials

**Framing note:** This section is not about an agent framework that the platform ships as a product. It describes the patterns and reference materials the platform provides to help consumers build their own AI systems using platform primitives.

A consumer building an AI agent, a RAG pipeline, a fine-tuning workflow, or an AI-powered SaaS product needs guidance on how platform services compose to support their workload. CloudForge provides this through:

1. **A Go agent SDK** (`pkg/agent/`) — a lightweight library (not a framework) providing typed wrappers around platform APIs: NATS event trigger subscription, CF-AIRuntime inference client, CF-SecretsConfig secret access, MinIO artifact storage, and OTel trace emission. Consumers import this library to integrate their agent or workflow with platform services; they are not locked into any particular agent execution framework.

2. **A Python integration guide** — Python is the dominant language for AI workloads. The platform generates a Python client from the same OpenAPI specs used for the Go CLI. Python consumers can call the CloudForge AI API, Events API, and Storage API from LangGraph agents, CrewAI workflows, or any other Python AI framework using the generated client.

3. **Reference architectures** in `docs/ai/` — documented, runnable examples for the most common consumer AI patterns: RAG pipeline, event-triggered inference pipeline, fine-tuning workflow, embedding generation on document upload. Each reference architecture uses only platform-native primitives.

The platform does not prescribe which agent framework consumers must use. LangGraph, CrewAI, AutoGen, custom state machines — all are valid. The platform provides the infrastructure they run on top of.

---

## 6. Required Adapters, Plugins, and Control Plane Components

This section defines the custom platform engineering required to transform the selected OSS components into a coherent platform. These are first-party CloudForge components, not optional integrations.

---

### 6.1 CloudForge Event Router (CF-EventRouter)

**Problem it solves:** NATS JetStream provides durable messaging but not EventBridge-like routing semantics: content-based event filtering, multi-target fan-out, rule-based dispatch to heterogeneous targets, dead-letter policies, and retry envelopes.

**Responsibilities:**
- Consume events from NATS JetStream streams
- Evaluate event content against configurable routing rules (JSON pattern matching on CloudEvents fields and payload)
- Dispatch matched events to configured targets: Knative functions, NATS subjects, external HTTP endpoints, CF-AIRuntime inference endpoints
- Implement retry logic with configurable backoff and dead-letter queue delivery
- Enforce IAM authorization on event bus access
- Emit structured routing metrics and traces to the observability layer
- Support AI workflow event type patterns: `cf.ai.inference.request.completed`, `cf.ai.model.deployed`, `cf.ai.training.job.finished`, `cf.storage.object.created` — these are standard CloudEvents matched by content, not by special routing logic

**API/Contract:**
- Management API (REST): CRUD for event buses, rules, and targets
- Runtime protocol: CloudEvents v1.0 envelope
- `simulate` endpoint: accepts a test event payload, returns which rules match and which targets would be dispatched (for consumer debugging)
- OPA integration for bus access control

**Control plane or data plane:** Control plane for rule management; data plane for event dispatch.

---

### 6.2 CloudForge Function Trigger Adapter (CF-FunctionTrigger)

**Problem it solves:** Knative Serving and Eventing do not provide the full Lambda-like trigger model: SQS queue triggers, S3 event triggers, scheduled triggers, and NATS JetStream pull-consumer triggers with concurrency control and batch sizing.

**Responsibilities:**
- Bridge NATS JetStream consumers to Knative function invocations
- Bridge MinIO S3-compatible event notifications to function triggers
- Implement scheduled/cron triggers via Kubernetes CronJob management
- Inject IAM context (tenant, caller identity, delegated permissions) as signed headers into function invocation envelopes
- Implement concurrency controls and throttling
- Collect function execution metrics and forward to the observability layer

**API/Contract:**
- CRD: `CloudForgeFunction` (wraps a `Ksvc` with CF metadata)
- CRD: `FunctionTrigger` (declarative trigger source → function target configuration)
- CloudEvents-compatible invocation envelope

**Control plane or data plane:** Control plane for CRD management; data plane for trigger dispatch.

---

### 6.3 CloudForge IAM Adapter (CF-IAM)

**Problem it solves:** Keycloak provides identity and authentication; OPA provides authorization policy evaluation. Neither provides the AWS-like IAM programming model: named policies, role attachment, service account delegation, permission boundaries, and cross-service authorization checks with a consistent API.

**Responsibilities:**
- Provide an IAM management API: create/update/delete policies, roles, users, service accounts, and role attachments
- Translate CloudForge IAM policy documents into OPA Rego policies and sync them to the OPA bundle store
- Manage Keycloak realm configuration for tenant onboarding
- Implement assume-role semantics via Keycloak token exchange with OPA policy guard
- Issue and validate API keys for programmatic access to inference endpoints and storage APIs
- Serve as the authorization check endpoint for all platform service adapters
- Maintain an audit log of all IAM mutations and authorization decisions (forwarded to OpenSearch)

**AI workload identity:** CF-IAM provides first-class service account types for AI workloads (`cf:ai:serving-endpoint`, `cf:ai:training-job`). These are standard service accounts with scope-limited default policies. CF-AIRuntime assigns one of these service account types to every model deployment and training job it creates, ensuring AI workloads have appropriate and auditable platform identities.

**API/Contract:**
- CloudForge IAM API (REST): `/iam/v1/policies`, `/iam/v1/roles`, `/iam/v1/users`, `/iam/v1/service-accounts`, `/iam/v1/api-keys`
- Internal gRPC endpoint: `AuthzCheck(principal, action, resource) → (allowed bool, reason string)`

**Control plane or data plane:** Control plane only.

---

### 6.4 CloudForge Secrets and Config Adapter (CF-SecretsConfig)

**Problem it solves:** OpenBao provides secret storage but not the AWS Secrets Manager or SSM Parameter Store developer experience: tenant-scoped namespacing, rotation hooks, or a platform API that enforces isolation without exposing OpenBao path internals.

**Responsibilities:**
- Provide a unified Secrets API that abstracts OpenBao paths behind a tenant-aware namespace model
- Provide a Parameters API over OpenBao KV for non-sensitive configuration values
- Integrate with CloudNativePG for automated database credential rotation
- Integrate with CF-IAM for access control: secret access requires IAM policy authorization
- Support AI-specific secret types: `cf:secret-type=hf-token` (for CF-AIRuntime model downloads), `cf:secret-type=model-api-key` (for external model provider access)
- Provide secret injection via init container for Kubernetes workloads
- Emit audit log entries for all secret access and rotation events

**Control plane or data plane:** Control plane for management API; data plane for injection and rotation delivery.

---

### 6.5 CloudForge Database Provisioning Controller (CF-DBController)

**Problem it solves:** CloudNativePG and Scylla Operator are excellent operators, but they do not provide a unified, tenant-scoped CloudForge database provisioning API.

**Responsibilities:**
- Expose a CloudForge Database API: create/modify/delete database instances, backup policies, restore operations
- Translate CloudForge database requests into CloudNativePG `Cluster` CRDs and Scylla `ScyllaCluster` CRDs
- Enforce that all PostgreSQL clusters have the pgvector extension enabled by default (via `shared_preload_libraries` and `CREATE EXTENSION IF NOT EXISTS vector` post-provision)
- Enforce tenant resource quotas
- Manage automatic backup scheduling to MinIO
- Inject database credentials into OpenBao on provision and manage rotation lifecycle
- Emit provisioning lifecycle events to CF-EventRouter

**Control plane or data plane:** Control plane.

---

### 6.6 CloudForge API Gateway Control Adapter (CF-GatewayControl)

**Problem it solves:** APISIX provides a powerful gateway, but configuring it programmatically from a multi-tenant platform requires a translation layer with tenant isolation enforcement.

**Responsibilities:**
- Translate CloudForge API Gateway route definitions into APISIX route, upstream, and plugin configurations
- Enforce tenant namespace isolation in route configuration
- Integrate APISIX authentication plugins with CF-IAM: JWT tokens and API keys both validated against CF-IAM
- Provide an `ai-proxy` route type that pre-configures APISIX for AI inference traffic: streaming passthrough, API key auth, token-budget rate limiting, per-request usage event emission to CF-EventRouter
- Manage TLS certificate lifecycle via cert-manager integration
- Register routes automatically on behalf of CF-AIRuntime when a model serving deployment is created (no manual route configuration required for AI endpoints)

**Control plane or data plane:** Control plane for route management; data plane is APISIX itself.

---

### 6.7 CloudForge Observability Adapter (CF-Observability)

**Problem it solves:** Platform services emit telemetry in multiple formats. OpenSearch, Prometheus, and Tempo need data delivered in their respective formats. The platform must enforce tenant isolation in telemetry, manage index lifecycle, expose a structured log and trace query API, and ingest AI-specific telemetry without requiring consumers to understand OpenSearch.

**Responsibilities:**
- Manage OpenTelemetry Collector configuration and routing rules
- Enforce tenant isolation in OpenSearch indices (tenant-scoped index naming, IAM-backed query authorization)
- Manage index lifecycle policies (retention, rollover, archival to MinIO)
- Expose a CloudForge Logs API for tenant log queries (structured query, not raw Lucene/DSL)
- Expose a CloudForge AI Usage API: token usage by model by day per tenant/project, backed by aggregation queries against AI inference index
- Expose a CloudForge AI Traces API: structured query against inference request records and consumer agent execution traces
- Manage Grafana datasource and dashboard provisioning per tenant
- Scrape and expose AI-specific metrics: vLLM metrics family (`vllm:*`) into Prometheus; GPU utilization via DCGM Exporter

**Control plane or data plane:** Control plane for configuration and API; data plane for the telemetry pipeline.

---

### 6.8 CloudForge Resource and Provisioning Controller (CF-ResourceController)

**Problem it solves:** CloudForge needs a top-level resource model: tenants, projects, resource quotas, resource inventory, and a consistent provisioning lifecycle across all services.

**Responsibilities:**
- Manage the tenant and project hierarchy
- Enforce resource quotas across all provisioned resources — including AI-specific quota dimensions: `ai.serving.deployments`, `ai.serving.gpu_millicores`, `ai.training.concurrent_jobs`, `storage.model_artifacts_gb`
- Provide a unified resource API: list all resources in a project, tag resources, enforce lifecycle policies
- Manage the provisioning state machine for multi-step resource creation
- Provide billing hooks (Phase 2) for metered resource usage including AI token consumption

**Control plane or data plane:** Control plane only.

---

### 6.9 CloudForge AI Runtime Service (CF-AIRuntime)

**Problem it solves:** KServe, vLLM, and Ollama are excellent execution engines, but they do not provide a tenant-scoped, IAM-governed, quota-enforced, metered AI serving API. Consumers should not interact with KServe CRDs directly, the same way they do not interact with CloudNativePG CRDs directly.

**Responsibilities:**
- Provide a model registry: consumers register models by name with a source (MinIO path or HuggingFace model ID), runtime preference (vLLM or Ollama), and hardware requirements
- Manage model download jobs: trigger a Kubernetes Job to pull model weights from HuggingFace using the tenant's stored `hf-token`, place weights in the tenant's MinIO model bucket
- Manage serving deployments: create/update/delete KServe `InferenceService` CRDs based on tenant deployment requests; select runtime (vLLM on GPU nodes, Ollama on CPU nodes) based on cluster capability
- Expose an OpenAI-compatible inference proxy: validate API key via CF-IAM, route request to the correct `InferenceService` endpoint, intercept response to count tokens, emit `cf.ai.inference.request.completed` CloudEvent to CF-EventRouter, record usage in CF-ResourceController
- Enforce token budget quotas per project: reject inference requests when a project's token budget is exhausted
- Register routes automatically via CF-GatewayControl when a deployment is created: consumer's model endpoint is immediately accessible at a public URL with no additional configuration
- Support batch inference jobs: Kubernetes Job-based offline inference over datasets in MinIO

**APIs/Contract:**
- Model registry: `POST /ai/v1/{tenant}/{project}/models`
- Serving deployments: `POST /ai/v1/{tenant}/{project}/deployments`
- Inference proxy: `POST /ai/v1/{tenant}/{project}/infer/{deployment}/v1/chat/completions`
- Embeddings proxy: `POST /ai/v1/{tenant}/{project}/infer/{deployment}/v1/embeddings`
- Model download: `POST /ai/v1/{tenant}/{project}/models/{name}/pull`

**Control plane or data plane:** Control plane for registry, deployment management, and quota enforcement; data plane for the inference proxy and batch job execution.

**Operational risks:** Streaming proxy correctness (must not buffer LLM streaming responses); cold start latency of large models on first invocation; GPU node availability for vLLM deployments; model weight download time for large models.

---

## 7. High-Level Architecture

The platform is organized into four layers:

```
┌──────────────────────────────────────────────────────────────────────────┐
│                           PLATFORM SURFACE                               │
│   CloudForge CLI  │  CloudForge Web Console  │  CloudForge API (REST)   │
└─────────────────────────────┬────────────────────────────────────────────┘
                              │
┌─────────────────────────────▼────────────────────────────────────────────┐
│                          CONTROL PLANE                                   │
│                                                                          │
│  CF-ResourceController   CF-IAM             CF-SecretsConfig             │
│  CF-DBController         CF-GatewayControl  CF-EventRouter (mgmt)        │
│  CF-FunctionTrigger (mgmt)                  CF-AIRuntime (mgmt)          │
│  CF-Observability                                                        │
│                                                                          │
│  [State: CloudNativePG, OpenBao, OPA bundles in MinIO]                  │
└─────────────────────────────┬────────────────────────────────────────────┘
                              │
┌─────────────────────────────▼────────────────────────────────────────────┐
│                           DATA PLANE                                     │
│                                                                          │
│  ┌─────────────────────────────────┐  ┌───────────────────────────────┐ │
│  │         COMPUTE LAYER           │  │       MESSAGING LAYER         │ │
│  │                                 │  │                               │ │
│  │  Knative Serving/Eventing       │  │  NATS JetStream               │ │
│  │  CF-FunctionTrigger (runtime)   │  │  CF-EventRouter (runtime)     │ │
│  │                                 │  │                               │ │
│  │  KServe + vLLM (GPU nodes)      │  └───────────────────────────────┘ │
│  │  KServe + Ollama (CPU nodes)    │                                     │
│  │  CF-AIRuntime (inference proxy) │  ┌───────────────────────────────┐ │
│  └─────────────────────────────────┘  │        DATA LAYER             │ │
│                                       │                               │ │
│  ┌─────────────────────────────────┐  │  MinIO (object + model store) │ │
│  │         INGRESS LAYER           │  │  CloudNativePG + pgvector     │ │
│  │                                 │  │  ScyllaDB (Alternator)        │ │
│  │  Apache APISIX                  │  │  PgBouncer                    │ │
│  │  Contour + Envoy                │  └───────────────────────────────┘ │
│  │  Cilium (L4 + network policy)   │                                     │
│  └─────────────────────────────────┘                                     │
└─────────────────────────────┬────────────────────────────────────────────┘
                              │
┌─────────────────────────────▼────────────────────────────────────────────┐
│                  OBSERVABILITY AND SECURITY LAYER                        │
│                                                                          │
│  OpenSearch              Prometheus + Alertmanager                       │
│  Grafana + Tempo         OpenTelemetry Collector                         │
│  Cilium (network policy) cert-manager                                    │
│  OpenBao (secrets)       Keycloak (identity)                             │
│  Open Policy Agent       DCGM Exporter (GPU metrics)                    │
└──────────────────────────────────────────────────────────────────────────┘
```

**Ingress flow:** All external traffic enters through APISIX. APISIX enforces authentication (JWT validation for interactive clients; API key validation for programmatic and AI inference clients) before forwarding. AI inference requests are routed via the `ai-proxy` route type to CF-AIRuntime's inference proxy, which validates the API key, routes to the correct KServe endpoint, and streams the response back.

**AI inference flow:** Consumer deploys a model via `cf ai deploy` → CF-AIRuntime creates `InferenceService` CRD → KServe schedules vLLM or Ollama pod on appropriate node → CF-GatewayControl creates APISIX route → consumer calls endpoint with API key → APISIX → CF-AIRuntime proxy → vLLM/Ollama → streaming response → token count emitted to CF-EventRouter → usage recorded in CF-ResourceController.

**Event-driven AI flow:** Consumer uploads document to MinIO → MinIO event notification → CF-FunctionTrigger → embedding function invoked → function calls CF-AIRuntime embeddings endpoint → embeddings stored in PostgreSQL pgvector column → similarity search queries available immediately.

---

## 8. Control Plane and Data Plane Design

### Control Plane

The CloudForge control plane is a collection of cooperating microservices deployed as a dedicated workload on the platform Kubernetes cluster. It owns resource definitions, enforces policies, and drives the state of the data plane toward the desired configuration.

**Control plane services:**

| Service | Primary Responsibility | State Backend |
|---|---|---|
| CF-ResourceController | Tenant/project lifecycle, quotas (including AI quotas), resource inventory | PostgreSQL |
| CF-IAM | Policy management, Keycloak sync, OPA bundle management, API key issuance | PostgreSQL + OPA bundle store (MinIO) |
| CF-SecretsConfig | Secret and parameter CRUD, rotation, AI credential injection | OpenBao |
| CF-DBController | Database provisioning via operator CRDs; pgvector enforcement | Kubernetes API + PostgreSQL |
| CF-GatewayControl | APISIX route management; AI proxy route configuration | APISIX etcd + CF state |
| CF-EventRouter (control) | Event bus and rule CRUD; AI workflow event patterns | PostgreSQL |
| CF-FunctionTrigger (control) | Trigger configuration, function registry | PostgreSQL + Kubernetes CRDs |
| CF-AIRuntime (control) | Model registry, deployment management via KServe, quota enforcement | PostgreSQL + Kubernetes CRDs |
| CF-Observability | Collector config, index lifecycle, tenant datasources, AI usage aggregation | OpenSearch + Prometheus |

The control plane exposes a unified CloudForge API through APISIX, handling authentication and routing to the appropriate service.

**High-availability:** Each control plane service runs as a minimum of two replicas. Leader election via Kubernetes Lease objects is used for services with reconciliation loops (CF-DBController, CF-FunctionTrigger, CF-AIRuntime). The control plane's PostgreSQL is managed by CloudNativePG with automated failover.

### Data Plane

The data plane consists of the service backends and the runtime components of the adapters. The data plane operates independently of control plane availability for steady-state workload execution.

**Critical principle: control plane outages must not interrupt running workloads.** NATS continues routing messages, Knative continues serving functions, vLLM continues serving inference requests, and MinIO continues serving objects if the CloudForge control plane is temporarily unavailable. New provisioning operations will queue; running infrastructure continues.

**Data plane isolation between tenants:**
- NATS JetStream: tenant-scoped accounts with isolated stream namespaces
- Kubernetes: tenant workloads in dedicated namespaces with Cilium network policies
- MinIO: per-tenant bucket policies and IAM policies
- ScyllaDB: per-tenant keyspaces
- CloudNativePG: per-tenant database clusters in isolated namespaces
- KServe `InferenceService` deployments: per-tenant Kubernetes namespace with network policies preventing cross-tenant inference endpoint access

---

## 9. IAM, Security, Secrets, and Tenancy

### Tenancy Model

CloudForge uses a three-level hierarchy: **Tenant → Project → Resource**.

- **Tenant** corresponds to an organization or team. It maps to a Keycloak realm (one realm per tenant in v1; migrated to Keycloak Organizations model in Phase 2 for managed offering scale).
- **Project** is a logical grouping of resources within a tenant. Resources within a project share networking and default IAM trust.
- **Resource** is any provisioned service: database, function, event bus, storage bucket, AI model deployment, etc.

Resource identifiers follow the pattern: `cf://{tenant}/{project}/{service}/{resource-id}`.

### Authentication

All API access is authenticated using JWT tokens issued by Keycloak for interactive users and service-to-service calls. API keys (long-lived bearer tokens) are issued by CF-IAM for programmatic clients and for AI inference endpoint access — scenarios where full OIDC flows are impractical.

Tenant workloads receive platform identity via Kubernetes service account token projection, automatically rotated. AI serving deployments (`InferenceService` pods) receive workload identity tokens scoped to the `cf:ai:serving-endpoint` service account type, granting them read access to their model artifacts in MinIO and read access to their registered secrets in CF-SecretsConfig — and nothing else.

### Authorization

OPA is the policy decision point. Platform services call OPA's REST API synchronously. Policies are compiled from CloudForge IAM policy documents by CF-IAM and distributed as signed bundles to OPA instances via MinIO.

CloudForge IAM policy format:

```json
{
  "effect": "allow",
  "principal": "cf://acme-corp/project-prod/service-account/order-processor",
  "actions": ["cf:events:publish", "cf:storage:get", "cf:ai:infer"],
  "resources": [
    "cf://acme-corp/project-prod/events/order-bus/*",
    "cf://acme-corp/project-prod/storage/documents/*",
    "cf://acme-corp/project-prod/ai/deployment/llm-prod"
  ],
  "conditions": {}
}
```

The `cf:ai:infer` action is a first-class IAM action type, not a special case. Consumers control which service accounts and users can call which inference deployments through standard CloudForge IAM policies.

### Secrets and Credential Management

OpenBao is the root of trust for all secrets:

- **Tenant application secrets:** Credentials, API keys, third-party tokens in OpenBao KV
- **AI credentials:** HuggingFace tokens, remote model provider keys, training dataset credentials — standard KV secrets with `cf:secret-type` labels
- **Database credentials:** Dynamically generated by OpenBao database secret engine, short-lived and automatically rotated
- **Platform internal credentials:** Service-to-service mTLS certificates issued by OpenBao PKI, 24-hour TTL
- **Encryption keys:** Data-at-rest keys for object storage and database backups via OpenBao Transit
- **Model artifact encryption:** Model weights stored in MinIO can optionally be encrypted at rest using OpenBao Transit keys, configured at bucket creation time

### Network Security

Cilium enforces default-deny L3/L4 and L7 network policies. Tenant namespaces cannot communicate with each other or with the control plane namespace without explicit policy grants. GPU nodes have additional ingress policies that allow only the KServe inference router and CF-AIRuntime proxy to initiate connections to inference pods — preventing direct tenant pod-to-pod access to inference endpoints.

---

## 10. ESS / Observability / Telemetry Design

### Architecture

The observability stack is built around three signal types — logs, metrics, and traces — unified under OpenTelemetry. All platform services and all AI serving workloads emit telemetry to this layer from day one.

```
Platform Services + Consumer Workloads + AI Serving Pods
           │
           ▼ (OTLP)
OpenTelemetry Collector (DaemonSet + gateway)
           │
    ┌──────┴──────────────────────┐
    ▼                             ▼                     ▼
OpenSearch                   Prometheus            Grafana Tempo
(logs, AI inference records, (metrics + alerts,    (distributed
 AI agent traces, analytics)  GPU utilization,      traces)
                               token throughput)
    │                             │
    └─────────────────────────────┘
                  │
               Grafana
    (platform dashboards + per-tenant dashboards)
```

### OpenSearch as the Platform ESS Backend

OpenSearch is exposed as both a platform-internal dependency and a managed tenant service. Index naming enforces isolation:

- `cf-platform-*` — platform operational logs, audit trails. Platform operators only.
- `cf-{tenant}-{project}-app-*` — tenant application logs.
- `cf-{tenant}-{project}-ai-infer-*` — one document per inference request: model name, deployment ID, prompt token count, completion token count, latency ms, status, tenant, project, timestamp.
- `cf-{tenant}-{project}-ai-agent-*` — consumer agent execution traces (consumers who emit structured trace events via the agent SDK or via OTel directly).

### What the Platform Collects

**From all platform services:** structured logs, request latency histograms, error rates, queue depths, database connection pool utilization, storage operation latency, Knative function cold start times and invocation counts.

**From CF-EventRouter and CF-FunctionTrigger:** per-rule match rates, delivery success/failure rates, DLQ depths, retry counts.

**From AI serving (vLLM via Prometheus, KServe via OTel):**
- Time-to-first-token histogram (p50, p95, p99)
- Token throughput (tokens/sec per deployment)
- Inference request count and error rate per deployment
- GPU memory utilization per node
- GPU compute utilization per node
- Model loading time on cold start
- Queue length (pending inference requests)

These are standard Prometheus metrics emitted by vLLM (`vllm:*` metrics family) and scraped by Prometheus. They are available in Grafana dashboards alongside application-level metrics — no separate AI monitoring system is required.

**From the Kubernetes cluster:** node and pod resource utilization, GPU scheduling events, Kubernetes API server latency.

### AI Usage and Billing API

CF-Observability provides a CloudForge AI Usage API that aggregates token counts from the `ai-infer-*` OpenSearch index and returns structured usage summaries per tenant/project. This API is used by:

- Consumers: monitor their AI workload cost and usage
- CF-ResourceController: enforce token budget quotas
- The billing layer (Phase 2): compute per-tenant AI charges for the managed offering

The usage API is a structured REST endpoint — consumers do not need to query OpenSearch directly.

### Observability as a Managed Service

Tenants query their application logs and AI traces through the CloudForge Logs API and AI Traces API (backed by OpenSearch) without direct OpenSearch access. A structured query API is exposed for common patterns. Power users can be granted scoped direct OpenSearch access via CF-IAM policy.

Alerting is configured through the CloudForge Alerting API (backed by Alertmanager), with notification channels managed through CF-SecretsConfig.

---

## 11. AI Infrastructure Layer

### Design Principle

CloudForge provides AI infrastructure — not an AI product. The platform's job is to ensure that when a consumer wants to build an AI application, agent, or workflow, every infrastructure primitive they need is already available, secured, observable, and properly isolated within their tenant context.

The AI infrastructure is not a separate layer bolted on top of the platform. It is woven into every service layer:

| Platform Layer | AI Infrastructure Capability |
|---|---|
| Identity (CF-IAM) | AI workload identity types; API key model for inference access; IAM actions for `cf:ai:infer` and `cf:ai:deploy` |
| Secrets (CF-SecretsConfig) | HuggingFace tokens; model API keys; training dataset credentials; automatic injection into model download jobs |
| Compute (Knative + KServe) | Peer compute workload types: functions and model serving deployments are managed by the same platform layer |
| Storage (MinIO) | Model artifact store; training dataset store; ONNX/checkpoint/adapter storage; model weight downloads from HuggingFace |
| Database (CloudNativePG + pgvector) | Vector similarity search built into every PostgreSQL instance; no separate vector database required |
| Eventing (NATS + CF-EventRouter) | AI workflow orchestration backbone; documented AI workflow event types; fan-out to multiple pipeline consumers |
| API Gateway (APISIX + CF-GatewayControl) | AI-proxy route type with streaming, API key auth, token-budget rate limiting; auto-registration of inference endpoints |
| Observability (OpenSearch + Prometheus) | Inference request telemetry; GPU utilization; token usage per tenant; AI trace query API |

### Model Serving Infrastructure

CF-AIRuntime manages the model serving lifecycle. When a consumer deploys a model:

1. CF-AIRuntime validates the request against CF-ResourceController quota (`ai.serving.deployments`, `ai.serving.gpu_millicores`)
2. CF-AIRuntime creates a KServe `InferenceService` object in the tenant's namespace, specifying the runtime (vLLM for GPU, Ollama for CPU) and model source (MinIO path)
3. KServe schedules the inference pod on an appropriate node
4. CF-GatewayControl is called to register an APISIX route for the endpoint
5. CF-IAM creates a service account with `cf:ai:serving-endpoint` type bound to the deployment
6. The deployment status transitions to `READY` and the consumer receives the endpoint URL

The consumer's application calls the endpoint using an API key (issued via CF-IAM) at the standard OpenAI-compatible path. The runtime underneath (vLLM or Ollama) is invisible to the consumer's code.

### Vector Search Infrastructure

pgvector is pre-installed on all CloudNativePG-managed PostgreSQL clusters. No separate provisioning step is required. A consumer creates a table with a vector column using standard SQL:

```sql
CREATE TABLE documents (
    id UUID PRIMARY KEY,
    content TEXT,
    embedding vector(1536)
);
CREATE INDEX ON documents USING hnsw (embedding vector_cosine_ops);
```

Similarity search:

```sql
SELECT id, content
FROM documents
ORDER BY embedding <=> $1
LIMIT 10;
```

This is sufficient for RAG systems, semantic search, embedding-based classification, and recommendation workloads at SME scale. If a consumer's workload grows beyond what pgvector handles at acceptable latency, they can deploy a dedicated vector store (Qdrant, Weaviate) as a standard Kubernetes workload on the platform. The platform does not need to manage it.

### Consumer AI Patterns

When the platform's core layers are operational, consumers can build:

**RAG Pipeline (document Q&A, knowledge base, semantic search)**
- Documents stored in MinIO (Storage API)
- MinIO upload event triggers an embedding function (CF-FunctionTrigger)
- Function calls the consumer's deployed embedding model (CF-AIRuntime inference endpoint)
- Embeddings stored in PostgreSQL pgvector column (CF-DBController)
- Query time: retrieve top-k embeddings via pgvector similarity search, pass context to LLM, return answer

**Event-Driven Inference Pipeline**
- Application publishes event to NATS (CF-EventRouter)
- Routing rule matches and triggers a processing function
- Function calls deployed LLM for classification, summarization, or generation
- Result published back to event bus; downstream consumers receive it

**Custom Model Serving Endpoint**
- Upload model weights to MinIO
- Register and deploy via CF-AIRuntime
- Endpoint auto-registered in APISIX with API key auth and rate limiting
- Consumers call their own model at their own domain

**Training and Fine-Tuning Workflow** (Phase 2)
- Training data in MinIO
- Airflow DAG orchestrates preprocessing, training job submission, evaluation, artifact export
- Training job runs on GPU node as a Kubernetes Job (PyTorchJob via Kubeflow Training Operator in Phase 2)
- Fine-tuned weights written back to MinIO
- New deployment registered via CF-AIRuntime

**Multi-Step AI Agent**
- NATS event triggers agent execution
- Agent calls inference endpoint for LLM reasoning
- Agent writes intermediate state to PostgreSQL
- Agent stores artifacts in MinIO
- Agent reads secrets from CF-SecretsConfig
- Execution trace emitted to OpenSearch AI trace index via OTel
- Final result published as event; downstream systems react

All of these patterns use only platform-native primitives. No external AI infrastructure is required.

### What Is Platform-Native vs Consumer Responsibility

| Concern | Platform-native | Consumer responsibility |
|---|---|---|
| Inference serving infrastructure | CF-AIRuntime + KServe + vLLM/Ollama | Which model to deploy; what prompts to send |
| Vector search | pgvector on every PostgreSQL | Schema design; index tuning; query patterns |
| Model artifact storage | MinIO with naming conventions | Which models to store; dataset management |
| AI workflow events | NATS JetStream + CF-EventRouter | Workflow logic; business rules |
| AI workload identity | CF-IAM service account types | Policy design; permission grants |
| AI secrets management | CF-SecretsConfig with AI secret types | Which credentials to store |
| Inference telemetry | CF-Observability AI traces and usage | What to log from within agent code |
| Agent/workflow framework choice | Not prescribed; SDK reference only | LangGraph, CrewAI, custom — consumer decides |
| Prompt engineering | Not a platform concern | Consumer responsibility |
| Model selection | Consumer chooses models to deploy | Platform serves whatever is deployed |
| Experiment tracking, MLflow | Not in platform scope | Consumer deploys as standard workload if needed |

---

## 12. Self-Hosted vs Managed Offering Model

### Self-Hosted Model

The self-hosted deployment model is the primary v1 target. An operator deploys CloudForge onto a Kubernetes cluster they manage. CloudForge provides a Helm chart, a bootstrap CLI (`cf-install`), and a platform operator.

**Minimum viable self-hosted configuration (v1):**
- **Without GPU:** 3 nodes (8 vCPU, 32 GB RAM, 500 GB NVMe each). Ollama provides AI inference on CPU. Suitable for development, low-traffic production, and consumers whose AI workloads are not latency-critical.
- **With GPU:** 3–5 standard nodes + 1 GPU node (NVIDIA T4 minimum; A100/H100 for high-throughput inference). vLLM provides production-grade GPU inference. GPU node is optional — the platform operates without it.

The GPU node is optional infrastructure, not a platform requirement. `cf-install preflight` detects GPU nodes and configures the appropriate AI runtime (vLLM vs Ollama) automatically.

### Managed Offering Model

The managed offering is a future commercial model where CloudForge operates the platform for customers. The architectural requirements that differ from self-hosted:

1. **Billing and metering:** Usage data (compute time, storage bytes, API calls, token consumption for AI inference) must be aggregated and attributed to tenants. CF-ResourceController is designed with billing hooks for Phase 2 activation.
2. **GPU fleet management:** The managed offering must manage a GPU node pool for AI inference, including GPU utilization optimization, multi-tenant GPU sharing (via MIG partitioning in Phase 2), and GPU cost attribution per tenant.
3. **Stronger tenant isolation:** Dedicated node pools for premium tiers; AI inference isolation via MIG partitioning prevents GPU-level noisy-neighbor effects.
4. **Multi-cluster architecture:** A global control plane managing multiple Kubernetes clusters per region, with tenant routing.

**One codebase for both models.** Self-hosted and managed offerings run identical platform code. The difference is configuration and operational procedures.

---

## 13. Phased Implementation Roadmap

### Phase 1: MVP Platform — AI-Capable from Day One (Months 1–6)

**Objective:** A working, installable CloudForge platform that provides the core service set to a self-hosted SME customer — including AI serving infrastructure — with functional IAM, observability, and end-to-end AI workload capability.

AI serving is not deferred to a later phase. When Phase 1 ships, a consumer should be able to deploy a model and call it via the platform's API, store and retrieve model artifacts, and use pgvector for embedding workloads.

**Services included:**

*Core platform:*
- Identity: Keycloak + OPA + CF-IAM (identity-based policies, API keys for inference)
- Secrets: OpenBao + CF-SecretsConfig (including AI credential types)
- Tenancy: CF-ResourceController (including AI quota types)
- API Gateway: APISIX + CF-GatewayControl (including `ai-proxy` route type and streaming support)
- Storage: MinIO + Storage API (model artifact bucket conventions)
- Database: CloudNativePG + pgvector by default + CF-DBController (PostgreSQL only in Phase 1)
- Eventing: NATS JetStream + CF-EventRouter (basic routing rules; AI workflow event type patterns documented)
- Functions: Knative Serving + CF-FunctionTrigger (NATS trigger + cron)

*AI infrastructure (deployed alongside compute, not after it):*
- KServe + vLLM (GPU nodes, if present) + Ollama (CPU fallback)
- CF-AIRuntime: model registry, deployment management, OpenAI-compatible inference proxy, usage metering
- pgvector pre-installed on all CloudNativePG clusters
- AI-specific telemetry in CF-Observability: vLLM metrics, token usage API, AI inference traces in OpenSearch

*Observability:*
- OTel Collector + Prometheus + Grafana (with AI serving dashboards)
- OpenSearch + CF-Observability (with AI usage and traces API)

*Operations:*
- Helm chart with `dev`, `small`, `production` profiles (GPU optional in all)
- `cf-install` with GPU node detection and runtime selection

**Major engineering work:**
- CF-ResourceController with AI quota types
- CF-IAM with API key model and AI workload identity types
- CF-EventRouter routing rules engine
- CF-FunctionTrigger NATS-to-Knative bridge
- CF-AIRuntime model registry, KServe management, inference proxy
- CF-GatewayControl with `ai-proxy` route type and streaming passthrough
- CF-DBController with pgvector default
- CF-Observability with AI usage aggregation
- End-to-end test covering AI inference scenario

**Biggest risks:**
- CF-IAM policy model underestimated in complexity
- Streaming proxy in CF-AIRuntime requires careful implementation (no buffering of LLM responses)
- NATS JetStream multi-tenant account provisioning model must be validated before full implementation (spike)
- GPU hardware availability for self-hosted consumers is variable; Ollama CPU fallback must be robust

**Intentionally deferred:**
- ScyllaDB in CF-DBController (PostgreSQL + pgvector covers Phase 1 needs)
- MySQL support
- DLQ with retry policy in CF-EventRouter
- Apache Airflow data pipeline service
- GPU MIG partitioning for multi-tenant inference isolation
- Training job submission API
- Consumer AI reference SDK (documents the patterns; SDK implementation is Phase 2)
- Managed offering billing infrastructure

---

### Phase 2: Multi-Tenancy Hardening and Advanced AI Capabilities (Months 7–12)

**Objective:** Harden tenant isolation, complete IAM feature set, activate billing infrastructure, and add the advanced AI capabilities that require stable platform foundations.

**Services added/enhanced:**

*Platform hardening:*
- IAM: resource-based policies, permission boundaries, cross-project role assumption
- Database: ScyllaDB + CF-DBController NoSQL support; MySQL support; ScyllaDB CDC → NATS bridge (DynamoDB Streams equivalent)
- Eventing: DLQ with configurable retry policy and backoff
- Data pipeline: Apache Airflow adapter (CF-DataPipeline) for consumer ETL and training data workflows
- Billing hooks: token usage, compute time, and storage GB metering in CF-ResourceController

*Advanced AI:*
- Training job submission API in CF-AIRuntime: `POST /ai/v1/{tenant}/{project}/training-jobs` — submit Kubernetes Job with GPU node affinity, MinIO input/output, resource limits
- Model fine-tuning API: higher-level LoRA/QLoRA fine-tuning API with preset configurations; consumer specifies base model, training data path, output path, epochs
- Consumer AI Agent SDK: Go library (`pkg/agent/`) providing typed wrappers for NATS event triggers, inference client, secret access, MinIO artifact I/O, and OTel trace emission
- Python integration guide: generated Python client from CloudForge OpenAPI specs for use in LangGraph, CrewAI, and other Python AI frameworks
- Reference architectures: RAG pipeline, event-triggered inference pipeline, fine-tuning workflow

**Biggest risks:**
- ScyllaDB CDC → NATS bridge reliability under high write throughput
- Airflow multi-tenancy is operationally complex; DAG namespace isolation requires significant adapter work
- GPU MIG partitioning requires specific hardware (A100/H100); may not be available in all test environments

---

### Phase 3: Managed Offering and Enterprise Features (Months 13–24)

**Objective:** Commercial managed offering, multi-cluster architecture, GPU MIG partitioning for enterprise-grade multi-tenant AI isolation.

**Services added/enhanced:**
- Multi-cluster architecture: global control plane, cluster assignment, cross-cluster tenant routing
- GPU MIG partitioning: NVIDIA A100/H100 MIG slicing for hard GPU isolation between tenants
- Batch inference jobs: offline inference over MinIO datasets as a managed API
- OpenSearch as tenant-facing managed search service (in addition to internal observability use)
- Managed offering: commercial packaging, SLA infrastructure, billing production system, NOC tooling
- Platform marketplace: plugin model for third-party adapters and service integrations

---

## 14. Risks, Tradeoffs, and Open Questions

### Risk 1: CF-IAM Complexity

CF-IAM is the most critical and most complex service. The policy model, Keycloak integration, OPA evaluation pipeline, and API key lifecycle all interact. Underestimating implementation effort creates cascading delays. The spike on OPA evaluation performance (validating p99 < 5ms at 100 concurrent authorization requests) is mandatory before implementation begins.

### Risk 2: CF-AIRuntime Streaming Proxy Correctness

The inference proxy in CF-AIRuntime must correctly handle chunked-transfer encoding and Server-Sent Events from vLLM and Ollama without buffering responses. Buffering breaks the streaming experience — the entire response would arrive at once rather than token by token. The implementation must use Go's `http.Flusher` interface correctly and must be validated end-to-end (from vLLM through CF-AIRuntime proxy to the test client) before the Phase 1 MVP is declared complete.

### Risk 3: GPU Availability in Self-Hosted Deployments

Many SME self-hosted clusters will not have GPU nodes. The Ollama CPU fallback is essential. If Ollama in CPU mode is not robust as a substitute — if it is too slow for typical consumer workloads, or if there are API incompatibilities between the Ollama and vLLM response surfaces — the AI serving capability will be effectively unavailable to most Phase 1 users. This must be validated via a spike before Phase 6 implementation.

### Risk 4: NATS JetStream Account Provisioning at Scale

The NATS accounts model provides strong multi-tenancy, but dynamic account provisioning (creating a new account without restarting the NATS cluster) depends on the NATS operator's CRD support. At large tenant counts in the managed offering, per-account state memory pressure becomes a concern. This must be validated via spike before Phase 5 implementation and re-evaluated before the managed offering reaches significant scale.

### Risk 5: Knative Cold Start for AI-Calling Functions

Functions that call CF-AIRuntime on startup (e.g., to warmup an inference connection) may experience compounded cold start latency: Knative cold start + model serving pod cold start. For latency-sensitive AI workloads, minimum replicas should be set to 1 on both the function and the `InferenceService`. Documentation must be explicit about this.

### Risk 6: pgvector Performance Ceiling

pgvector is sufficient for SME-scale AI workloads. However, consumers building very large-scale embedding stores (tens of millions of vectors at high dimensions with millisecond query requirements) may eventually find pgvector insufficient. The platform must be clear about this ceiling and document the migration path to a dedicated vector store. This is not a v1 risk — it becomes relevant when a consumer's embedding store grows past approximately 10 million vectors.

### Tradeoff: ScyllaDB Alternator vs MongoDB

ScyllaDB Alternator is the recommended DynamoDB-compatible backend. For consumers building document-centric applications with complex aggregation pipelines, MongoDB may be a better fit. MongoDB (via Percona Operator) should be offered as an alternative CF-DBController engine type in Phase 2, covering the document workload gap.

### Tradeoff: No Dedicated Vector Database Service

The decision to use pgvector as the vector search layer (rather than building a separate Qdrant or Weaviate managed service) is a deliberate simplification. It is the right call for SME use cases. If a consumer outgrows pgvector, they can deploy a dedicated vector store as a standard Kubernetes workload. The platform does not need to manage it as a first-class service at this stage.

### Open Question: Keycloak Realm Model at Managed Offering Scale

For v1, one Keycloak realm per tenant is recommended for simplicity and isolation. At managed offering scale (potentially thousands of tenants), per-realm management becomes operationally expensive. Keycloak's Organizations feature (introduced in Keycloak 24+) provides multi-tenancy within a shared realm. Migration to the Organizations model should be planned for Phase 2. This migration requires careful identity data migration and must be planned before the managed offering reaches significant tenant count.

### Open Question: Training Job Infrastructure

The training job submission API (Phase 2) requires a decision on job runtime: a plain Kubernetes `Job` (sufficient for fine-tuning) or a distributed training operator (Kubeflow Training Operator, or Ray for distributed workloads). For LoRA/QLoRA fine-tuning on a single GPU node, a plain Kubernetes Job is adequate. For multi-GPU distributed training, an operator is required. The scope of Phase 2 training job support should be scoped to single-GPU fine-tuning to contain complexity, with multi-GPU distributed training deferred to Phase 3.

---

## 15. Final Recommendation

### Recommended v1 Stack

| Platform Capability | Recommended Component | Custom Work Required |
|---|---|---|
| Messaging / Eventing | NATS JetStream | Thin wrapper |
| EventBridge semantics | CF-EventRouter | Custom — must build |
| Functions | Knative Serving | Thin wrapper |
| Function triggers | CF-FunctionTrigger | Custom — must build |
| AI serving (GPU) | KServe + vLLM | OSS integration |
| AI serving (CPU/dev) | KServe + Ollama | OSS integration |
| AI runtime management | CF-AIRuntime | Custom — must build |
| Object Storage | MinIO | Thin wrapper |
| Vector Search | pgvector on CloudNativePG | OSS integration (default-on) |
| NoSQL / Key-Value | ScyllaDB Alternator | Thin wrapper |
| Relational Database | CloudNativePG (PostgreSQL) | Medium adapter |
| DB Proxy | PgBouncer (platform-managed) | Embedded in DB controller |
| API Gateway | Apache APISIX | Medium adapter |
| Load Balancing / Ingress | Cilium + Contour | OSS integration |
| IAM | Keycloak + OPA | Substantial custom layer |
| Secrets | OpenBao | Medium adapter |
| Config Store | OpenBao KV | Shared with secrets |
| Observability / ESS | OpenSearch + OTel + Prometheus + Grafana | Medium adapter |

### Services That Are Thin Wrappers

MinIO (Storage API), NATS JetStream (Messaging API), and ScyllaDB Alternator (NoSQL API) require primarily provisioning and IAM integration work. The underlying API surfaces are close to the intended CloudForge experience.

### Services That Require Substantial Custom Logic

**CF-IAM** and **CF-EventRouter** are the two platform components with the highest engineering risk and the highest platform value. CF-IAM is the identity and authorization backbone for every service, including AI endpoints. CF-EventRouter is the routing engine that connects events to workloads — including AI workflow automation. Both must be built first, staffed most heavily, and tested most rigorously.

**CF-AIRuntime** is new in this version. It is not as structurally foundational as CF-IAM, but it is on the MVP critical path and has its own technical risk: the streaming proxy, the KServe integration, and the model download job management each require careful implementation. It should be staffed and started in the same phase as CF-FunctionTrigger.

### Where AWS-like Behavior Is Worth Approximating

The EventBridge + Lambda trigger model — "define a rule, attach a function" — is the most valuable pattern to approximate closely. The OpenAI-compatible API surface for inference endpoints is the most valuable AI-specific interface to provide — it means consumer code works against any deployed model without SDK changes.

### Where Exact Imitation Adds Unnecessary Complexity

AWS's multi-account model and Organization structure is powerful but adds significant operational overhead. CloudForge's Tenant/Project model is simpler and more appropriate for SMEs. AWS SDK and CLI compatibility is not worth engineering for the platform surface — a clean CloudForge-native CLI and API is more maintainable and provides a better long-term developer experience.

For AI specifically: AWS Bedrock's model catalog approach (selecting managed models from a menu) is not applicable here. CloudForge provides the serving infrastructure; consumers bring their own models. The platform does not curate or host a model catalog.

### The Most Important Design Decision in This Document

AI infrastructure is part of the platform's compute, storage, database, and observability layers — deployed in the same phases, governed by the same IAM model, observable through the same telemetry pipeline. It is not a late-phase feature, not a separate product, and not a special case in any part of the architecture.

When a consumer asks "can I build an AI application on this platform?", the answer should be yes from day one, using the same tools they already use for the rest of their application. The storage is already there. The database has vector search already enabled. The eventing is already there. The secrets are already there. The observability is already there. The inference runtime is already deployed and accessible via API key. The only thing the consumer needs to do is bring a model and write their application logic.

---

*End of Document*

**Revision history:**  
v0.1 — Initial draft, April 2026  
v1.0 — AI infrastructure reframed as cross-cutting platform capability throughout all layers; pgvector added to database layer as default capability; CF-AIRuntime introduced as a required platform adapter alongside CF-FunctionTrigger; GPU scheduling added to compute/container orchestration section; AI serving placed in Phase 1 MVP alongside compute layer; AI observability integrated into CF-Observability from v1; "Agent Framework Strategy" section replaced with "AI Infrastructure Layer" documenting consumer patterns; Keycloak API key model added for inference endpoint access; no-GPU Ollama fallback path documented; AI quota types added to CF-ResourceController; AI credential types added to CF-SecretsConfig; streaming proxy requirement added to CF-GatewayControl

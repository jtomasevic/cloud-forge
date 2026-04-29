# CloudForge: Introducing CF-VPC
## Tenant Isolation, Onboarding, Provisioning, and Access — v1.0

**Status:** Proposal for Architecture Review  
**Date:** April 2026  
**Document:** `docs/3-Introduce-CF-VPC.md`  
**Supersedes:** `docs/1-cloud-forge-architecture-proposal.v0.1.md` (additive upgrade, not replacement)  
**Audience:** Engineering Leadership, Platform Architects, Senior Engineers  

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Problem Statement](#2-problem-statement)
3. [Why Per-Customer Isolation Matters](#3-why-per-customer-isolation-matters)
4. [Proposed Tenant Isolation Architecture](#4-proposed-tenant-isolation-architecture)
5. [Control Plane Architecture](#5-control-plane-architecture)
6. [Account, Credential, and Profile Storage](#6-account-credential-and-profile-storage)
7. [Provisioning Model](#7-provisioning-model)
8. [Gateway and Service Exposure Model](#8-gateway-and-service-exposure-model)
9. [Console and CLI Access Model](#9-console-and-cli-access-model)
10. [Control Plane Router, HA, and Load Balancing](#10-control-plane-router-ha-and-load-balancing)
11. [Recommended Tenant Isolation Spike](#11-recommended-tenant-isolation-spike)
12. [Improvement Path for Current CF](#12-improvement-path-for-current-cf)
13. [Technical Tasks and Spikes](#13-technical-tasks-and-spikes)
14. [Final Recommendation](#14-final-recommendation)

---

## 1. Executive Summary

The current CloudForge architecture is well-designed for its compute, storage, eventing, and AI layers. However, it contains a structural gap that will become a liability as the platform matures: **there is no first-class per-customer network isolation model**. Kubernetes namespaces with Cilium network policies are not a VPC. They are a partial mitigation — correct network policies can be inadvertently weakened, lateral movement within the same cluster remains possible, and a misconfiguration in a Cilium policy does not have the same consequence as a routing table misconfiguration in a VPC. The isolation model is policy-enforced, not topology-enforced.

This proposal addresses that gap directly. It introduces:

1. **A per-tenant virtual cluster model** (virtual clusters via vCluster) giving each customer a topologically isolated Kubernetes network boundary — not just policy isolation.
2. **A CF-Provisioner control plane service** responsible for onboarding new customers, creating tenant environments, and orchestrating service provisioning inside those environments.
3. **A ScyllaDB-backed account and profile store** for customer credentials, provisioned service inventory, and service profiles — replacing the "MinIO DB" placeholder from earlier discussions.
4. **A typed provisioning contract model** where each service type (NATS, MinIO, PostgreSQL, etc.) exposes a versioned provisioning schema that validates, executes, and tracks resource lifecycle inside the tenant environment.
5. **A layered gateway model** with private-only (intra-tenant) and public exposure modes per service instance.
6. **A CF-Router control plane component** that resolves tenant identity from tokens and API keys, routes API requests to the correct tenant environment, and enforces isolation at the routing layer.
7. **A hardened access model** covering UI console (Keycloak JWT + session) and CLI (API keys backed by CF-IAM).

This proposal is written as an additive upgrade. It does not discard the current architecture — it fills the gaps the current architecture leaves open. The sections that follow explain each area concretely, with technology recommendations, implementation tradeoffs, and a clear path from what exists today.

---

## 2. Problem Statement

The current CF architecture addresses service integration well. It does not yet address the foundational structural question: **what does it mean for two customers to share the same CloudForge platform without being able to affect each other?**

The current proposal uses Kubernetes namespaces as the isolation boundary and Cilium network policies as the enforcement mechanism. This is a reasonable starting point but is not sufficient for a production multi-tenant platform for the following reasons:

**Problem 1: Namespace isolation is policy-dependent, not topology-dependent.**  
A network policy misconfiguration, a missing label selector, or an overly permissive rule in a Cilium policy can silently allow cross-tenant traffic. There is no structural routing barrier between namespaces in a single Kubernetes cluster; the barrier exists only because a policy says so.

**Problem 2: Onboarding is not modeled as infrastructure provisioning.**  
The current proposal creates a Keycloak realm, sets up NATS accounts, and provisions namespaces. That is account creation, not environment creation. A production multi-tenant platform must treat onboarding as: create the network, create the environment, configure the gateway, assign the address space, set the quota, and then create the account. The order matters.

**Problem 3: There is no catalog of what a customer has provisioned.**  
The current architecture tracks resources inside the control plane's PostgreSQL database, but there is no explicit model for "what services does customer X have running, in what state, with what parameters?" The provisioning lifecycle — request, validate, execute, confirm, track, modify, tear down — is not modeled as a first-class platform concept.

**Problem 4: Credentials and account profiles are not cleanly separated.**  
The current design does not specify where customer usernames, password hashes, and service profiles are stored. Using OpenBao for this is wrong (it is a secrets store, not a user database). Using PostgreSQL in the control plane is possible but mixes operational data with identity data. This needs its own data model.

**Problem 5: Public vs private service exposure is not modeled.**  
Some customer services should be accessible only from within the customer's own environment (a private NATS cluster). Others need to be accessible from the public internet (a development endpoint) or from the customer's own external systems. There is no gateway model in the current architecture that controls this boundary per-service.

**Problem 6: The control plane has no explicit router component.**  
When a request arrives at the platform API, there is no defined mechanism for "which tenant does this request belong to, and where does it go?" The current architecture assumes APISIX routes to the correct service, but APISIX is a service gateway, not a tenant-aware request router. Tenant context resolution, routing decisions, and request dispatch to the correct tenant environment require a dedicated component.

---

## 3. Why Per-Customer Isolation Matters

### The hard requirement, stated plainly

Customer A and customer B must not share the same service network. Full stop.

This is not a security nicety. It is a structural requirement for operating a multi-tenant platform at any serious level:

- **Data residency and compliance.** A customer whose data must not leave their logical environment cannot share a network with another customer, even under a correctly-configured network policy. The requirement is structural, not policy-based.
- **Blast radius containment.** A misconfigured service in customer A's environment must not be able to discover, probe, or reach customer B's services — not even by accident.
- **Trust model clarity.** Customers need to be able to understand their isolation guarantee in plain terms: "your services run in their own network; no other customer can reach them." That statement is true when the isolation is topological. It is a conditional statement ("unless a policy is misconfigured") when the isolation is policy-only.
- **Auditability.** "No cross-tenant traffic occurred" is verifiable when the traffic path is topologically impossible. It requires policy audit and log correlation when isolation is policy-based.
- **Commercial viability.** Enterprise customers and any customer in a regulated industry will ask "how are we isolated from other tenants?" The answer "network policies prevent access" is not adequate. The answer "you have your own virtual private network" is.

### What "isolated tenant network" means in this proposal

Each customer gets a **virtual cluster** — a dedicated Kubernetes API server instance, running inside the host cluster, with its own isolated pod network CIDR, its own service network, its own DNS namespace, and its own set of provisioned service instances. Services provisioned for customer A are pods running inside customer A's virtual cluster. Services provisioned for customer B run inside customer B's virtual cluster.

The host cluster's Cilium network policies provide an additional hard boundary at the infrastructure level: cross-virtual-cluster traffic is default-denied, and the control plane's communication channels to each virtual cluster are the only permitted cross-boundary flows.

This model is analogous to AWS VPCs. The VPC is the topological boundary. Network policies (security groups) provide fine-grained control within it. You cannot accidentally route traffic from VPC A to VPC B unless an explicit peering or transit gateway is configured.

---

## 4. Proposed Tenant Isolation Architecture

### 4.1 The Isolation Model

CloudForge uses a **two-layer isolation architecture**:

**Layer 1: Physical boundary — the platform network**  
The CloudForge control plane, shared infrastructure (Keycloak, OpenBao, Prometheus, OpenSearch, the platform PostgreSQL), and the CF-Provisioner run in the platform network. This is a dedicated Kubernetes namespace set (`cf-system`, `cf-control-plane`, `cf-observability`, `cf-security`) with Cilium policies that allow inbound only from the platform API gateway and outbound only to the Kubernetes API server and to tenant virtual cluster API servers.

**Layer 2: Logical boundary — virtual clusters per tenant**  
Each provisioned tenant receives a **vCluster** — a lightweight Kubernetes API server (k3s-based) running as a StatefulSet inside a host cluster namespace dedicated to that tenant. The vCluster has:

- Its own pod CIDR (e.g., `10.100.{tenant-index}.0/24`)
- Its own service CIDR
- Its own DNS namespace (`{tenant-id}.cluster.local`)
- Its own RBAC, network policies, and resource quotas
- Its own ingress configuration

Services provisioned for the tenant (NATS JetStream, MinIO, PostgreSQL, Knative, etc.) are deployed as workloads inside the tenant's vCluster. From the tenant's perspective, they have a Kubernetes cluster. From the platform's perspective, they have a managed namespace set with an isolated API server.

```
┌─────────────────────────────────────────────────────────────────────┐
│                      HOST KUBERNETES CLUSTER                        │
│                                                                     │
│  ┌───────────────────────────────┐                                  │
│  │    PLATFORM NETWORK            │                                  │
│  │   (cf-system, cf-control-plane)│                                  │
│  │                               │                                  │
│  │  CF-Provisioner   CF-Router   │                                  │
│  │  CF-IAM           CF-Accounts │                                  │
│  │  Keycloak         OpenBao     │                                  │
│  │  PostgreSQL       ScyllaDB    │                                  │
│  └───────────┬───────────────────┘                                  │
│              │  Provisioner API (gRPC, mTLS)                        │
│              │                                                      │
│  ┌───────────▼───────────┐  ┌──────────────────────────┐           │
│  │  TENANT A NAMESPACE   │  │   TENANT B NAMESPACE     │           │
│  │                       │  │                          │           │
│  │  vCluster-A           │  │  vCluster-B              │           │
│  │  ┌─────────────────┐  │  │  ┌───────────────────┐  │           │
│  │  │  NATS-A         │  │  │  │  NATS-B           │  │           │
│  │  │  MinIO-A        │  │  │  │  MinIO-B          │  │           │
│  │  │  PostgreSQL-A   │  │  │  │  PostgreSQL-B     │  │           │
│  │  │  Functions-A    │  │  │  │  Functions-B      │  │           │
│  │  └─────────────────┘  │  │  └───────────────────┘  │           │
│  │  Pod CIDR: 10.100.1.0 │  │  Pod CIDR: 10.100.2.0   │           │
│  └───────────────────────┘  └──────────────────────────┘           │
│       ↑ Default-deny              ↑ Default-deny                    │
│       No route between tenant A and tenant B networks               │
└─────────────────────────────────────────────────────────────────────┘
```

### 4.2 Is Kubernetes Namespace Isolation Sufficient?

**No. Namespace isolation alone is not sufficient for a production multi-tenant platform.**

Kubernetes namespaces are a scope boundary, not a network boundary. All pods in all namespaces in the same cluster share the same underlying network fabric. The only thing preventing cross-namespace communication is a network policy that explicitly denies it. That policy can be:

- Accidentally absent on a new resource
- Incorrectly labeled, silently excluding pods that should be covered
- Not applied at all if the policy controller restarts or has a bug
- Bypassed by a privileged workload or a misconfigured service account

Cilium's eBPF enforcement makes this more reliable than kube-proxy-based network policies, but the fundamental problem remains: the isolation guarantee is "we configured a policy and it should be working" rather than "the traffic path does not exist."

**What vCluster adds that namespace isolation does not:**

| Guarantee | Namespace + NetworkPolicy | vCluster |
|---|---|---|
| Separate pod network CIDR | No (shared) | Yes |
| Separate service CIDR | No (shared) | Yes |
| Separate DNS namespace | No | Yes |
| Separate Kubernetes RBAC | Partial (namespace-scoped roles) | Full (dedicated API server) |
| Separate resource quotas | Yes | Yes |
| Separate CRDs | No (cluster-scoped) | Yes |
| Blast radius containment | Policy-dependent | Topological |
| Customer-explainable isolation | "We have policies" | "You have your own network" |

### 4.3 Technology Recommendation: vCluster

**Recommended technology: vCluster (Loft Labs, open source under Apache 2.0)**

vCluster creates a fully functional Kubernetes API server inside a standard Kubernetes namespace. The virtual cluster's workloads run as pods in the host cluster but are only visible and addressable within the virtual cluster's network boundary.

Why vCluster over alternatives:

- **vs. dedicated physical clusters:** vCluster is 10–20× cheaper at small tenant counts. A dedicated cluster per tenant requires dedicated nodes, load balancers, and operational overhead that is unacceptable at SME scale. vCluster is the right model until a customer is large enough to justify dedicated infrastructure.
- **vs. namespace-only isolation:** vCluster provides network topology isolation, not just policy isolation. It also gives the tenant their own Kubernetes API server, which allows platform-standard CRDs, RBAC, and resource models to be scoped per tenant without cluster-scoped resource conflicts.
- **vs. Kata Containers / microVMs:** Kata provides hardware-level container isolation but does not solve the network topology problem. It is an additional hardening layer, not a replacement for network isolation.

**vCluster operational model in CF:**

1. CF-Provisioner calls the vCluster Helm chart / CLI to create a new virtual cluster for a new tenant
2. The virtual cluster API server runs as a 1–3 replica StatefulSet in a host namespace named `tenant-{tenant-id}`
3. CF-Provisioner receives a kubeconfig for the virtual cluster and stores it (encrypted) in OpenBao
4. All subsequent provisioning for that tenant is performed by applying Kubernetes manifests to the tenant's virtual cluster API server via that kubeconfig
5. Cilium network policies on the host cluster enforce that the tenant namespace has no direct connectivity to other tenant namespaces or to the platform network (except through the provisioner communication channel)

### 4.4 Traffic Separation Between Tenants

Traffic separation operates at three levels:

**Level 1: Host network policy (Cilium)**  
Default-deny egress from every tenant namespace to every other tenant namespace and to the platform network, enforced by Cilium eBPF rules at the kernel level. Explicit allow rules permit only:
- The CF-Provisioner to connect to the tenant's vCluster API server (gRPC/mTLS on port 6443)
- The tenant's ingress gateway to receive inbound traffic from the platform's external load balancer
- Metrics scraping from the CF-Observability collector

**Level 2: vCluster network isolation**  
Each vCluster has a non-overlapping pod CIDR assigned at creation time. The virtual cluster's control plane (CoreDNS, kube-proxy equivalent) resolves names only within the virtual cluster's namespace. A pod inside tenant A's vCluster cannot resolve `nats.tenant-b.svc.cluster.local` — the DNS entry does not exist in tenant A's DNS.

**Level 3: Application-level isolation**  
NATS accounts, MinIO bucket policies, and database credentials are provisioned per-tenant within their vCluster. Even if a hypothetical path existed between vClusters at the network level, the application credentials would prevent access.

### 4.5 Validated Spike Requirement

The isolation model described here must be validated through a focused spike before being adopted as the platform's hard isolation guarantee. The spike is described in detail in Section 11.

---

## 5. Control Plane Architecture

### 5.1 Overview

The CloudForge control plane gains a new first-class service in this proposal: **CF-Provisioner**, responsible for the full lifecycle of tenant environments and service instances. The existing CF-ResourceController is narrowed to quota enforcement and resource inventory. CF-Provisioner owns the execution side: creating environments, deploying services, managing state transitions.

```
┌────────────────────────────────────────────────────────────────────────┐
│                       PLATFORM NETWORK                                 │
│                                                                        │
│  External Traffic                                                      │
│       │                                                                │
│  ┌────▼───────────────────────────────────────┐                        │
│  │  CF-Router (platform API entry point)       │                        │
│  │  Resolves tenant identity, routes requests │                        │
│  └────┬─────────────┬──────────────┬───────────┘                       │
│       │             │              │                                   │
│  ┌────▼────┐  ┌─────▼─────┐  ┌────▼────────────────────┐              │
│  │ CF-IAM  │  │CF-Accounts│  │   CF-Provisioner        │              │
│  │ Keycloak│  │(ScyllaDB) │  │                         │              │
│  │ OPA     │  │           │  │  onboarding             │              │
│  └─────────┘  └───────────┘  │  vCluster lifecycle     │              │
│                              │  service provisioning   │              │
│  ┌───────────────────────┐   │  state machine          │              │
│  │  CF-ResourceController│   │  quota validation       │              │
│  │  (quotas, inventory)  │   └────────────┬────────────┘              │
│  └───────────────────────┘                │                           │
│                              ┌────────────▼────────────┐              │
│  ┌───────────────────────┐   │  vCluster API (mTLS)    │              │
│  │  OpenBao (secrets)    │   │  kubeconfig per tenant  │              │
│  │  Platform PostgreSQL  │   │  stored in OpenBao      │              │
│  └───────────────────────┘   └────────────┬────────────┘              │
└────────────────────────────────────────────│───────────────────────────┘
                                             │
        ┌────────────────────────────────────▼──────────────────────────┐
        │               TENANT VIRTUAL CLUSTERS                         │
        │                                                               │
        │  vCluster-A          vCluster-B          vCluster-C           │
        │  (isolated network)  (isolated network)  (isolated network)   │
        └───────────────────────────────────────────────────────────────┘
```

### 5.2 CF-Provisioner: Roles and Responsibilities

CF-Provisioner is the execution engine of the control plane. It is responsible for:

**Onboarding:**
- Receiving a new tenant registration request (from UI console or CLI via CF-Router)
- Validating the request (unique tenant ID, quota pre-check, compliance metadata)
- Creating the tenant record in CF-Accounts (ScyllaDB)
- Allocating a pod CIDR block for the tenant's vCluster
- Creating the vCluster instance via the vCluster operator/Helm
- Waiting for the vCluster to become healthy (Kubernetes API ready)
- Storing the vCluster kubeconfig in OpenBao under the tenant's path
- Initializing the tenant's vCluster with platform-standard baseline resources (default network policies, namespace structure, platform service accounts, observability agent)
- Creating the tenant's initial Keycloak realm / organization
- Generating and returning the tenant's initial admin credentials

**Service Provisioning:**
- Accepting provisioning requests for each service type (NATS, MinIO, PostgreSQL, Knative, ScyllaDB)
- Looking up the provisioning handler for the requested service type
- Calling CF-ResourceController to validate quota headroom
- Creating a `ProvisioningJob` record in CF-Accounts (with idempotency key)
- Executing the provisioning handler, which applies Kubernetes manifests to the tenant's vCluster
- Polling the provisioned resource for readiness (operator-specific health check)
- Writing provisioned credentials to the tenant's OpenBao path
- Updating the service profile record in CF-Accounts
- Emitting lifecycle events to the platform NATS (for observability and billing hooks)

**Deprovisioning:**
- Accepting deprovision requests for service instances or entire tenant environments
- Executing teardown in correct dependency order (remove services before removing vCluster)
- Archiving tenant data before deletion (configurable retention window)
- Cleaning up OpenBao paths, Keycloak realms, Cilium policies, and host namespace

### 5.3 Communication Between Control Plane and Tenant Environments

The control plane communicates with tenant virtual clusters exclusively via the **Kubernetes API server of the vCluster**, using a kubeconfig stored in OpenBao. There is no direct pod-to-pod connection between the platform network and any tenant environment.

This is important: the provisioner does not SSH into tenant nodes, does not have direct gRPC connections to tenant services, and does not use shared secrets. It speaks Kubernetes to the tenant's Kubernetes. This is the correct model because:

- It uses a well-defined, audited protocol (Kubernetes API)
- It does not require any open listening port on the tenant side
- Access is controlled by the vCluster's own RBAC (the provisioner has a platform service account with the minimum required permissions inside each vCluster)
- It is revocable — removing the kubeconfig from OpenBao and revoking the service account removes the provisioner's access entirely

### 5.4 Onboarding Flow End-to-End

```
User submits new account request (UI or CLI)
    │
    ▼
CF-Router resolves as an unauthenticated/pre-auth provisioning request
    │
    ▼
CF-Provisioner validates:
    - tenant ID uniqueness
    - contact information completeness
    - initial plan / quota selection
    │
    ▼
CF-Accounts: write tenant record (status: PROVISIONING)
    │
    ▼
CF-ResourceController: reserve initial quota block
    │
    ▼
vCluster operator: create new virtual cluster
  - assign pod CIDR: 10.100.{n}.0/24
  - assign service CIDR: 10.200.{n}.0/24
  - name: tenant-{tenant-id}
    │
    ▼
Wait for vCluster API server ready (health check loop, max 5 min)
    │
    ▼
Apply baseline manifests to vCluster:
  - default-deny NetworkPolicy
  - platform observability agent (DaemonSet)
  - platform metrics collector sidecar injector
  - CF platform service account for provisioner
    │
    ▼
OpenBao: write vCluster kubeconfig at cf/tenants/{tenant-id}/kubeconfig
    │
    ▼
Keycloak: create tenant realm / organization
  - admin user (temporary password)
  - client for CLI API key flow
  - client for UI console OIDC flow
    │
    ▼
CF-IAM: initialize tenant default policies
    │
    ▼
CF-Accounts: update tenant record (status: ACTIVE)
    │
    ▼
Email: send admin credentials and console URL to tenant admin
```

Total expected provisioning time: **2–4 minutes** for an empty vCluster. Service provisioning adds time per service type (NATS: ~30s, PostgreSQL: ~2 min with operator, MinIO: ~45s).

---

## 6. Account, Credential, and Profile Storage

### 6.1 What Needs to Be Stored

The control plane must maintain:

1. **Customer accounts:** tenant ID, organization name, contact info, plan/tier, status, timestamps
2. **User records:** username, email, password hash (bcrypt/Argon2id), tenant association, roles, MFA state
3. **Service profiles:** per-tenant list of provisioned service instances with parameters, status, version, endpoints
4. **Provisioning job log:** history of all provisioning and deprovisioning operations with state machine transitions, timestamps, and error records
5. **API keys:** key ID, hashed key value, tenant association, scopes, expiry, last-used timestamp

### 6.2 Should MinIO Be Used as a Database?

**No. MinIO is object storage. It is not appropriate as a database for account data.**

MinIO has no query model beyond prefix listing. It has no indexing. It has no transactions. It has no secondary indexes. Storing user accounts as JSON objects in MinIO and querying them by prefix is an object store being used as a key-value store, which is a misuse of the technology and will result in poor performance for any lookup beyond exact key retrieval.

MinIO is the correct store for:
- Model weights and AI artifacts
- Backup archives
- Document and media objects
- Batch job inputs/outputs
- OPA policy bundles

MinIO is the wrong store for:
- User account records
- Password hashes
- Service profiles with structured query requirements
- Provisioning state

### 6.3 Recommended Datastore: ScyllaDB (CQL native, not Alternator)

**Recommendation: ScyllaDB with native CQL for the CF-Accounts data store**

ScyllaDB is already in the CloudForge stack. For the account store, use ScyllaDB via native CQL (not Alternator/DynamoDB-compatible API) because:

- CQL provides secondary indexes, materialized views, and lightweight transactions (LWT) for conditional writes — all needed for idempotent provisioning
- ScyllaDB provides low-latency reads at high concurrency, appropriate for the routing hot path (every API request resolves tenant identity)
- A single ScyllaDB cluster can serve both the CF-Accounts data and the NATS-bridged ScyllaDB workloads for tenant services
- The platform already has a ScyllaDB operator and deployment model from Task 0.7

**Schema overview (CQL):**

```cql
-- Tenant accounts
CREATE TABLE cf.tenants (
    tenant_id     UUID PRIMARY KEY,
    slug          TEXT,
    display_name  TEXT,
    status        TEXT,     -- PROVISIONING | ACTIVE | SUSPENDED | DELETED
    plan_id       TEXT,
    pod_cidr      TEXT,
    svc_cidr      TEXT,
    created_at    TIMESTAMP,
    updated_at    TIMESTAMP
);
CREATE MATERIALIZED VIEW cf.tenants_by_slug AS
    SELECT * FROM cf.tenants WHERE slug IS NOT NULL AND tenant_id IS NOT NULL
    PRIMARY KEY (slug, tenant_id);

-- User records
CREATE TABLE cf.users (
    user_id       UUID,
    tenant_id     UUID,
    email         TEXT,
    password_hash TEXT,     -- Argon2id hash
    role          TEXT,
    mfa_enabled   BOOLEAN,
    status        TEXT,
    created_at    TIMESTAMP,
    PRIMARY KEY (tenant_id, user_id)
);
CREATE MATERIALIZED VIEW cf.users_by_email AS
    SELECT * FROM cf.users WHERE email IS NOT NULL
    AND tenant_id IS NOT NULL AND user_id IS NOT NULL
    PRIMARY KEY (email, tenant_id, user_id);

-- Provisioned service instances
CREATE TABLE cf.service_instances (
    tenant_id     UUID,
    instance_id   UUID,
    service_type  TEXT,    -- NATS | MINIO | POSTGRESQL | KNATIVE | SCYLLADB
    display_name  TEXT,
    status        TEXT,    -- PENDING | PROVISIONING | READY | ERROR | DELETED
    version       TEXT,
    parameters    TEXT,    -- JSON blob: service-specific provisioning parameters
    endpoints     TEXT,    -- JSON blob: connection strings, internal/external URLs
    created_at    TIMESTAMP,
    updated_at    TIMESTAMP,
    PRIMARY KEY (tenant_id, instance_id)
);

-- Provisioning job log
CREATE TABLE cf.provisioning_jobs (
    job_id            UUID,
    tenant_id         UUID,
    idempotency_key   TEXT,
    operation         TEXT,   -- PROVISION | DEPROVISION | MODIFY
    service_type      TEXT,
    instance_id       UUID,
    status            TEXT,   -- QUEUED | RUNNING | SUCCEEDED | FAILED
    error_message     TEXT,
    started_at        TIMESTAMP,
    completed_at      TIMESTAMP,
    PRIMARY KEY (tenant_id, job_id)
) WITH CLUSTERING ORDER BY (job_id DESC);
```

**Password storage:** Argon2id with per-user salt. Never store plaintext or reversible passwords. Use `golang.org/x/crypto/argon2` with parameters: memory=64MB, iterations=3, parallelism=4.

**API keys:** Store only the BLAKE2b hash of the API key in `cf.api_keys`. The raw key is returned exactly once at creation and not stored anywhere in the platform. Rotation invalidates the old record and creates a new one.

### 6.4 Why Not PostgreSQL for the Account Store?

PostgreSQL (via CloudNativePG) is the right choice for structured relational data with complex joins. For the account store, the access patterns are primarily:

- Lookup by tenant slug (from JWT or API key, on every request)
- Lookup by email (login flow)
- List service instances by tenant (profile page, CLI)
- Insert with conditional uniqueness check (account creation)

These are low-latency, high-frequency point reads and simple list queries with no multi-table joins. ScyllaDB handles these patterns with lower latency and higher throughput than PostgreSQL. Additionally, ScyllaDB's active-active replication model provides better HA characteristics for this hot-path data than a PostgreSQL primary/replica setup.

PostgreSQL remains the right choice for the platform's operational data (CF-ResourceController inventory, audit log, complex reporting queries) — data that requires joins, aggregations, or complex transactions.

---

## 7. Provisioning Model

### 7.1 Provisioning as a First-Class Concept

Every service instance in CloudForge is created through a **provisioning contract** — a versioned, validated specification of what the consumer wants and what the platform will create. Provisioning is not a direct API call to a Kubernetes operator. It is a declarative request that the platform accepts, validates, queues, executes, and tracks through a state machine.

### 7.2 Provisioning Request Structure

Every provisioning request has the same outer envelope:

```json
{
  "idempotency_key": "cli-session-abc123-nats-1",
  "service_type": "NATS_JETSTREAM",
  "display_name": "my-event-bus",
  "parameters": { ... service-specific ... },
  "capabilities": { ... business-level intent ... }
}
```

The `parameters` block is service-specific and governed by a versioned JSON Schema registered per service type. The `capabilities` block expresses business intent that the platform uses to select defaults (e.g., `"high_throughput": true` translates to specific NATS cluster sizing).

**Example: NATS JetStream provisioning request**

```json
{
  "idempotency_key": "cli-20260428-001",
  "service_type": "NATS_JETSTREAM",
  "display_name": "order-events",
  "parameters": {
    "cluster_size": 3,
    "storage_type": "file",
    "storage_gb": 50,
    "max_payload_bytes": 1048576,
    "replicas": 3
  },
  "capabilities": {
    "at_least_once_delivery": true,
    "ordered_consumers": false,
    "event_sourcing": false,
    "max_retention_days": 30
  }
}
```

**Example: PostgreSQL provisioning request**

```json
{
  "idempotency_key": "ui-provision-pg-prod",
  "service_type": "POSTGRESQL",
  "display_name": "app-database-prod",
  "parameters": {
    "version": "16",
    "instance_class": "small",
    "storage_gb": 100,
    "high_availability": true,
    "pgvector_enabled": true
  },
  "capabilities": {
    "vector_search": true,
    "connection_pooling": true,
    "automated_backups": true,
    "backup_retention_days": 7
  }
}
```

### 7.3 Provisioning State Machine

```
REQUESTED
    │
    ▼ (validation passes, quota available)
QUEUED
    │
    ▼ (worker picks up job)
PROVISIONING
    │
    ├─▶ ERROR (validation failure, quota exceeded, operator error)
    │       │
    │       ▼
    │   FAILED (terminal, can be retried with new request + idempotency key)
    │
    ▼ (operator reports Ready)
CONFIGURING
    │   (writing credentials to OpenBao, updating CF-Accounts, registering endpoints)
    │
    ▼
READY (terminal success state)
    │
    ▼ (deprovision request)
DEPROVISIONING
    │
    ▼
DELETED (terminal)
```

State transitions are written to `cf.provisioning_jobs` in ScyllaDB with LWT (lightweight transactions) to prevent duplicate state transitions.

### 7.4 Provisioning Handlers

Each service type is implemented as a **provisioning handler** — a Go struct implementing the `ProvisioningHandler` interface:

```go
type ProvisioningHandler interface {
    // ServiceType returns the unique identifier for this handler.
    ServiceType() string

    // Validate checks whether the provisioning request is structurally
    // valid and quota-compatible before any infrastructure changes are made.
    Validate(ctx context.Context, req *ProvisioningRequest) error

    // Provision executes the provisioning steps against the tenant's vCluster.
    // It must be idempotent: calling it twice with the same request must not
    // create duplicate resources.
    Provision(ctx context.Context, tenantKubeconfig *rest.Config, req *ProvisioningRequest) (*ServiceInstance, error)

    // Health returns the current health/readiness status of a provisioned instance.
    Health(ctx context.Context, tenantKubeconfig *rest.Config, instance *ServiceInstance) (HealthStatus, error)

    // Deprovision removes the provisioned instance and all associated resources.
    // It must be safe to call on a partially-provisioned instance.
    Deprovision(ctx context.Context, tenantKubeconfig *rest.Config, instance *ServiceInstance) error
}
```

Handlers for v1: `NATSJetStreamHandler`, `MinIOHandler`, `PostgreSQLHandler`, `KnativeHandler`, `ScyllaDBHandler`.

Each handler applies Kubernetes manifests to the tenant's vCluster using the tenant's stored kubeconfig. The handlers do not know about each other; the provisioner orchestrates them and manages dependencies (e.g., MinIO must be ready before an AI runtime handler can provision the model bucket).

### 7.5 Tracking Provisioned Services Over Time

The `cf.service_instances` table in ScyllaDB is the authoritative inventory of what each tenant has. It stores:

- The service type and version that was provisioned
- The parameters used for provisioning (for audit and reprovisioning)
- The current status
- The connection endpoints (stored as JSON: internal endpoint within vCluster, external endpoint via tenant gateway)
- The last-modified timestamp

The UI console and CLI both query this table to display the tenant's service profile. Modifications to a running service (e.g., increasing PostgreSQL storage) create a new `provisioning_job` and update the `service_instances` record through the same state machine.

---

## 8. Gateway and Service Exposure Model

### 8.1 The Problem with a Single Platform Gateway

The current CF architecture uses APISIX as a single platform-wide API gateway. This is appropriate for the **CloudForge platform API** (the control plane API that tenants call to provision services, manage identity, etc.). It is not sufficient for **tenant service exposure** (the need for customer A's NATS cluster to be reachable by customer A's applications, but not by customer B's applications, and potentially from the internet for certain services).

These are two different routing problems and should not share the same gateway instance.

### 8.2 Two Gateway Tiers

**Tier 1: Platform API Gateway (existing)**  
APISIX serving the CloudForge control plane API. All tenants share this gateway for platform API calls (provisioning, IAM, observability queries, etc.). Requests here are always authenticated and routed by CF-Router to the appropriate control plane service. This gateway has no knowledge of individual service instances.

**Tier 2: Tenant Service Gateway (new)**  
Each tenant has a dedicated **Envoy/Contour ingress instance** deployed inside their vCluster. This tenant gateway is the entry point for all access to that tenant's provisioned services. It operates inside the tenant's network boundary. The platform's external load balancer routes traffic to the correct tenant gateway based on the tenant's domain / SNI header.

```
Internet / External Clients
          │
┌─────────▼─────────────────────────────────┐
│   Platform External Load Balancer          │
│   (Cilium L4LB or cloud LB)               │
└──────┬──────────────────┬─────────────────┘
       │                  │
       ▼                  ▼
  Platform API         Tenant Routes
  APISIX               (by SNI / domain)
  (control plane)          │
                    ┌──────┴──────────────────┐
                    │                         │
               ┌────▼──────┐         ┌────────▼─────┐
               │Tenant-A   │         │Tenant-B       │
               │Gateway    │         │Gateway        │
               │(Envoy in  │         │(Envoy in      │
               │vCluster-A)│         │vCluster-B)    │
               └────┬──────┘         └───────────────┘
                    │
          ┌─────────┴─────────────┐
          │                       │
     ┌────▼────┐           ┌──────▼──────┐
     │ NATS-A  │           │ MinIO-A     │
     │(private)│           │(public dev  │
     │         │           │ endpoint)   │
     └─────────┘           └─────────────┘
```

### 8.3 Private vs Public Service Exposure

Every provisioned service instance has an **exposure policy** set at provisioning time and modifiable afterward:

**Private (default):**  
The service is accessible only from within the tenant's vCluster. Its endpoint is a cluster-internal DNS name: `nats-order-events.tenant-a.svc.cluster.local`. External traffic cannot reach it. This is the correct default for production NATS clusters, internal databases, and any service that should not be internet-accessible.

**Tenant-internal public:**  
The service is accessible from the tenant's own external applications (e.g., a customer's on-premises application connecting to their NATS cluster). Traffic routes through the tenant gateway with mTLS, authenticated against the service's credentials. The endpoint is a subdomain of the tenant's assigned domain: `nats-order-events.{tenant-id}.cf-services.io`.

**Platform-public (dev/external):**  
The service is accessible from the public internet. Used for development endpoints, MinIO consoles, and any service the tenant explicitly wants to expose externally. The endpoint is publicly routable. Rate limiting and authentication are enforced by the tenant gateway. This mode is subject to quota enforcement and can be disabled for compliance tiers.

Exposure policy is stored in `cf.service_instances` as part of the `endpoints` JSON blob:

```json
{
  "internal": "nats-order-events.default.svc.cluster.local:4222",
  "tenant_private": "nats-order-events.acme-corp.cf-services.io:4222",
  "public": null,
  "exposure_mode": "TENANT_PRIVATE"
}
```

### 8.4 Tenant Gateway Lifecycle

The tenant gateway (Envoy/Contour instance) is provisioned automatically when the tenant's vCluster is created. It is part of the baseline manifests applied during onboarding (Section 5.4). CF-Provisioner manages the gateway's route configuration: when a service is provisioned with non-private exposure, CF-Provisioner creates the corresponding Contour `HTTPProxy` or `TCPProxy` object in the tenant's vCluster, and the tenant gateway starts serving the route.

The platform's external load balancer routes to the correct tenant gateway using SNI (for TLS) or subdomain prefix. cert-manager (deployed in each vCluster) manages TLS certificates for tenant service endpoints.

### 8.5 What Is Appropriate for v1 vs Later Hardening

**v1:**
- Private and tenant-private exposure modes, fully functional
- Public exposure mode with basic auth and rate limiting
- One tenant gateway per tenant (single instance, no HA)
- Automatic certificate management via cert-manager + Let's Encrypt

**v2 hardening:**
- Tenant gateway HA (2–3 Envoy replicas per tenant)
- Mutual TLS between client and tenant gateway
- WAF rules at the tenant gateway layer
- IP allowlisting for tenant-private exposure
- Dedicated IP addresses per tenant (not shared with other tenants at the LB level)

---

## 9. Console and CLI Access Model

### 9.1 UI Console

**Login model: Keycloak OIDC with tenant-scoped realms**

Each tenant has a Keycloak realm (or Keycloak Organization if using the new multi-tenant model). The UI console login page accepts an email address, looks up the tenant by email domain or by explicit tenant ID entry, and redirects to the correct Keycloak realm for authentication.

The console receives a short-lived **JWT access token** (15-minute TTL) and a **refresh token** (7-day TTL with sliding window). The access token contains:
- `sub`: user ID
- `cf_tenant_id`: tenant UUID
- `cf_roles`: array of roles in this tenant
- `iss`: Keycloak issuer URL for this tenant's realm

The console sends the JWT in the `Authorization: Bearer` header on all platform API calls. CF-Router validates the JWT (signature, expiry, issuer) and extracts `cf_tenant_id` to route the request to the correct tenant context.

**Where credentials are stored:**  
- Password hash: `cf.users` table in ScyllaDB (CF-Accounts service)
- Keycloak maintains its own shadow copy for SSO session management. The password set by the user during onboarding is hashed by the CF-Accounts service (Argon2id) before storage and also synced to Keycloak's credential store via the Keycloak Admin API.
- Recovery codes and MFA TOTP secrets: OpenBao, path `cf/tenants/{tenant-id}/users/{user-id}/mfa`

**Is auth central or tenant-scoped?**  
Auth is **tenant-scoped**. Each tenant authenticates against their own Keycloak realm. A user in tenant A's realm cannot log in to the console with tenant B's context — the JWT issuer in the token is realm-specific and CF-Router enforces issuer matching. There is a platform-level super-admin identity (for platform operators) that authenticates against the platform realm and is not a tenant user.

**Session model:**  
Stateless on the server side. The UI holds the JWT and refresh token. Logout invalidates the Keycloak session server-side, which prevents refresh. The JWT itself may still be valid for up to 15 minutes after logout — this is an acceptable tradeoff for v1. For v2, short-lived JWTs (5 minutes) with instant revocation via a Keycloak session check endpoint can reduce the window.

### 9.2 CLI Access

**Model: API keys (long-lived bearer tokens)**

For programmatic and CLI access, the correct model is **API keys** — not OIDC flows. A browser-based OAuth2 flow is impractical for automated pipelines, CI/CD, and command-line usage. API keys are the industry standard for this pattern (GitHub PATs, AWS access keys, Stripe API keys).

**How API keys work in CF:**

1. User generates an API key via the UI console or via `cf apikey create --name "ci-pipeline" --scope "provision,read"`. 
2. CF-IAM generates a random 32-byte key with prefix `cf_` for easy recognition (`cf_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`).
3. CF-IAM stores the BLAKE2b hash of the key in `cf.api_keys` (ScyllaDB) alongside the tenant ID, scopes, expiry, and created-by user. **The raw key is never stored.**
4. The raw key is returned to the caller exactly once. The user is responsible for storing it securely.
5. On each CLI request, the CLI sends `Authorization: Bearer cf_live_xxx...` 
6. CF-Router performs a fast BLAKE2b hash of the presented key and looks up the hash in `cf.api_keys` (a single ScyllaDB read with the hash as the lookup key, ~1ms).
7. If found and not expired, the tenant ID and scopes are extracted and the request proceeds.

**Rotation:**  
`cf apikey rotate --id <key-id>` creates a new key, returns it once, and marks the old key as `ROTATING` with a 24-hour grace period before it is disabled. This allows rotation without a deployment window.

**Scopes:**  
API keys are issued with explicit scopes: `provision:write`, `provision:read`, `iam:read`, `iam:write`, `secrets:read`. A CI/CD key that only needs to read service endpoints should not have `iam:write`. The scope is validated in CF-Router against the required scope for the route being accessed.

**How CLI identity maps to tenant/account:**  
The API key lookup returns the `tenant_id` and `user_id` that created the key. CF-Router uses `tenant_id` for all routing decisions. Audit logs record `user_id` for attribution. The key is indistinguishable from a JWT session in terms of what access it grants — it is simply a faster and simpler authentication mechanism for non-interactive clients.

---

## 10. Control Plane Router, HA, and Load Balancing

### 10.1 CF-Router: Purpose and Design

CF-Router is the single entry point for all API traffic directed at CloudForge. Its job is narrow but critical:

1. **Authenticate the request** (validate JWT or API key)
2. **Resolve tenant context** (extract `tenant_id` from the token/key)
3. **Route the request** to the correct control plane service
4. **Enforce request-level authorization** (does this tenant's token have the required scope for this route?)
5. **Pass the authenticated tenant context downstream** (inject `cf-tenant-id`, `cf-user-id`, `cf-scopes` headers to the backend service)

CF-Router does **not**:
- Make provisioning decisions
- Understand service-specific semantics
- Hold open connections to tenant vClusters
- Cache tenant state (it reads from CF-Accounts on every request for the routing lookup)

### 10.2 Request Routing Logic

```
Incoming request: POST /api/v1/provision/services
Authorization: Bearer cf_live_abc123

CF-Router:
  1. Strip the token from Authorization header
  2. Identify token type: prefix "cf_live_" → API key
  3. BLAKE2b hash the key
  4. Lookup hash in ScyllaDB cf.api_keys → {tenant_id, scopes, status}
  5. Check: status == ACTIVE, expiry not passed, scope includes "provision:write"
  6. Extract route: /api/v1/provision/* → CF-Provisioner service
  7. Forward request to CF-Provisioner with injected headers:
     X-CF-Tenant-ID: {tenant_id}
     X-CF-User-ID: {user_id}
     X-CF-Scopes: provision:write,provision:read

CF-Provisioner:
  - Trusts the injected X-CF-Tenant-ID header (never re-validates the original token)
  - Executes the request in the context of the resolved tenant
```

CF-Router's routing table maps URL path prefixes to backend services:

| Path prefix | Backend service |
|---|---|
| `/api/v1/provision/*` | CF-Provisioner |
| `/api/v1/iam/*` | CF-IAM |
| `/api/v1/secrets/*` | CF-SecretsConfig |
| `/api/v1/accounts/*` | CF-Accounts |
| `/api/v1/resources/*` | CF-ResourceController |
| `/api/v1/observe/*` | CF-Observability |
| `/api/v1/events/*` | CF-EventRouter |
| `/api/v1/functions/*` | CF-FunctionTrigger |
| `/api/v1/ai/*` | CF-AIRuntime |

### 10.3 Should CF-Router Be Stateless?

**Yes. CF-Router must be stateless.**

CF-Router holds no per-request state between calls. The only read it performs is the ScyllaDB API key lookup (or Keycloak JWT validation, which is purely cryptographic). This design means:

- Any CF-Router instance can handle any request
- Scaling is horizontal with no coordination required
- Failure of one router instance does not affect other instances or any in-flight requests
- Blue/green deployments are instant — old and new router versions can run simultaneously

The ScyllaDB lookup is the only stateful operation. ScyllaDB's consistent hashing means this read is always routed to the correct shard node and completes in ~1ms.

### 10.4 Replication and HA

**CF-Router:** Run 3+ replicas behind the platform's external load balancer (Cilium L4LB). No leader election — all replicas are active. HPA scales based on request rate. The load balancer health-checks each replica and removes unhealthy instances from rotation within 10 seconds.

**CF-Provisioner:** Run 2+ replicas. Provisioning jobs are written to ScyllaDB before execution begins. The provisioner uses distributed locking (ScyllaDB LWT) to ensure only one replica executes a given provisioning job. If the executing replica crashes, another replica picks up the job from ScyllaDB after a configurable heartbeat timeout (30 seconds). Provisioning jobs are idempotent (applying manifests to a vCluster is idempotent — if the resource exists, `kubectl apply` is a no-op).

**CF-Accounts (ScyllaDB):** ScyllaDB runs as a 3-node cluster inside the platform namespace, managed by the Scylla Operator. The `cf.api_keys` and `cf.tenants` tables use `CONSISTENCY QUORUM` for reads on the routing hot path to ensure consistent results across replicas.

**CF-IAM + Keycloak:** Keycloak runs as a 2-replica active-active cluster backed by the platform PostgreSQL. CF-IAM runs as 2+ replicas with no state of its own (state is in Keycloak and the OPA bundle store in MinIO).

### 10.5 Shared State Requirements

CF-Router and CF-Provisioner share state through ScyllaDB (CF-Accounts). There is no in-process shared state and no shared in-memory cache. This means:

- No sticky sessions
- No distributed cache (Redis, Memcached) is required
- Consistency is provided by ScyllaDB's replication model

For the routing hot path (API key lookup), the latency budget is:
- ScyllaDB read: ~1ms (with QUORUM consistency)
- Cryptographic hash: < 0.1ms
- Total router overhead per request: < 2ms

This is acceptable. If it becomes a bottleneck at very high request rates, a time-bounded local cache (Ristretto or similar, 5-second TTL) can be added to CF-Router for the API key hash→tenant-ID mapping.

### 10.6 Job Orchestration for Provisioning

Provisioning is inherently a multi-step, long-running operation (creating a vCluster can take 2–4 minutes). It must not be executed synchronously in an HTTP request handler.

**Model: task queue + workers**

CF-Provisioner maintains a `cf.provisioning_jobs` queue in ScyllaDB. The API handler for `POST /provision/services`:
1. Validates the request
2. Writes the job with status `QUEUED`
3. Returns `202 Accepted` with a `job_id`
4. The client polls `GET /provision/jobs/{job_id}` for status

A background worker goroutine in CF-Provisioner polls for `QUEUED` jobs, acquires a LWT lock, transitions to `PROVISIONING`, executes the handler, and transitions to `READY` or `FAILED`.

NATS JetStream is used as an optional push notification channel: when a job transitions to `READY`, CF-Provisioner publishes a `cf.platform.provisioning.completed` event. The UI console subscribes to this event over a websocket and updates the provisioning status indicator in real time without polling.

**Idempotency:** Every provisioning request includes an `idempotency_key` (client-generated, e.g., UUID or session+timestamp). If a request with a given `idempotency_key` already exists for a tenant, the handler returns the existing job record rather than creating a new one. This prevents duplicate service instances from UI double-clicks, network retries, or CI pipeline retries.

---

## 11. Recommended Tenant Isolation Spike

### 11.1 Context

The tenant isolation model proposed in Section 4 is architecturally sound but contains integration points and tradeoffs that must be validated empirically before being committed to as the platform's isolation guarantee. This spike de-risks the most critical architectural decision in this proposal.

### 11.2 Spike Goal

**Validate that vCluster-based tenant isolation meets CloudForge's requirements for network isolation, operational complexity, performance overhead, and provisioning speed.**

This is not an exploration spike. The expected outcome is a go/no-go decision on vCluster as the isolation mechanism, and if no-go, a ranked fallback recommendation with evidence.

### 11.3 What Must Be Tested

**Test 1: Network isolation correctness**  
Deploy two tenant virtual clusters (tenant-A, tenant-B) on the same k3d host cluster. Inside each vCluster, deploy an echo server. From tenant-A's vCluster, attempt to reach tenant-B's echo server by:
- Direct IP (using the pod IP of the tenant-B echo server pod)
- DNS (attempting to resolve `echo.default.svc.cluster.local` from tenant-A)
- Host-level arp/ip scan

Expected result: All attempts fail. DNS does not resolve across vCluster boundaries. Direct IP is unreachable due to Cilium network policy on the host.

**Test 2: Provisioning speed**  
Provision a new vCluster from scratch (via CF-Provisioner or equivalent script) and measure time to:
- vCluster API server ready
- Baseline manifests applied
- First service instance (NATS JetStream) ready

Measure 10 repeated provisioning cycles and record p50, p95 timings.  
Expected target: vCluster ready < 90s, NATS JetStream ready < 120s after that.

**Test 3: Provisioner communication path**  
Verify that CF-Provisioner (running in the platform namespace) can apply Kubernetes manifests to a tenant's vCluster via the stored kubeconfig, and that this communication is:
- Authenticated (service account with minimum required permissions)
- mTLS-encrypted
- Isolated: CF-Provisioner cannot reach any tenant namespace except through the vCluster API server

**Test 4: Resource overhead per tenant**  
Measure the memory and CPU overhead of an idle vCluster (API server + etcd + CoreDNS, no tenant services running).  
Expected target: < 512MB RAM, < 150m CPU per idle vCluster (vCluster v0.33.2 k3s-backed measured at **384 MiB / 64m** p50 steady-state — Run 3, 3 samples × 30s, vClusters 73 min idle).  
This determines the minimum host cluster sizing for N tenants.

**Test 5: Cilium policy enforcement**  
Deploy a pod in tenant-A's host namespace and attempt to open a TCP connection to tenant-B's host namespace on an arbitrary port. Verify that Cilium denies the connection and records a policy violation event.

**Test 6: Failure recovery**  
Kill the vCluster API server pod for tenant-A. Verify:
- Tenant-A services (NATS, etc.) continue operating in the vCluster's host namespace
- CF-Provisioner detects the API server failure and reports it
- The vCluster API server pod is restarted by Kubernetes and regains connectivity within 60s
- Provisioner resumes normal operation without manual intervention

### 11.4 Technology Options to Evaluate

| Option | Description | Evaluate |
|---|---|---|
| **vCluster (primary)** | Lightweight virtual Kubernetes cluster with k3s control plane | Primary recommendation |
| **vCluster with k0s** | vCluster using k0s as the inner control plane (lower memory) | Alternative control plane |
| **Namespace-only + strict Cilium** | Strong Cilium L7 policies instead of virtual clusters | Fallback if vCluster overhead unacceptable |
| **Dedicated node pools per tenant** | Physical node isolation, one node pool per tenant | Premium tier only, Phase 3 |

### 11.5 Measuring Success

| Metric | Pass Threshold | Fail Threshold |
|---|---|---|
| Cross-tenant network isolation | 100% blocked in all test vectors | Any vector succeeds |
| vCluster provisioning time (p95) | < 90s to API ready | > 180s |
| NATS provisioning inside vCluster (p95) | < 3min | > 5min |
| Idle vCluster RAM overhead | < 512MB | > 768MB |
| Provisioner communication correctness | 100% apply success rate | Any rejection |
| vCluster recovery after API server crash | < 60s to re-ready | > 120s |

### 11.6 Expected Outputs

1. A `spikes/tenant-isolation/` directory with:
   - Scripts to create a 3-node k3d cluster
   - Scripts to provision 2 vCluster instances
   - Go test program executing all 6 tests above and recording pass/fail with evidence
   - README with setup instructions
2. Measured results for all 6 test categories in a `FINDINGS.md`
3. A recommendation document: "Use vCluster / Use namespace isolation / Use dedicated node pools" with the evidence from the tests
4. Resource sizing formula: "N tenants requires M GB RAM and X CPU cores for the vCluster control planes alone"

### 11.7 Architectural Decisions That Must Come Out of This Spike

- Is vCluster the correct isolation primitive for CF v1?
- What is the minimum host cluster size to support 10 / 50 / 200 concurrent tenants?
- Does the provisioner communication model (kubeconfig → vCluster API) work under the required security constraints?
- Does Cilium correctly enforce cross-vCluster isolation in all tested attack vectors?
- What fallback recommendation is correct if vCluster overhead is unacceptable?

---

## 12. Improvement Path for Current CF

### 12.1 What Changes

**Must change:**

1. **Add CF-Provisioner as a new control plane service.** This does not replace CF-ResourceController — it separates execution (CF-Provisioner) from quota and inventory (CF-ResourceController). This is a new service with no existing analogue in v0.1.

2. **Add CF-Router as the explicit API entry point.** Currently, APISIX handles routing. CF-Router is a lightweight Go service that wraps APISIX's routing with tenant-aware token validation and context injection. APISIX becomes a lower-level transport; CF-Router owns the business logic of "which tenant is this request for."

3. **Add CF-Accounts as a new service backed by ScyllaDB.** User accounts, password hashes, API keys, and service profiles currently have no explicit home. This service owns that data and exposes a private API to CF-IAM, CF-Router, and CF-Provisioner.

4. **Introduce per-tenant virtual clusters.** This is the largest structural change. It requires vCluster operator deployment, CIDR allocation logic in CF-Provisioner, baseline manifest management, and updates to all provisioning handlers to target the tenant's vCluster API server rather than the host cluster.

5. **Introduce per-tenant gateway instances.** Envoy/Contour instances deployed inside each vCluster at onboarding time. CF-Provisioner manages their route configuration. The platform external load balancer must be updated to route to tenant gateways by SNI.

**Should change:**

6. **Onboarding must become a provisioning workflow, not just an account creation step.** The existing account creation flow in CF-IAM must be extended to trigger CF-Provisioner, which creates the vCluster and baseline resources. This is a workflow change, not a new component.

7. **The ScyllaDB deployment (from Task 0.7) must be configured for platform-internal use** (CF-Accounts data) rather than only as a tenant-facing service. The ScyllaDB cluster in the platform network holds CF-Accounts data. Tenant-facing ScyllaDB instances run inside tenant vClusters.

### 12.2 What Can Remain

- **CF-IAM (Keycloak + OPA):** Remains. Keycloak's realm model maps cleanly to the per-tenant vCluster model. CF-IAM manages realm lifecycle as part of the onboarding flow.
- **CF-SecretsConfig (OpenBao):** Remains. OpenBao stores kubeconfigs, per-tenant credentials, and platform secrets. The namespace model in OpenBao already supports per-tenant path isolation (`cf/tenants/{tenant-id}/...`).
- **CF-DBController, CF-FunctionTrigger, CF-EventRouter:** Remain as provisioning handlers within CF-Provisioner's handler registry. Their internal logic does not change — only the kubeconfig they operate against changes (from host cluster to tenant vCluster).
- **CF-Observability:** Remains. The observability agent deployed as a baseline manifest in each vCluster ships telemetry to the platform-level OpenSearch/Prometheus. Tenant-scoped index naming already provides isolation at the data level.
- **CF-GatewayControl:** Remains for platform API gateway management (APISIX routes). Tenant service gateway management moves to CF-Provisioner.
- **CF-AIRuntime, CF-ResourceController:** Remain as-is.
- **The NATS-based platform eventing backbone:** Remains. Provisioning lifecycle events continue to be published on NATS. The NATS cluster in the platform network is separate from tenant NATS instances inside vClusters.

### 12.3 What to Introduce First

**Priority 1 (blocks everything else):**
- Tenant isolation spike (Section 11) — must happen before vCluster adoption is committed
- CF-Accounts service + ScyllaDB schema
- CF-Router as a lightweight wrapper over APISIX

**Priority 2 (can be parallel after Priority 1):**
- CF-Provisioner skeleton with onboarding workflow and vCluster lifecycle
- API key model in CF-IAM (needed for CLI access)
- Per-tenant gateway model (Envoy instance in vCluster)

**Priority 3:**
- Provisioning handlers for each service type (NATS, MinIO, PostgreSQL, etc.)
- Provisioning job state machine UI in console
- CIDR allocation and management in CF-Provisioner

### 12.4 What Is Risky and Needs Validation

1. **vCluster overhead at scale.** vCluster v0.33.2 (k3s-backed) consumes **384 MiB RAM and 64m CPU** per idle control plane (p50 steady-state, Run 3: 3 samples × 30s, vClusters 73 min idle). A 50-tenant platform requires ~18.8 GiB RAM for vCluster control planes; 200 tenants requires ~75 GiB. A dedicated 96 GiB platform cluster (3× 8-core/32 GiB nodes) comfortably supports up to ~50 tenants + platform services; scale to 128 GiB for 200 tenants. Planning budget: 512 MiB RAM / 150m CPU per vCluster (includes safety headroom). The spike (Section 11) confirmed these numbers with the new multi-sample steady-state probe.

2. **Provisioner kubeconfig management.** Storing one kubeconfig per tenant in OpenBao creates a significant number of OpenBao secrets. The lease and renewal lifecycle must be tested at 100+ tenant scale.

3. **Cilium policy complexity at scale.** Each new tenant namespace requires a new set of Cilium network policies. At 200+ tenants, policy compilation and enforcement latency may increase. This must be profiled.

4. **Keycloak realm-per-tenant at scale.** Keycloak's realm model is suitable for < 100 tenants with individual realm instances. At larger scale, the Keycloak Organizations feature (Keycloak 24+) should replace the per-realm model. This migration must be planned proactively.

### 12.5 What Can Be Delivered Incrementally

The transition to per-tenant vClusters does not need to happen atomically. A phased delivery is possible:

**Increment 1:** Deploy CF-Accounts, CF-Router, and API key model. No vCluster yet. Existing namespace isolation continues.

**Increment 2:** Deploy CF-Provisioner with vCluster support. New tenants get vClusters. Existing tenants continue in namespace mode. Both modes run simultaneously.

**Increment 3:** Migrate existing tenants from namespace mode to vCluster mode (tooling required: tenant environment migrator script).

**Increment 4:** Deprecate namespace mode. All tenants in vCluster mode.

This allows the platform to ship the new control plane components (CF-Accounts, CF-Router) quickly, validate the provisioning model, and then layer in the vCluster isolation upgrade without a full-platform cutover.

---

## 13. Technical Tasks and Spikes

### Spike Tasks

---

**Spike: Tenant Network Isolation (vCluster)**  
- **Purpose:** Validate that vCluster provides sufficient network isolation, acceptable overhead, and a viable provisioner communication model.  
- **Key question:** Is vCluster the correct isolation primitive for CF v1, and what does it cost per tenant?  
- **Expected output:** FINDINGS.md with test results, go/no-go recommendation, host cluster sizing formula.  
- **Dependencies:** k3d, vCluster CLI/operator, Cilium, basic CF-Provisioner prototype  
- **Category:** Architecture, Networking, Infrastructure

---

**Spike: ScyllaDB as Control Plane Account Store**  
- **Purpose:** Validate ScyllaDB CQL for the CF-Accounts data model: correctness of LWT for idempotent provisioning, performance of the API key hash lookup on the routing hot path, and MV query performance.  
- **Key question:** Can ScyllaDB serve the routing hot path (API key lookup) at < 2ms p99, and do LWT-based state transitions work correctly under concurrent write load?  
- **Expected output:** Benchmark results, schema validation, go/no-go on ScyllaDB for CF-Accounts.  
- **Dependencies:** Task 0.7 (ScyllaDB deployment)  
- **Category:** Backend, Platform

---

**Spike: Provisioner Communication Security Model**  
- **Purpose:** Validate that the CF-Provisioner → vCluster API server communication path is secure, auditable, and resistant to privilege escalation.  
- **Key question:** Can CF-Provisioner apply manifests to a tenant's vCluster via stored kubeconfig without gaining access to other tenants' vClusters or to the host cluster beyond its own namespace?  
- **Expected output:** Security model documentation, RBAC configuration, penetration test results (attempting privilege escalation from provisioner service account).  
- **Dependencies:** Spike: Tenant Network Isolation  
- **Category:** Security, Architecture

---

**Spike: Cilium Network Policy Scaling**  
- **Purpose:** Measure Cilium policy compilation and enforcement latency as the number of tenant namespaces grows from 10 to 200.  
- **Key question:** At what tenant count does Cilium policy overhead become observable in packet latency? Is there a hard limit?  
- **Expected output:** Latency measurements at 10, 50, 100, 200 tenant namespaces. Recommendation on Cilium configuration (e.g., policy caching, partial evaluation) if degradation is observed.  
- **Dependencies:** Spike: Tenant Network Isolation  
- **Category:** Networking, Infrastructure

---

### Implementation Tasks

---

**Task: CF-Accounts Service**  
- **Purpose:** Build the ScyllaDB-backed service that stores customer accounts, users, API keys, and service profiles.  
- **Key question:** Where does account data live and how is it queried efficiently?  
- **Expected output:** Running CF-Accounts service with CRUD API for tenants, users, API keys, and service instances. Integration with CF-IAM for user creation. Integration with CF-Router for API key lookup.  
- **Dependencies:** Spike: ScyllaDB as Control Plane Account Store  
- **Category:** Backend, Platform

---

**Task: CF-Router Service**  
- **Purpose:** Build the tenant-aware API entry point that validates tokens/API keys, resolves tenant context, and routes to backend control plane services.  
- **Key question:** How are all platform API requests authenticated and routed to the correct backend?  
- **Expected output:** CF-Router deployed in front of all control plane services, handling JWT validation, API key lookup, scope enforcement, and header injection. p99 routing overhead < 3ms.  
- **Dependencies:** CF-Accounts (API key lookup), CF-IAM (JWT validation)  
- **Category:** Backend, Platform, Security

---

**Task: API Key Model in CF-IAM**  
- **Purpose:** Implement API key generation, rotation, and validation for CLI and programmatic access.  
- **Key question:** How do non-interactive clients authenticate with CloudForge?  
- **Expected output:** `cf apikey create`, `cf apikey list`, `cf apikey rotate`, `cf apikey delete` CLI commands. API key records in CF-Accounts. CF-Router integration for key validation.  
- **Dependencies:** CF-Accounts, CF-IAM, CF-Router  
- **Category:** Backend, Security

---

**Task: CF-Provisioner Service — Core**  
- **Purpose:** Build the CF-Provisioner service skeleton: onboarding workflow, job queue, state machine, and vCluster lifecycle management.  
- **Key question:** How are new tenant environments created and managed by the platform?  
- **Expected output:** CF-Provisioner service handling `POST /provision/onboard`, `POST /provision/services`, `GET /provision/jobs/{id}`. vCluster creation and teardown working end-to-end.  
- **Dependencies:** Spike: Tenant Network Isolation, CF-Accounts, CF-IAM, OpenBao kubeconfig storage  
- **Category:** Backend, Platform, Infrastructure

---

**Task: CF-Provisioner — Service Handlers (NATS, MinIO, PostgreSQL)**  
- **Purpose:** Implement provisioning handlers for each service type, targeting the tenant's vCluster API server.  
- **Key question:** How are individual services provisioned inside a tenant's isolated environment?  
- **Expected output:** Functional provisioning of NATS JetStream, MinIO, and PostgreSQL (with pgvector) inside a tenant vCluster. Credentials written to OpenBao. Endpoints recorded in CF-Accounts.  
- **Dependencies:** CF-Provisioner Core, Spike: Tenant Network Isolation  
- **Category:** Backend, Platform

---

**Task: Per-Tenant Gateway Provisioning**  
- **Purpose:** Deploy Envoy/Contour instance inside each tenant vCluster at onboarding time and implement private/public exposure mode switching.  
- **Key question:** How are tenant services exposed (privately and publicly) in a controlled, per-tenant way?  
- **Expected output:** Tenant gateway deployed as part of onboarding baseline manifests. CF-Provisioner creates Contour route objects when a service's exposure mode is set to non-private. Platform external LB routes to tenant gateways by SNI.  
- **Dependencies:** CF-Provisioner Core, Spike: Tenant Network Isolation  
- **Category:** Networking, Platform, Infrastructure

---

**Task: CIDR Allocation and Management**  
- **Purpose:** Implement a CIDR block allocator in CF-Provisioner that assigns non-overlapping pod and service CIDRs to each new vCluster.  
- **Key question:** How are tenant vCluster network addresses assigned without conflicts?  
- **Expected output:** A CIDR allocator that tracks used ranges in ScyllaDB, allocates from a configured supernet (e.g., 10.100.0.0/16 for pods, 10.200.0.0/16 for services), and handles concurrent allocation requests safely (LWT).  
- **Dependencies:** CF-Accounts, CF-Provisioner Core  
- **Category:** Networking, Backend

---

**Task: UI Console — Login and Session Model**  
- **Purpose:** Implement the UI console login flow using Keycloak OIDC with tenant-scoped realms.  
- **Key question:** How do customers log in to the CloudForge console and maintain authenticated sessions?  
- **Expected output:** Login page with email→tenant lookup, Keycloak OIDC redirect, JWT session management (15-min access token, 7-day refresh), logout with Keycloak session invalidation.  
- **Dependencies:** CF-IAM, Keycloak deployment  
- **Category:** Backend, Security

---

**Task: UI Console — Provisioning and Service Profile View**  
- **Purpose:** Implement the provisioning workflow in the UI console, including service request forms, provisioning job status tracking, and the service profile dashboard.  
- **Key question:** How do customers provision and manage their services through the console?  
- **Expected output:** Service catalog view, provisioning request form per service type, real-time provisioning status (via NATS push or polling), service profile page showing all provisioned instances.  
- **Dependencies:** CF-Provisioner, CF-Accounts, NATS platform events  
- **Category:** Backend, Platform

---

**Task: Tenant Environment Migrator**  
- **Purpose:** Build a migration tool to move existing tenants from namespace-only isolation to vCluster isolation, with no data loss and minimum downtime.  
- **Key question:** How do existing tenants transition to the new isolation model?  
- **Expected output:** Migration script/tool that creates a vCluster for an existing tenant, replicates all service instances, updates DNS and gateway routing, and switches traffic with a cut-over window of < 5 minutes.  
- **Dependencies:** CF-Provisioner, existing service handlers  
- **Category:** Platform, Ops

---

**Task: Keycloak Organizations Migration Planning**  
- **Purpose:** Design and document the migration from per-realm to Keycloak Organizations model for the managed offering at scale.  
- **Key question:** At what tenant count does the per-realm model become operationally unmanageable, and what is the migration path?  
- **Expected output:** Migration runbook, data migration scripts, target Organizations configuration, compatibility test results.  
- **Dependencies:** CF-IAM, Keycloak 24+ deployment  
- **Category:** Security, Ops

---

## 14. Final Recommendation

### The Structural Decision

**Build true network isolation into the platform foundation before onboarding production tenants.**

The current CF architecture is well-designed for its compute and service layers. The single gap that, left unaddressed, will make it difficult or impossible to operate as a production multi-tenant platform is the absence of per-customer network isolation. Every other improvement — better provisioning UX, richer service catalog, more sophisticated IAM — is undermined if two tenants' workloads share the same network fabric.

The specific mechanism recommended is **vCluster per tenant**, validated through the spike in Section 11. This is not the only approach (dedicated node pools, strong namespace isolation with Cilium) but it is the best balance of isolation strength, operational cost, and customer-explainability for a self-hosted SME platform.

### The Implementation Order

1. **Run the tenant isolation spike first.** Do not start building CF-Provisioner, CF-Accounts, or the per-tenant gateway model until the spike validates vCluster as the correct mechanism. If vCluster fails the spike, the fallback recommendation from the spike determines the next step.

2. **Build CF-Accounts and CF-Router as standalone services.** These are small, self-contained, and valuable independently. CF-Router can be deployed as a lightweight shim in front of the existing APISIX while vCluster work proceeds in parallel.

3. **Build CF-Provisioner incrementally.** Start with the onboarding workflow and vCluster lifecycle. Add service handlers one at a time. The first working handler (NATS JetStream) demonstrates the provisioner model end-to-end. Subsequent handlers follow the same pattern.

4. **Deploy per-tenant gateways as part of the onboarding baseline.** The tenant gateway is a deployment detail of the onboarding flow. Once CF-Provisioner can create a vCluster and apply baseline manifests, adding the Envoy/Contour instance to the baseline is a small incremental step.

5. **Run existing tenants in namespace mode during the transition.** New tenants get vClusters immediately. Existing tenants migrate using the tenant environment migrator tool. Both modes run simultaneously for the transition period.

### The Non-Negotiable Elements

- **Per-customer network isolation is a hard requirement, not a roadmap item.** It must be in place before any customer data is placed on the platform.
- **CF-Accounts must be ScyllaDB-backed.** MinIO is not a database. PostgreSQL is the wrong tradeoff for this access pattern.
- **API keys are the correct CLI authentication model.** OIDC flows do not work in automation. API keys with scopes, rotation, and hash-only storage are the industry standard.
- **Provisioning must be asynchronous with explicit state machine.** Synchronous provisioning calls that timeout are an operational liability. The job-based model with idempotency keys is mandatory from v1.
- **CF-Router must be stateless.** Stateful routers create deployment and scaling problems that are expensive to fix later. The ScyllaDB lookup is fast enough; no caching is needed in v1.

### What This Proposal Adds to the Current Architecture

| Area | Current CF (v0.1) | This Proposal |
|---|---|---|
| Tenant isolation | Kubernetes namespace + Cilium policy | vCluster per tenant (topological) |
| Onboarding | Account creation + Keycloak realm | Environment provisioning: vCluster + gateway + baseline |
| Service provisioning | Service-specific flows | Unified provisioning handler model with state machine |
| Account store | Not specified | CF-Accounts on ScyllaDB (explicit, designed) |
| API entry point | APISIX | CF-Router (tenant-aware, stateless) with APISIX as transport |
| API authentication (CLI) | Not specified | API keys (scoped, rotatable, hash-stored) |
| Service exposure | APISIX routes | Per-tenant gateway with private/public exposure modes |
| Control plane HA | 2+ replicas (existing) | 3+ replicas CF-Router, distributed locking for CF-Provisioner, active-active ScyllaDB |

This is not a rewrite. It is an upgrade. The service layer (CF-EventRouter, CF-FunctionTrigger, CF-AIRuntime, CF-DBController, CF-Observability) stays as designed. The control plane gains three new services (CF-Accounts, CF-Router, CF-Provisioner) and a new isolation mechanism (vCluster). Everything else evolves in place.

---

*End of Document*

**Revision history:**  
v1.0 — April 2026 — Initial architecture upgrade proposal addressing per-customer network isolation, control plane provisioner, account store, provisioning model, gateway model, CLI/console access, and HA design. Written as additive upgrade to `1-cloud-forge-architecture-proposal.v0.1.md`.

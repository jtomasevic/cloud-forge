# CloudForge: Tenant Isolation and Provisioning
## Architecture, Design, and Implementation Guide — v1.0

**Status:** Reference Architecture  
**Date:** May 2026  
**Audience:** Engineering teams, technical leads, architecture reviewers, technical stakeholders  
**References:**
- `docs/3-Introduce-CF-VPC.md` — CF-VPC proposal (source of truth for isolation architecture)
- `docs/2-cloud-forge-implementation-plan.v0.1.md` — Implementation plan
- `docs/CF-VPC-Service-Proposal.md` — VPC provisioning slice implementation
- `spikes/tenant-isolation/FINDINGS.md` — vCluster isolation spike (GO decision)
- `spikes/scylladb-accounts/FINDINGS.md` — ScyllaDB account store spike (GO decision)
- `spikes/cilium-enforcement/FINDINGS.md` — Cilium eBPF enforcement spike (GO decision)

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [Why Tenant Isolation Matters](#2-why-tenant-isolation-matters)
3. [Tenant Isolation Explained in Simple Terms](#3-tenant-isolation-explained-in-simple-terms)
4. [Technical Architecture of Tenant Isolation](#4-technical-architecture-of-tenant-isolation)
5. [Technology Choices and Responsibilities](#5-technology-choices-and-responsibilities)
6. [Network Diagram](#6-network-diagram)
7. [Component Diagram](#7-component-diagram)
8. [Provisioning Flow](#8-provisioning-flow)
9. [Relationship Between Accounts and Provisioned Tenant Environments](#9-relationship-between-accounts-and-provisioned-tenant-environments)
10. [Summary](#10-summary)

---

## 1. Introduction

CloudForge is a multi-tenant cloud platform. Multiple customers — called **tenants** — share the same underlying infrastructure. Each tenant can provision managed services (NATS event streaming, object storage, PostgreSQL databases, AI runtimes, serverless functions) through the CloudForge control plane API, CLI, and console.

Sharing infrastructure between tenants creates an immediate and non-negotiable responsibility: **no tenant must be able to see, reach, or affect another tenant's data or services**. This requirement — tenant isolation — is not optional and cannot be solved by convention or careful usage. It must be structurally enforced by the platform itself.

This document explains:
- What tenant isolation means and why it matters
- How CloudForge implements it technically
- Which technology enforces which boundary
- How tenant environments are created through the provisioning process
- How accounts, identity, and provisioned resources connect to each other

The document is written to be readable at two levels: a plain-language explanation first, followed by a precise technical specification.

---

## 2. Why Tenant Isolation Matters

### 2.1 The core requirement

When Tenant A and Tenant B both use CloudForge, neither should be able to reach the other's services — not even accidentally. This is not a security nicety or a stretch goal. It is the foundational trust guarantee that makes a multi-tenant platform viable.

Without strong isolation, the following failures become possible:

- A misconfigured service in Tenant A's environment can probe Tenant B's database or event stream
- A compromised workload in Tenant A can attempt lateral movement to Tenant B's environment
- A configuration bug in the platform can accidentally expose Tenant B's endpoints to Tenant A's pods
- Traffic from one tenant's application can accidentally reach another tenant's internal service

Each of these failures is a security breach, a compliance violation, and a commercial liability.

### 2.2 Four reasons isolation must be structural, not policy-based

Policy-based isolation (rules that *say* traffic is blocked) is weaker than structural isolation (infrastructure where the traffic *path does not exist*).

**Reason 1: Security.** A policy can be misconfigured. A missing label selector, an overly permissive CIDR range, or a transient policy controller failure can silently open a gap. When isolation is topological, there is no policy to misconfigure.

**Reason 2: Compliance.** Regulated industries (financial services, healthcare, government) require data residency guarantees that cannot be stated as "we have policies that should prevent cross-tenant access." The correct statement is "customer data cannot leave the customer's isolated network boundary." That statement is provable only with topological isolation.

**Reason 3: Blast radius containment.** A production incident in Tenant A must not affect Tenant B. Structural network separation contains failures within the boundary where they occur.

**Reason 4: Customer trust.** An enterprise customer evaluating a multi-tenant platform will ask: "How am I isolated from other customers?" The answer "we use network policies" is a conditional answer. The answer "you have your own private virtual cluster with its own network" is an unconditional answer.

### 2.3 What the platform guarantees

CloudForge guarantees:

- Each tenant has their own isolated virtual network (distinct pod CIDR, service CIDR, DNS namespace)
- No pod belonging to Tenant A can initiate a connection to any pod belonging to Tenant B
- The control plane's communication with each tenant is the only permitted cross-boundary flow, and it uses the Kubernetes API over mTLS — not a direct pod connection
- Revocation of a tenant's access (kubeconfig deletion from the secrets store) immediately eliminates the control plane's ability to manage that tenant's environment

---

## 3. Tenant Isolation Explained in Simple Terms

### 3.1 The apartment building analogy

Imagine a large apartment building. The building is the CloudForge platform. Each apartment is a tenant's environment.

- **Without isolation:** The building has corridors that connect every apartment to every other apartment. There are locks on the doors, but if a lock breaks or someone forgets to close a door, residents can enter each other's apartments.

- **With isolation (CloudForge model):** Each apartment is a fully self-contained unit with its own internal wiring, plumbing, and phone line. The corridors between apartments do not exist — there is no physical path from apartment A to apartment B's kitchen. The building manager (the control plane) can reach each apartment through a dedicated intercom line that only the manager can use.

### 3.2 What a tenant environment looks like

When a new customer signs up for CloudForge, the platform creates for them:

1. **Their own private Kubernetes cluster** — a lightweight virtual cluster (vCluster) running inside the CloudForge infrastructure, invisible to other tenants
2. **Their own private network** — a dedicated address range that no other tenant shares (e.g., `10.100.3.0/24` for pods, `10.200.3.0/24` for services)
3. **Their own private DNS** — service names resolve only inside the tenant's environment
4. **Their own credentials** — an API key that authenticates them to the CloudForge API; a kubeconfig stored securely in the platform's secrets vault for the control plane to manage their cluster

Services the tenant provisions (a NATS event bus, a PostgreSQL database, a MinIO object store) run **inside the tenant's virtual cluster**, on the tenant's private network. Other tenants' services cannot reach them.

### 3.3 What the control plane is

The **control plane** is the part of CloudForge that manages tenants and their environments. It does not run inside any tenant's environment — it runs in a separate, protected platform network. Tenants interact with the control plane through:

- The web console (authenticated via Keycloak SSO)
- The `cf` CLI (authenticated via API keys)
- Direct API calls (authenticated via API keys or JWTs)

The control plane creates and manages tenant environments, but it does so by speaking Kubernetes to the tenant's virtual cluster — not by running code inside the tenant's environment.

---

## 4. Technical Architecture of Tenant Isolation

### 4.1 Two-layer isolation model

CloudForge implements a **two-layer isolation architecture**:

```
Layer 1: Platform network (control plane namespace set)
         cf-system, cf-control-plane, cf-security, cf-data, cf-observability
         ↓ strict Cilium eBPF policies: ingress from platform API only
         ↓ no connectivity to tenant namespaces except via vCluster API port

Layer 2: Per-tenant virtual cluster (one per tenant)
         namespace: tenant-{tenant-id}
         vCluster API server (k3s-based StatefulSet)
         Own pod CIDR:     10.100.{n}.0/24
         Own service CIDR: 10.200.{n}.0/24
         Own DNS:          {service}.{namespace}.svc.cluster.local (internal to vCluster)
         Default-deny ingress from all other tenant namespaces
```

Layer 1 protects the control plane from tenants. Layer 2 protects tenants from each other.

### 4.2 Kubernetes namespaces alone are insufficient

A common misconception is that Kubernetes namespaces provide isolation. They do not. Kubernetes namespaces are a **scope boundary**, not a **network boundary**. All pods across all namespaces in a Kubernetes cluster share the same underlying network fabric unless a network policy explicitly denies cross-namespace traffic.

Network policy isolation is policy-dependent:

- A missing policy allows all traffic
- An incorrectly labeled policy silently excludes pods
- A policy controller restart can leave a window with no enforcement
- A privileged pod can bypass network policies entirely

**vCluster adds what namespaces cannot provide:**

| Property | Namespace + NetworkPolicy | vCluster |
|----------|--------------------------|---------|
| Separate pod network CIDR | No (shared cluster network) | Yes |
| Separate service CIDR | No (shared) | Yes |
| Separate DNS namespace | No | Yes |
| Separate Kubernetes RBAC | Partial (namespace roles only) | Full (dedicated API server) |
| Separate CRD scope | No (cluster-scoped) | Yes |
| Blast radius containment | Policy-dependent | Topological |
| Explainable to customer | "We use network policies" | "You have your own network" |

### 4.3 The vCluster model

Each tenant's environment is a **vCluster** — a Kubernetes API server (k3s-based) running as a StatefulSet inside a host cluster namespace dedicated to that tenant.

```
Host cluster namespace: tenant-acme-corp
│
├── vCluster StatefulSet (k3s API server)
│   └── Listens on port 6443 (accessible only from cf-system via Cilium policy)
│
├── Pod CIDR:     10.100.3.0/24   (assigned at provisioning, never overlaps)
├── Service CIDR: 10.200.3.0/24
│
└── Workloads (managed by CF-Provisioner via the vCluster API):
    ├── NATS JetStream cluster
    ├── MinIO object store
    ├── PostgreSQL (CloudNativePG)
    ├── Knative serving instances
    └── CF-Observability agent (sidecar injector)
```

From the tenant's perspective: they have a Kubernetes cluster.  
From the platform's perspective: they have a managed namespace set with an isolated API server.

### 4.4 Network enforcement: Cilium eBPF policies

Cilium enforces isolation at the kernel level using eBPF programs attached to the Linux network stack. Cilium policies are evaluated by the kernel — not in userspace, not by a proxy, and not in a pod that can be restarted. This makes enforcement significantly more reliable than traditional Kubernetes network policies.

**Platform namespace policies:**
```
TenantIsolationPolicy (applied to every tenant namespace):
  endpointSelector: {}   → applies to all pods in the tenant namespace
  ingress:
    - fromEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: tenant-{tenant-id}
            → only allows ingress from pods in the same tenant namespace

ProvisionerAccessPolicy (applied to every tenant namespace):
  endpointSelector:
    matchLabels:
      app: vcluster         → targets the vCluster API server pod
  ingress:
    - fromEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: cf-system
      toPorts:
        - ports:
            - port: "6443"
              protocol: TCP
            → allows ONLY the CF-Provisioner to reach the vCluster API server
```

The combined effect: cross-tenant traffic is structurally impossible. The provisioner can reach each vCluster API server on port 6443. All other cross-namespace traffic is eBPF-dropped at the kernel before it leaves the sending pod.

### 4.5 Three levels of traffic separation

**Level 1: Host network (Cilium eBPF)**  
No pod in tenant-A's namespace can initiate a connection to any pod in tenant-B's namespace. Default-deny is enforced at the kernel level by Cilium. No routing table entry exists between tenant namespaces.

**Level 2: vCluster network**  
Each vCluster has a non-overlapping pod CIDR. The virtual cluster's CoreDNS only resolves names within the vCluster's own namespace. A pod in vCluster-A cannot resolve `nats.default.svc.cluster.local` from vCluster-B — the DNS entry simply does not exist in vCluster-A's DNS.

**Level 3: Application credentials**  
NATS accounts, MinIO bucket policies, and database credentials are provisioned per-tenant within their vCluster. Even if a hypothetical cross-boundary path existed, application credentials would prevent access.

### 4.6 Control plane communication model

The CF-Provisioner communicates with each tenant vCluster exclusively through the vCluster's **Kubernetes API server** on port 6443, using a kubeconfig stored in OpenBao.

```
CF-Provisioner (in cf-system)
    │
    │  kubectl/client-go API calls (mTLS, port 6443)
    │  using kubeconfig from OpenBao
    ▼
vCluster API server (in tenant-{tenant-id})
    │
    ▼
Apply Kubernetes resources inside the tenant's virtual cluster
```

Key properties:
- No direct pod-to-pod connection between the platform and any tenant
- The provisioner uses a standard, audited protocol (Kubernetes API)
- Revoking the kubeconfig from OpenBao immediately removes the provisioner's access to that tenant
- The provisioner holds a platform service account inside each vCluster with the minimum required RBAC permissions

---

## 5. Technology Choices and Responsibilities

### 5.1 Technology responsibility map

| Technology | Role | Why chosen | Isolation/Provisioning responsibility |
|-----------|------|-----------|--------------------------------------|
| **vCluster** (Loft Labs) | Per-tenant virtual Kubernetes cluster | Topological isolation; 10–20× cheaper than dedicated clusters at SME scale; validated in spike | Creates the private network boundary; owns pod CIDR, service CIDR, DNS namespace, and RBAC for each tenant |
| **Cilium** (eBPF CNI) | Network policy enforcement | Kernel-level enforcement via eBPF; no userspace proxy; cannot be bypassed by misconfigured pods | Enforces default-deny between tenants at the host network level; controls which pods can reach the vCluster API server |
| **CF-Provisioner** | Provisioning execution engine | Dedicated control plane service for the full tenant lifecycle; keeps provisioning logic out of the router | Orchestrates vCluster creation, CIDR allocation, Cilium policy application, kubeconfig storage, API key generation, service deployment |
| **CF-Router** | Platform API entry point | Stateless, horizontally scalable; separates auth/routing from provisioning logic | Resolves tenant identity from every API request (JWT or API key); routes requests to the correct backend service |
| **ScyllaDB** (CQL native) | Account data store | Low-latency point reads (~1ms QUORUM) for the routing hot path; LWT for idempotent provisioning; already in stack | Stores tenant records, API key hashes, provisioning jobs, service instance inventory; materialised view for slug → tenant_id resolution |
| **OpenBao** | Secrets storage | Open-source Vault fork; KV v2 with versioning and metadata deletion; already deployed in dev cluster | Stores per-tenant vCluster kubeconfigs at `secret/cf/tenants/{tenant-id}/kubeconfig`; revocation removes provisioner access instantly |
| **Keycloak** | Identity and SSO | Standard OIDC/OAuth2 provider; per-tenant realm model | Issues short-lived JWTs for console sessions; each tenant has their own Keycloak realm, preventing cross-tenant identity bleed |
| **CF-Accounts** | Account data access layer | Encapsulates all ScyllaDB reads and writes behind typed Go API | Creates tenant records with LWT; resolves slugs via MV; stores API key hashes; tracks job state transitions |
| **BLAKE2b-256** | API key hashing | ~3× faster than SHA-256; secure one-way hash; standard for credential hashing | Hashes API keys before storage; CF-Router hashes the incoming bearer token and does a point lookup in ScyllaDB |
| **cert-manager** | TLS certificate automation | Industry standard Kubernetes-native cert manager | Issues TLS certificates for tenant service endpoints; required by Scylla Operator for webhook TLS |

### 5.2 What each technology does NOT do

This matters for understanding boundaries:

| Technology | What it does NOT do |
|-----------|---------------------|
| Cilium | Does not manage tenant lifecycle; does not store secrets; does not route API requests |
| vCluster | Does not authenticate requests; does not store account data; does not manage CIDR allocation |
| OpenBao | Is not a user database; does not authenticate API requests; does not run policies |
| ScyllaDB | Does not store raw secrets or credentials; does not enforce network policies |
| Keycloak | Does not store API keys; does not route API requests; does not manage vCluster lifecycle |
| CF-Router | Does not provision services; does not hold state; does not cache tenant data between requests |

### 5.3 Design decisions and alternatives considered

**Why vCluster and not namespace-only isolation?**  
Namespace isolation is policy-dependent. vCluster provides topological separation. The tenant-isolation spike validated that vCluster provides all six required properties at acceptable performance (vCluster creation: p95 ~8.7s cold, p95 ~2.5s warm).

**Why vCluster and not dedicated clusters?**  
A dedicated cluster per tenant requires dedicated nodes, load balancers, and significant operational overhead. vCluster is 10–20× cheaper at SME tenant counts and shares the same host cluster infrastructure. The isolation properties are equivalent for the VPC provisioning model.

**Why ScyllaDB and not PostgreSQL for accounts?**  
The account store's access patterns are primarily high-frequency point reads (API key lookup on every request, tenant slug resolution). ScyllaDB provides lower latency (~1ms QUORUM) and better horizontal scalability for these patterns than PostgreSQL. PostgreSQL remains the right choice for platform operational data requiring joins and aggregations.

**Why API keys and not OIDC for CLI access?**  
Browser-based OAuth2/OIDC flows are impractical for CLI and CI/CD usage. API keys are the industry standard for programmatic access (GitHub PATs, AWS access keys, Stripe keys). The BLAKE2b hash model means the raw key is never stored, reducing breach impact.

---

## 6. Network Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         HOST KUBERNETES CLUSTER                             │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     PLATFORM NETWORK                                 │   │
│  │   Namespaces: cf-system · cf-control-plane · cf-security · cf-data  │   │
│  │                                                                      │   │
│  │  ┌──────────────┐   ┌──────────────┐   ┌──────────────────────────┐ │   │
│  │  │  CF-Router   │   │  CF-IAM      │   │  CF-Provisioner          │ │   │
│  │  │  port :8080  │   │  Keycloak    │   │  (VPC provisioning)      │ │   │
│  │  │  stateless   │   │  OPA         │   │  CIDR allocator          │ │   │
│  │  └──────┬───────┘   └──────────────┘   │  vCluster lifecycle      │ │   │
│  │         │                              │  policy application      │ │   │
│  │  ┌──────▼───────┐   ┌──────────────┐   └───────────┬──────────────┘ │   │
│  │  │  CF-Accounts │   │  OpenBao     │               │               │   │
│  │  │  (ScyllaDB)  │   │  cf-security │               │ port 6443     │   │
│  │  │  tenant data │   │  kubeconfigs │               │ (mTLS, only   │   │
│  │  │  API keys    │   │  secrets     │               │  from         │   │
│  │  └──────────────┘   └──────────────┘               │  cf-system)   │   │
│  └─────────────────────────────────────────────────────│───────────────┘   │
│                         Cilium default-deny between platform and tenants    │
│                                              │                             │
│             ┌────────────────────────────────┼────────────────────────┐   │
│             │        TENANT LAYER            │                        │   │
│             │                                ▼                        │   │
│  ┌──────────┴────────────────┐    ┌──────────────────────────┐       │   │
│  │  TENANT A NAMESPACE        │    │  TENANT B NAMESPACE       │       │   │
│  │  tenant-acme-corp          │    │  tenant-beta-inc          │       │   │
│  │                            │    │                           │       │   │
│  │  ┌──────────────────────┐  │    │  ┌─────────────────────┐ │       │   │
│  │  │  vCluster-acme-corp  │  │    │  │  vCluster-beta-inc  │ │       │   │
│  │  │  (k3s API server)    │  │    │  │  (k3s API server)   │ │       │   │
│  │  │                      │  │    │  │                     │ │       │   │
│  │  │  ┌────────────────┐  │  │    │  │  ┌───────────────┐  │ │       │   │
│  │  │  │  NATS cluster  │  │  │    │  │  │  NATS cluster │  │ │       │   │
│  │  │  │  PostgreSQL    │  │  │    │  │  │  PostgreSQL   │  │ │       │   │
│  │  │  │  MinIO         │  │  │    │  │  │  MinIO        │  │ │       │   │
│  │  │  │  AI runtime    │  │  │    │  │  └───────────────┘  │ │       │   │
│  │  │  └────────────────┘  │  │    │  └─────────────────────┘ │       │   │
│  │  └──────────────────────┘  │    └──────────────────────────┘       │   │
│  │  Pod CIDR:  10.100.1.0/24  │        Pod CIDR:  10.100.2.0/24       │   │
│  │  Svc CIDR:  10.200.1.0/24  │        Svc CIDR:  10.200.2.0/24       │   │
│  └────────────────────────────┘    ──────────────────────────────      │   │
│                                                                         │   │
│      ↑ Cilium TenantIsolationPolicy: default-deny cross-namespace       │   │
│        No route exists between tenant-acme-corp and tenant-beta-inc     │   │
│                                                                         │   │
│  ┌─────────────────────────────────────────────────────────────────┐   │   │
│  │              EXTERNAL ACCESS                                     │   │   │
│  │  Platform External LB ──► CF-Router (control plane API)         │   │   │
│  │  Tenant LB             ──► Per-tenant Envoy/Contour gateway      │   │   │
│  │                            (routes to tenant's own services)     │   │   │
│  └─────────────────────────────────────────────────────────────────┘   │   │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key network rules enforced by Cilium eBPF:**

| Source | Destination | Allowed | Policy |
|--------|------------|---------|--------|
| CF-Provisioner (cf-system) | vCluster API server port 6443 | Yes | `ProvisionerAccessPolicy` |
| Tenant A pod | Tenant B pod (any port) | **No** | `TenantIsolationPolicy` default-deny |
| Tenant A pod | Platform network | **No** | Platform isolation policy |
| Platform CF-Router | Platform backends | Yes | Platform-to-platform allow |
| External LB | CF-Router | Yes | Ingress rule |
| CF-Observability | Tenant metrics endpoint | Yes | Explicit scrape allow |

---

## 7. Component Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          CLOUDFORGE PLATFORM                                │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                      REQUEST ENTRY LAYER                              │  │
│  │                                                                       │  │
│  │    Browser / cf CLI / API clients                                     │  │
│  │         │                    │                                        │  │
│  │    JWT (console)        API key (CLI/programmatic)                    │  │
│  │         └──────────┬─────────┘                                        │  │
│  │                    ▼                                                  │  │
│  │             ┌─────────────┐                                           │  │
│  │             │  CF-Router  │  ← single entry point for all API traffic │  │
│  │             │  stateless  │  ← validates JWT / API key on every req   │  │
│  │             │  horizontal │  ← resolves tenant_id                     │  │
│  │             └──────┬──────┘  ← routes to backend service              │  │
│  └────────────────────│──────────────────────────────────────────────────┘  │
│                       │ X-CF-Tenant-ID header injected                      │
│  ┌────────────────────▼──────────────────────────────────────────────────┐  │
│  │                      CONTROL PLANE SERVICES                           │  │
│  │                                                                       │  │
│  │  ┌────────────────┐  ┌──────────────┐  ┌────────────────────────┐    │  │
│  │  │  CF-IAM        │  │  CF-Accounts │  │  CF-Provisioner        │    │  │
│  │  │                │  │              │  │                        │    │  │
│  │  │  Keycloak      │  │  TenantStore │  │  VPC provisioning      │    │  │
│  │  │  OPA policies  │  │  APIKeyStore │  │  CIDR allocation       │    │  │
│  │  │  JWT issuance  │  │  JobStore    │  │  vCluster lifecycle    │    │  │
│  │  │  realm/org mgmt│  │              │  │  CNP management        │    │  │
│  │  └────────────────┘  └──────┬───────┘  └──────────┬─────────────┘    │  │
│  │                             │                     │                   │  │
│  └─────────────────────────────│─────────────────────│───────────────────┘  │
│                                │                     │                      │
│  ┌─────────────────────────────│─────────────────────│───────────────────┐  │
│  │                      DATA LAYER                    │                   │  │
│  │                             │                     │                   │  │
│  │  ┌───────────────────┐      │         ┌───────────▼────────────────┐  │  │
│  │  │  ScyllaDB          │◄─────┘         │  OpenBao                   │  │  │
│  │  │                   │                │                            │  │  │
│  │  │  cf.tenants       │                │  secret/cf/tenants/        │  │  │
│  │  │  cf.api_keys      │                │   {tenant-id}/kubeconfig   │  │  │
│  │  │  cf.provisioning  │                │  secret/cf/tenants/        │  │  │
│  │  │    _jobs          │                │   {tenant-id}/credentials  │  │  │
│  │  │  cf.service_      │                └────────────────────────────┘  │  │
│  │  │    instances      │                                                 │  │
│  │  └───────────────────┘                                                 │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                      TENANT LAYER (one per tenant)                    │  │
│  │                                                                       │  │
│  │  ┌─────────────────────────────────────────────────────────────────┐ │  │
│  │  │  namespace: tenant-{tenant-id}                                   │ │  │
│  │  │                                                                  │ │  │
│  │  │  ┌──────────────────────────────────────────────────────────┐   │ │  │
│  │  │  │  vCluster (k3s API server StatefulSet)                   │   │ │  │
│  │  │  │                                                          │   │ │  │
│  │  │  │  ┌───────────┐ ┌───────────┐ ┌───────────┐ ┌─────────┐  │   │ │  │
│  │  │  │  │   NATS    │ │PostgreSQL │ │   MinIO   │ │Knative  │  │   │ │  │
│  │  │  │  │JetStream  │ │(operator) │ │           │ │serving  │  │   │ │  │
│  │  │  │  └───────────┘ └───────────┘ └───────────┘ └─────────┘  │   │ │  │
│  │  │  │                                                          │   │ │  │
│  │  │  │  ┌──────────────┐   ┌─────────────────────────────────┐  │   │ │  │
│  │  │  │  │ CF-Observ.   │   │  Envoy/Contour tenant gateway   │  │   │ │  │
│  │  │  │  │ agent        │   │  (public/private service expo.) │  │   │ │  │
│  │  │  │  └──────────────┘   └─────────────────────────────────┘  │   │ │  │
│  │  │  └──────────────────────────────────────────────────────────┘   │ │  │
│  │  │                                                                  │ │  │
│  │  │  Cilium policies:  TenantIsolationPolicy + ProvisionerAccess     │ │  │
│  │  │  Pod CIDR: 10.100.{n}.0/24   Svc CIDR: 10.200.{n}.0/24          │ │  │
│  │  └─────────────────────────────────────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Component interaction summary:**

| Component | Calls | Called by |
|-----------|-------|-----------|
| CF-Router | CF-Accounts (API key/slug lookup), CF-IAM (JWT validation), CF-Provisioner, CF-IAM | External clients (browser, CLI, API) |
| CF-Provisioner | vCluster CLI/operator, OpenBao (kubeconfig store), ScyllaDB (account/job updates), Cilium (CNP apply via kubectl) | CF-Router (on /api/v1/vpc/provision requests) |
| CF-Accounts | ScyllaDB | CF-Router (every request), CF-Provisioner (state updates) |
| vCluster | Host k8s API | CF-Provisioner (create/delete/manage) |
| OpenBao | — | CF-Provisioner (read/write kubeconfigs), CF-SecretsConfig (tenant secrets) |

---

## 8. Provisioning Flow

### 8.1 Tenant onboarding (VPC provisioning)

The following sequence occurs when a new customer signs up or when `POST /api/v1/vpc/provision` is called:

```
1. REQUEST RECEIVED
   POST /api/v1/vpc/provision
   Authorization: Bearer cf_live_...
   Body: { "tenant_id": "acme-corp", "display_name": "Acme Corp", "plan": "starter" }

                │
                ▼

2. AUTHENTICATION & ROUTING (CF-Router)
   - Hash the bearer token with BLAKE2b-256
   - Look up hash in ScyllaDB cf.api_keys → { tenant_id, scopes, status }
   - Verify status = ACTIVE, scope includes "provision:write"
   - Forward request to CF-Provisioner with X-CF-Tenant-ID header

                │
                ▼

3. INPUT VALIDATION (CF-Provisioner)
   - Validate tenant_id format (DNS label: lowercase alphanumeric + hyphens)
   - Check uniqueness in ScyllaDB via LWT (IF NOT EXISTS)
   - Validate plan name is recognised ("starter", "pro", "enterprise")

                │
                ▼

4. JOB CREATION (CF-Provisioner → ScyllaDB)
   - Insert idempotency record: cf.provisioning_jobs_by_idem (LWT IF NOT EXISTS)
     → If same (tenant_id, idempotency_key) already exists, return existing job_id
   - Insert job row: cf.provisioning_jobs, status = QUEUED
   - Return 202 Accepted to caller: { job_id, status: "QUEUED" }

                │
                ▼

5. BACKGROUND WORKFLOW (CF-Provisioner goroutine)

   5a. CLAIM JOB
       UPDATE cf.provisioning_jobs SET status = 'PROVISIONING'
       WHERE tenant_id = ? AND job_id = ?
       IF status = 'QUEUED'                           ← LWT: only 1 worker wins

   5b. CREATE TENANT RECORD
       INSERT INTO cf.tenants ... IF NOT EXISTS       ← LWT: prevents duplicate slugs

   5c. ALLOCATE CIDRs
       For i = 1..254:
         INSERT INTO cf.cidr_allocations (cidr=10.100.i.0/24, ...) IF NOT EXISTS
         If applied: pod CIDR claimed
         INSERT INTO cf.cidr_allocations (cidr=10.200.i.0/24, ...) IF NOT EXISTS
         If applied: svc CIDR claimed → CIDRPair complete
         Otherwise: advance to next index

   5d. CREATE HOST NAMESPACE
       kubectl create namespace tenant-acme-corp
       Labels: cloudforge.io/tenant-id=acme-corp, cloudforge.io/tier=tenant

   5e. APPLY CILIUM POLICIES
       TenantIsolationPolicy (default-deny ingress, same-namespace only)
       ProvisionerAccessPolicy (cf-system → vCluster:6443 only)

   5f. CREATE vCLUSTER
       vcluster create acme-corp \
         --namespace tenant-acme-corp \
         --pod-cidr 10.100.3.0/24 \
         --service-cidr 10.200.3.0/24 \
         --connect=false
       Wait for StatefulSet ready (kubectl rollout status, up to 90s)

   5g. STORE KUBECONFIG
       vcluster connect acme-corp --print
       → OpenBao PUT secret/cf/tenants/acme-corp/kubeconfig

   5h. GENERATE API KEY
       Generate 32 random bytes → "cf_live_" + hex(32 bytes)
       Hash with BLAKE2b-256 → key_hash
       INSERT INTO cf.api_keys (key_hash, key_id, tenant_id, ...) IF NOT EXISTS
       Raw key will be returned ONCE in the job result — never stored

   5i. ACTIVATE TENANT
       UPDATE cf.tenants SET pod_cidr = ?, svc_cidr = ?, updated_at = ?
       UPDATE cf.tenants SET status = 'ACTIVE'

   5j. COMPLETE JOB
       UPDATE cf.provisioning_jobs
         SET status = 'READY',
             result = '{"api_key":"cf_live_...","api_key_id":"...","vpc_info":{...}}',
             completed_at = ?

                │
                ▼

6. CLIENT POLLS FOR RESULT
   GET /api/v1/vpc/jobs/{job_id}
   Response: {
     "status": "READY",
     "api_key": "cf_live_a1b2c3...",    ← returned ONCE; store it safely
     "api_key_id": "uuid",
     "vpc_info": {
       "pod_cidr": "10.100.3.0/24",
       "service_cidr": "10.200.3.0/24",
       "vcluster_ready": true
     }
   }
```

Total expected time: **2–4 minutes** for a new vCluster (cold container images: ~8.7s p95 warm per spike validation).

### 8.2 Service provisioning (inside an existing tenant environment)

Once the tenant's virtual cluster is ready, additional services (NATS, PostgreSQL, MinIO, AI runtime) are provisioned on request:

```
1. Tenant sends service provisioning request:
   POST /api/v1/provision/services
   Authorization: Bearer cf_live_...
   Body: { service_type: "POSTGRESQL", display_name: "app-db", parameters: {...} }

2. CF-Router resolves tenant_id from API key (ScyllaDB lookup)

3. CF-Provisioner receives request with X-CF-Tenant-ID injected

4. CF-ResourceController validates quota (does this tenant have capacity?)

5. CF-Provisioner creates provisioning job in ScyllaDB

6. Background worker:
   a. Retrieve tenant's vCluster kubeconfig from OpenBao
   b. Create *rest.Config from kubeconfig
   c. Call the service's ProvisioningHandler.Provision(ctx, kubeconfig, req)
      - Applies Kubernetes manifests to the tenant's vCluster API server
      - Polls for operator readiness (PostgreSQL ready, NATS cluster formed, etc.)
   d. Write provisioned credentials to OpenBao:
        secret/cf/tenants/{tenant-id}/{service-id}/credentials
   e. Update cf.service_instances in ScyllaDB (status: READY, endpoints: {...})
   f. Mark provisioning job READY

7. Tenant can now connect to their service using the returned credentials
```

### 8.3 Deprovisioning

Deprovisioning follows the reverse order, ensuring resources are cleaned up completely:

```
DELETE /api/v1/vpc/{tenant_id}

Step 1: Revoke all API keys for the tenant
        UPDATE cf.api_keys SET status = 'REVOKED' WHERE key_hash = ?

Step 2: Revoke kubeconfig from OpenBao
        DELETE secret/cf/tenants/{tenant-id}/kubeconfig
        (CF-Provisioner immediately loses ability to manage the tenant's vCluster)

Step 3: Delete vCluster
        vcluster delete {tenant-id} --namespace tenant-{tenant-id}
        (All tenant workloads are removed; the private network ceases to exist)

Step 4: Delete host namespace
        kubectl delete namespace tenant-{tenant-id}
        (Cilium policies in the namespace are removed)

Step 5: Release CIDR allocations
        DELETE FROM cf.cidr_allocations WHERE cidr IN (pod_cidr, svc_cidr)
        (CIDRs available for future tenant allocation)

Step 6: Mark tenant DELETED
        UPDATE cf.tenants SET status = 'DELETED'

All steps are idempotent — safe to call twice after a partial failure.
```

### 8.4 Idempotency guarantees

Every write operation in the provisioning workflow uses ScyllaDB Lightweight Transactions (LWT) to prevent duplicate execution:

| Operation | LWT condition | Effect |
|-----------|--------------|--------|
| Create tenant record | `IF NOT EXISTS` | Two concurrent requests for the same slug produce one tenant record |
| Allocate CIDR | `IF NOT EXISTS` | Two concurrent allocations for the same CIDR block: only one wins; the other advances to the next index |
| Claim provisioning job | `IF status = 'QUEUED'` | Two CF-Provisioner replicas racing for the same job: exactly one executes it |
| Create API key | `IF NOT EXISTS` | Duplicate Store calls are silently deduplicated |

---

## 9. Relationship Between Accounts and Provisioned Tenant Environments

### 9.1 The conceptual model

CloudForge draws a clear boundary between **who a tenant is** (account data) and **what a tenant has** (provisioned environment and services):

```
ACCOUNT LAYER (CF-Accounts, ScyllaDB)
────────────────────────────────────────────────────────────────────────────
  Tenant record     → identity, status, plan, CIDR allocation
  User records      → who can authenticate as this tenant
  API keys          → programmatic access credentials (hash-only)
  Provisioning jobs → history and current state of all provisioning operations
  Service instances → inventory of what is provisioned, with endpoints
────────────────────────────────────────────────────────────────────────────

          ↕  CF-Provisioner reads accounts to find tenants
             CF-Provisioner writes accounts to record provisioned state

────────────────────────────────────────────────────────────────────────────
ENVIRONMENT LAYER (OpenBao + vCluster + Kubernetes resources)
────────────────────────────────────────────────────────────────────────────
  vCluster          → the tenant's isolated virtual Kubernetes cluster
  Kubeconfig        → stored in OpenBao; allows CF-Provisioner to manage vCluster
  Services          → NATS, PostgreSQL, MinIO etc. deployed inside the vCluster
  Credentials       → stored in OpenBao per service; never in ScyllaDB
  Cilium policies   → network isolation applied to the host namespace
────────────────────────────────────────────────────────────────────────────
```

### 9.2 How they connect: the lifecycle chain

```
Account creation
    │
    │  CF-Provisioner creates cf.tenants record (status: PROVISIONING)
    │  tenant_id = UUID generated at account creation
    ▼
CIDR allocation
    │
    │  CF-Provisioner writes pod_cidr + svc_cidr to cf.tenants
    │  CIDRs also recorded in cf.cidr_allocations (for deprovisioning release)
    ▼
vCluster creation
    │
    │  CF-Provisioner creates the vCluster using the allocated CIDRs
    │  vCluster is named after the tenant_id
    │  Cilium policies reference the tenant's namespace
    ▼
Kubeconfig stored in OpenBao
    │
    │  Path: secret/cf/tenants/{tenant-id}/kubeconfig
    │  This is the link between the account layer and the environment layer:
    │  CF-Provisioner uses the kubeconfig to talk to the tenant's vCluster
    ▼
API key generated
    │
    │  key_hash stored in cf.api_keys (tenant_id foreign key)
    │  raw key returned once in the job result
    │  This is the link between the account layer and API access:
    │  CF-Router uses the key_hash to resolve tenant_id on every request
    ▼
Tenant status → ACTIVE
    │
    │  CF-Accounts records the tenant as operational
    ▼
Service provisioning (repeated for each service)
    │
    │  CF-Provisioner reads kubeconfig from OpenBao
    │  Applies Kubernetes manifests to vCluster using kubeconfig
    │  Writes provisioned endpoints to cf.service_instances
    │  Writes service credentials to OpenBao (per-service path)
    ▼
Service inventory queryable by tenant
    │
    │  CF-Router routes GET /api/v1/accounts/services to CF-Accounts
    │  CF-Accounts reads cf.service_instances for the resolved tenant_id
```

### 9.3 Why tenant_id is the universal key

The `tenant_id` UUID is the central linking key across all layers:

| Layer | How tenant_id is used |
|-------|-----------------------|
| ScyllaDB `cf.tenants` | Primary key; `tenants_by_slug` MV resolves slug → tenant_id |
| ScyllaDB `cf.api_keys` | Foreign key field; allows CF-Router to map key_hash → tenant_id |
| ScyllaDB `cf.service_instances` | Partition key; all services for a tenant are co-located on the same ScyllaDB shard |
| ScyllaDB `cf.provisioning_jobs` | Partition key; all jobs for a tenant are co-located |
| OpenBao | Path prefix: `secret/cf/tenants/{tenant-id}/...` |
| Host cluster namespace | `tenant-{tenant-id}` |
| vCluster name | `{tenant-id}` |
| Cilium policy namespace selector | `tenant-{tenant-id}` |
| CF-Router header | `X-CF-Tenant-ID: {tenant-id}` (injected on every forwarded request) |

This design means any component in the platform can unambiguously identify the tenant context from a single UUID. There is no mapping table, no separate identifier, and no ambiguity between the account identity and the provisioned environment identity.

### 9.4 What happens if the account is deleted

When a tenant's account is deleted, the lifecycle chain is unwound in strict order:

1. API keys are revoked → CF-Router immediately rejects further requests
2. Kubeconfig is deleted from OpenBao → CF-Provisioner cannot manage the tenant's vCluster
3. vCluster is deleted → all tenant services and data are removed
4. Host namespace is deleted → Cilium policies are removed; network boundary dissolved
5. CIDR blocks are released → returned to the allocation pool
6. Tenant record is marked DELETED in ScyllaDB

The ordering guarantees that access is revoked before data is removed, and data is removed before records are cleaned up.

---

## 10. Summary

### What CloudForge's tenant isolation model provides

| Property | Mechanism | Guarantee level |
|----------|-----------|----------------|
| Network separation | vCluster per tenant (separate pod CIDR, service CIDR) | Topological — traffic path does not exist |
| Policy enforcement | Cilium eBPF TenantIsolationPolicy + ProvisionerAccessPolicy | Kernel-level — not bypassable by userspace |
| Secrets isolation | OpenBao per-tenant paths with scoped policies | Cryptographic — wrong token = 403 |
| Identity isolation | Keycloak per-tenant realms; API key → tenant_id binding | Cryptographic + database |
| Data isolation | ScyllaDB partition key = tenant_id; no cross-tenant queries | Database-level |
| Provisioning isolation | CF-Provisioner uses per-tenant kubeconfig from OpenBao | Protocol-level — Kubernetes API scoped to tenant's vCluster |

### The provisioning model in one sentence

When a tenant signs up, CF-Provisioner creates a private virtual cluster with its own network, applies Cilium policies to enforce that no other tenant can reach it, stores the credentials to manage it in OpenBao, and records the tenant's identity and service inventory in ScyllaDB — so the control plane always knows who the tenant is, where their environment lives, and what is running inside it.

### What has been validated

All three spike workstreams completed with GO decisions:

| Spike | Key finding | Impact |
|-------|-------------|--------|
| `spikes/tenant-isolation/` | vCluster provides full 6-property isolation at p95 ~8.7s creation time | Confirmed vCluster as the isolation primitive |
| `spikes/scylladb-accounts/` | API key lookup p99 ~1ms QUORUM; LWT correctness: exactly 1 winner per 20 concurrent goroutines | Confirmed ScyllaDB for the account store hot path |
| `spikes/cilium-enforcement/` | Cilium eBPF policies enforce default-deny at kernel level; `hubble observe` provides real-time flow visibility | Confirmed Cilium as the network enforcement layer |

### What is implemented

| Component | Location | Status |
|-----------|----------|--------|
| CNP rendering (TenantIsolationPolicy, ProvisionerAccessPolicy) | `internal/provisioner/cnp.go` | Done |
| Kubeconfig Store/Retrieve/Revoke in OpenBao | `internal/provisioner/kubeconfig.go` | Done |
| CIDR allocation with LWT (CIDRAllocationDB interface + GocqlCIDRDB) | `internal/provisioner/cidr.go` | Done |
| vCluster Create/Delete/Wait | `internal/provisioner/vcluster.go` | Done |
| API key generation + BLAKE2b hashing | `internal/provisioner/apikey.go` | Done |
| ScyllaDB schema (tenants, api_keys, cidr_allocations, provisioning_jobs) | `internal/accounts/schema/schema.cql` | Done |
| TenantStore, APIKeyStore, JobStore | `internal/accounts/` | Done |
| CF-Provisioner HTTP service (VPC provisioning slice) | `cmd/cf-provisioner/main.go` | Done |
| OpenBao in dev cluster | `deploy/kustomize/base/openbao.yaml` | Done |
| ScyllaDB in dev cluster | `deploy/kustomize/components/scylladb/` | Done |
| Cilium + Hubble in dev cluster | `Makefile install-cilium` | Done |

### Next steps

1. **CF-Router** — implement the token resolution and routing service
2. **Keycloak realm provisioning** — add realm creation to the onboarding flow
3. **Service handlers** — implement NATSHandler, MinIOHandler, PostgreSQLHandler
4. **Tenant gateway** — deploy per-tenant Envoy/Contour inside each vCluster
5. **CF-ResourceController** — add quota validation to the provisioning workflow
6. **CF-Observability** — add the platform observability agent to baseline vCluster manifests

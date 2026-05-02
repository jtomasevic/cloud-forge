# CF-VPC Service Proposal
## Private Network Provisioning for CloudForge Tenants

**Status:** Proposal — Ready for Implementation  
**Date:** May 2026  
**Author:** Architecture Review  
**References:**
- `docs/3-Introduce-CF-VPC.md` — full CF-VPC architecture proposal
- `spikes/tenant-isolation/FINDINGS.md` — vCluster isolation validated (GO)
- `spikes/scylladb-accounts/FINDINGS.md` — ScyllaDB account store validated (GO)
- `spikes/cilium-enforcement/FINDINGS.md` — Cilium eBPF enforcement validated (GO)
- `internal/provisioner/` — CNP rendering and kubeconfig storage (done)

---

## What already exists (the foundation)

Three spikes are complete and all returned GO decisions. Two core primitives are
implemented. The service can be built on real validated infrastructure already
running in the dev cluster.

| Piece | Location | Status |
|-------|----------|--------|
| vCluster isolation validated (all 6 tests) | `spikes/tenant-isolation/` | **GO** |
| ScyllaDB as account store validated (LWT, API key lookup, MV queries) | `spikes/scylladb-accounts/` | **GO** |
| Cilium eBPF enforcement validated | `spikes/cilium-enforcement/` | **GO** |
| CNP rendering per tenant | `internal/provisioner/cnp.go` | Done |
| Kubeconfig Store/Retrieve/Revoke in OpenBao | `internal/provisioner/kubeconfig.go` | Done |
| OpenBao running in dev cluster (`cf-security`) | `deploy/kustomize/base/openbao.yaml` | Done |
| ScyllaDB running in dev cluster (`cf-data`) | `deploy/kustomize/components/scylladb/` | Done |
| Cilium + Hubble in dev cluster | `Makefile` `install-cilium` | Done |

---

## The service: `cf-provisioner` — VPC provisioning slice

This is **not** a full CF-Provisioner implementation. It is the narrowest useful
slice: the one that answers the question "given a tenant ID, create their private
network and return the credentials to reach the CF API."

### API surface

```
POST /api/v1/vpc/provision
{
  "tenant_id":    "acme-corp",
  "display_name": "Acme Corp",
  "plan":         "starter"
}

Response 202 Accepted:
{
  "job_id": "uuid",
  "status": "PROVISIONING"
}
```

```
GET /api/v1/vpc/jobs/{job_id}

Response (when complete):
{
  "status":    "READY",
  "tenant_id": "acme-corp",
  "api_key":   "cf_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "api_key_id": "uuid",
  "vpc_info": {
    "pod_cidr":      "10.100.1.0/24",
    "service_cidr":  "10.200.1.0/24",
    "vcluster_ready": true
  }
}
```

The API key is returned **exactly once** in the job response. It is never stored
in plaintext anywhere in the platform — only its BLAKE2b hash is persisted in
ScyllaDB. The tenant uses this key to authenticate all subsequent CF API calls.

---

## What the service orchestrates (the full provisioning sequence)

```
Step 1:  Validate request
         - tenant_id format (Kubernetes namespace naming rules)
         - tenant_id uniqueness in ScyllaDB (LWT: IF NOT EXISTS)
         - plan is a recognized value

Step 2:  Write tenant record to ScyllaDB (status: PROVISIONING)
         - cf.tenants table
         - Idempotent: IF NOT EXISTS LWT prevents duplicate records

Step 3:  Allocate CIDRs
         - Pod CIDR:     10.100.{n}.0/24  from supernet 10.100.0.0/16
         - Service CIDR: 10.200.{n}.0/24  from supernet 10.200.0.0/16
         - Tracked in cf.cidr_allocations with LWT (concurrent-safe)

Step 4:  Create host namespace on the Kubernetes cluster
         - kubectl create namespace tenant-{tenant-id}
         - Labels: cloudforge.io/tenant-id, cloudforge.io/tier=tenant

Step 5:  Apply Cilium network policies (primitives already built)
         a. internal/provisioner.TenantIsolationPolicy(namespace)
            → default-deny ingress; same-namespace only
         b. internal/provisioner.ProvisionerAccessPolicy(namespace)  ← new
            → allow cf-system → tenant-* port 6443 (vCluster API server)

Step 6:  Create vCluster (the topological boundary)
         vcluster create {tenant-id} \
           --namespace tenant-{tenant-id} \
           --pod-cidr 10.100.{n}.0/24 \
           --service-cidr 10.200.{n}.0/24 \
           --connect=false
         Wait for vCluster API server ready (up to 90s, validated in spike)

Step 7:  Store vCluster kubeconfig in OpenBao (already built)
         internal/provisioner.Store(ctx, client, tenantID, kubeconfigYAML)
         Path: secret/cf/tenants/{tenant-id}/kubeconfig

Step 8:  Generate API key
         - Generate 32 random bytes, prefix "cf_live_"
         - Compute BLAKE2b hash
         - Write hash + metadata to ScyllaDB cf.api_keys (LWT)
         - Raw key returned exactly once in the job response

Step 9:  Update tenant record to ACTIVE in ScyllaDB

Step 10: Return job status READY with api_key + vpc_info
```

---

## The one missing Cilium rule

The current CNP only enforces same-namespace default-deny. There is one explicit
allow rule that is currently absent and required for the provisioner to function
after isolation is applied:

```yaml
# Applied per-tenant at provisioning time (alongside TenantIsolationPolicy)
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: provisioner-access
  namespace: tenant-acme-corp          # set per tenant at provision time
spec:
  endpointSelector:
    matchLabels:
      app: vcluster                    # the vCluster API server pod
  ingress:
    - fromEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: cf-system
      toPorts:
        - ports:
            - port: "6443"
              protocol: TCP
```

Without this rule, `TenantIsolationPolicy` blocks the provisioner from reaching
the vCluster API server. This is a third function in `internal/provisioner/cnp.go`
alongside `TenantIsolationPolicy` and `PlatformIsolationPolicy`:

```go
// ProvisionerAccessPolicy renders a CiliumNetworkPolicy that allows the
// CF-Provisioner (running in cf-system) to reach the tenant's vCluster API
// server on port 6443. Must be applied alongside TenantIsolationPolicy,
// otherwise the provisioner cannot manage the tenant after isolation is
// enforced.
func ProvisionerAccessPolicy(tenantNamespace string) ([]byte, error)
```

---

## New files and packages needed

```
internal/provisioner/
├── cnp.go              ← EXISTS — extend with ProvisionerAccessPolicy()
├── kubeconfig.go       ← EXISTS — no changes needed
│
├── vcluster.go         ← NEW: Create, Wait, Delete vCluster
│                              Wraps vcluster CLI (exec) or Helm SDK
│
├── cidr.go             ← NEW: Allocate non-overlapping CIDRs
│                              Tracks used ranges in ScyllaDB with LWT
│
└── apikey.go           ← NEW: Generate key, compute BLAKE2b hash,
                               write to ScyllaDB cf.api_keys

internal/accounts/
├── doc.go              ← NEW package: ScyllaDB access for CF-Accounts data
├── tenant.go           ← NEW: Create, Get, UpdateStatus for tenant records
├── apikey.go           ← NEW: Store, Lookup, Revoke API key records
└── schema/
    └── schema.cql      ← NEW: CQL DDL (validated in scylladb-accounts spike)

cmd/cf-provisioner/
└── main.go             ← NEW: HTTP server, job queue, background worker
                               Minimum code — orchestrates internal packages
```

---

## The ScyllaDB schema (all patterns validated in the spike)

```cql
-- Tenant accounts
-- Primary key: tenant_id (UUID)
-- Hot path lookup: tenants_by_slug MV (p99 2.71ms QUORUM — validated)
CREATE TABLE cf.tenants (
    tenant_id    UUID PRIMARY KEY,
    slug         TEXT,
    display_name TEXT,
    status       TEXT,       -- PROVISIONING | ACTIVE | SUSPENDED | DELETED
    plan_id      TEXT,
    pod_cidr     TEXT,       -- "10.100.n.0/24"
    svc_cidr     TEXT,       -- "10.200.n.0/24"
    created_at   TIMESTAMP,
    updated_at   TIMESTAMP
);
CREATE MATERIALIZED VIEW cf.tenants_by_slug AS
    SELECT * FROM cf.tenants
    WHERE slug IS NOT NULL AND tenant_id IS NOT NULL
    PRIMARY KEY (slug, tenant_id);

-- API key records — hash-only storage (raw key never persisted)
-- Hot path: key_hash TEXT PRIMARY KEY → single-partition QUORUM read ~1ms
-- Validated in scylladb-accounts spike Benchmark 1
CREATE TABLE cf.api_keys (
    key_hash     TEXT PRIMARY KEY,   -- BLAKE2b(raw_key) — the lookup key
    key_id       UUID,
    tenant_id    UUID,
    created_by   UUID,
    scopes       TEXT,               -- "provision:write,provision:read"
    status       TEXT,               -- ACTIVE | ROTATING | REVOKED
    expires_at   TIMESTAMP,
    last_used_at TIMESTAMP,
    created_at   TIMESTAMP
);

-- CIDR allocation tracker (LWT-safe concurrent allocation)
-- LWT correctness validated in scylladb-accounts spike Benchmark 2
CREATE TABLE cf.cidr_allocations (
    cidr         TEXT PRIMARY KEY,   -- "10.100.1.0/24"
    tenant_id    UUID,
    cidr_type    TEXT,               -- "POD" | "SERVICE"
    allocated_at TIMESTAMP
);

-- Provisioning job log (for async 202 Accepted / GET job pattern)
CREATE TABLE cf.provisioning_jobs (
    job_id           UUID,
    tenant_id        UUID,
    idempotency_key  TEXT,
    operation        TEXT,   -- "PROVISION_VPC" | "DEPROVISION_VPC"
    status           TEXT,   -- QUEUED | PROVISIONING | READY | FAILED
    error_message    TEXT,
    result           TEXT,   -- JSON: api_key_id, vpc_info (NO raw key)
    started_at       TIMESTAMP,
    completed_at     TIMESTAMP,
    PRIMARY KEY (tenant_id, job_id)
) WITH CLUSTERING ORDER BY (job_id DESC);
```

---

## What the returned API key gives the tenant

The `cf_live_xxx` key returned in the completed job response is the tenant's
credential for all subsequent CF control plane API calls:

```bash
# Provision a private network
cf vpc provision --tenant acme-corp --plan starter
# Returns: cf_live_a1b2c3...   (store this — never shown again)

# All subsequent CF API calls use the key
cf services provision nats --name order-events
# Header: Authorization: Bearer cf_live_a1b2c3...
#
# CF-Router (future service):
#   1. BLAKE2b-hash the presented key
#   2. Lookup hash in cf.api_keys → {tenant_id, scopes, status}
#   3. Check: status==ACTIVE, not expired, scope includes "provision:write"
#   4. Forward to CF-Provisioner with X-CF-Tenant-ID: acme-corp
```

The key has default scope `provision:write,provision:read`. Narrower-scoped keys
can be generated later:

```bash
cf apikey create --scope provision:read --name "ci-readonly"
```

---

## What is NOT in scope for this service

| Out of scope | Reason |
|---|---|
| Provisioning services inside the vCluster (NATS, MinIO, PostgreSQL) | CF-Provisioner v2 (service handlers). VPC provisioning only creates the network. |
| CF-Router service | Separate service; adds tenant-aware routing on top of this key model. |
| Per-tenant Envoy/Contour gateway | Part of service provisioning, not network provisioning. |
| UI console | CLI-first is the correct implementation order. |
| Keycloak realm creation | Identity provisioning — separate concern from network provisioning. |
| Tenant environment migrator | Phase 3 (existing tenants → vCluster migration). |

---

## Deprovisioning (the reverse flow)

```
DELETE /api/v1/vpc/{tenant_id}

Step 1:  Revoke all API keys for the tenant (cf.api_keys → status: REVOKED)
Step 2:  Delete vCluster and its host namespace
         vcluster delete {tenant-id} --namespace tenant-{tenant-id}
         kubectl delete namespace tenant-{tenant-id}
Step 3:  Revoke kubeconfig from OpenBao (already built)
         internal/provisioner.Revoke(ctx, client, tenantID)
Step 4:  Release CIDR allocations (delete from cf.cidr_allocations)
Step 5:  Update tenant record to DELETED in ScyllaDB
```

Deprovisioning must be idempotent — each step is safe to call twice. `Revoke`
in `kubeconfig.go` is already idempotent (OpenBao DeleteMetadata returns 204
even for missing paths).

---

## Async job pattern

vCluster creation takes 2–90s (validated: p95 ~8.7s cold, ~2.5s warm). The
provision endpoint must not hold the HTTP connection open.

```
POST /api/v1/vpc/provision → 202 Accepted { job_id }
                                       ↓
                              background worker goroutine
                              executes steps 1–10
                                       ↓
GET /api/v1/vpc/jobs/{job_id} → { status: READY, api_key: "cf_live_..." }
```

Job state is persisted in `cf.provisioning_jobs` before the background goroutine
starts. If the service restarts mid-provisioning, the worker picks up `QUEUED`
jobs from ScyllaDB on startup. LWT ensures only one replica executes a given job
(validated in spike Benchmark 2: exactly 1 winner across 20 concurrent goroutines).

---

## Build order (recommended)

| # | Task | Depends on |
|---|------|-----------|
| 1 | `internal/accounts/` package + ScyllaDB schema | ScyllaDB in dev cluster |
| 2 | `internal/provisioner/cidr.go` | `internal/accounts/` |
| 3 | `internal/provisioner/vcluster.go` | vcluster CLI installed |
| 4 | `internal/provisioner/apikey.go` | `internal/accounts/` |
| 5 | Extend `internal/provisioner/cnp.go` with `ProvisionerAccessPolicy` | existing cnp.go |
| 6 | `cmd/cf-provisioner/main.go` + workflow | all of the above |
| 7 | `make` targets: `provision-vpc`, `deprovision-vpc`, `vpc-status` | cmd binary |
| 8 | Integration tests (≥90% coverage, testcontainers) | all of the above |

---

## Summary: what needs to be built

| # | Package / file | New or extend | Validated by |
|---|----------------|--------------|--------------|
| 1 | `internal/provisioner/vcluster.go` | New | tenant-isolation spike |
| 2 | `internal/provisioner/cidr.go` | New | scylladb-accounts spike (LWT) |
| 3 | `internal/provisioner/apikey.go` | New | scylladb-accounts spike |
| 4 | `internal/accounts/tenant.go` | New | scylladb-accounts spike |
| 5 | `internal/accounts/apikey.go` | New | scylladb-accounts spike |
| 6 | `internal/provisioner/cnp.go` | Extend | cilium-enforcement spike |
| 7 | `cmd/cf-provisioner/main.go` | New | — |
| 8 | ScyllaDB schema (`cf.tenants`, `cf.api_keys`, `cf.cidr_allocations`) | New (CQL) | scylladb-accounts spike |
| 9 | Cilium CNP update (`ProvisionerAccessPolicy`) | Extend | cilium-enforcement spike |

All validation is done. All infrastructure is running in `make dev-up`. The
remaining work is implementation, not exploration.

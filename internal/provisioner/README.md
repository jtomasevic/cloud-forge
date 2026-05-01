# `internal/provisioner`

Building blocks for the CloudForge tenant provisioner service (CF-Provisioner).

This package contains the first two operations executed when a new tenant is onboarded:

1. **CNP rendering** (`cnp.go`) — render the `CiliumNetworkPolicy` YAML that enforces default-deny isolation for the tenant namespace.
2. **Kubeconfig storage** (`kubeconfig.go`) — store, retrieve, and revoke the tenant's vCluster kubeconfig in OpenBao.

Both are prerequisites for everything else the provisioner does. A namespace without a CNP is unprotected. A kubeconfig that isn't in OpenBao means no subsequent provisioning job can reach that tenant's API server.

---

## How it fits into the architecture

```
New tenant request arrives
          │
          ▼
CF-Provisioner creates vCluster
(vCluster CLI / Helm)
          │
          ▼
┌─────────────────────────────────────────────────────────────────┐
│ internal/provisioner (this package)                             │
│                                                                  │
│  ① TenantIsolationPolicy(namespace)                             │
│     → renders CiliumNetworkPolicy YAML                          │
│     → kubectl apply → Cilium agent picks up CNP in ~ms          │
│     → eBPF dataplane enforces deny-by-default                   │
│                                                                  │
│  ② Store(ctx, client, tenantID, kubeconfigYAML)                 │
│     → writes to OpenBao at:                                     │
│       secret/cf/tenants/{tenant-id}/kubeconfig                  │
│     → all provisioner replicas can now reach this tenant        │
└─────────────────────────────────────────────────────────────────┘
          │
          ▼
Provisioner calls Retrieve(ctx, client, tenantID)
at the start of each subsequent provisioning job
          │
          ▼
Uses kubeconfig to connect to tenant's vCluster API server
(apply NATS manifests, MinIO config, database init, etc.)
          │
          ▼
Tenant deprovisioned → Revoke(ctx, client, tenantID)
(hard delete from OpenBao — all versions removed)
```

---

## OpenBao path structure

```
OpenBao (KV v2 engine, mount: secret/)
│
└── cf/
    └── tenants/
        ├── acme-corp/
        │   └── kubeconfig          ← vCluster kubeconfig YAML
        ├── beta-startup/
        │   └── kubeconfig
        └── gamma-inc/
            └── kubeconfig
```

### Why KV v2 (not KV v1)?

| Feature | KV v1 | KV v2 |
|---------|-------|-------|
| Versioning | None | Last N versions retained |
| Soft delete | None | Mark as deleted, recoverable |
| Metadata endpoint | None | Yes (separate path) |
| Rotation without downtime | Not safe | Safe (new version written before old is used) |

KV v2 enables the **zero-downtime rotation pattern**:

```
1. Store(ctx, client, tenantID, newKubeconfig)  → creates version N+1
2. Verify connectivity with newKubeconfig
3. Revoke old vCluster service account (inside vCluster)
```

At no point is there a window where the kubeconfig is missing.

### Policy scoping (production model)

In production, the provisioner pod authenticates to OpenBao via Kubernetes auth and receives a short-lived token scoped to a **per-tenant policy**. The policy for tenant `acme-corp`:

```hcl
path "secret/data/cf/tenants/acme-corp/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "secret/metadata/cf/tenants/acme-corp/*" {
  capabilities = ["read", "delete", "list"]
}
```

A token with this policy **cannot** read `secret/data/cf/tenants/beta-startup/*`. This is verified by `TestCrossTenant_PolicyIsolation` in `kubeconfig_integration_test.go`.

---

## API

### CNP rendering

```go
// Render the CiliumNetworkPolicy YAML for a tenant namespace.
yaml, err := provisioner.TenantIsolationPolicy("acme-corp")

// Render for the platform namespace (cf-system).
yaml, err := provisioner.PlatformIsolationPolicy("cf-system")
```

Both return `[]byte` ready to pass to `kubectl apply` or the Kubernetes client.

### Kubeconfig storage

```go
// Store a tenant's vCluster kubeconfig in OpenBao.
// Call this immediately after the vCluster API server becomes ready.
err := provisioner.Store(ctx, client, "acme-corp", kubeconfigYAML)

// Retrieve the kubeconfig at the start of each provisioning job.
kc, err := provisioner.Retrieve(ctx, client, "acme-corp")
if errors.Is(err, provisioner.ErrNotFound) {
    // tenant was never provisioned, or was already deprovisioned
}

// Permanently delete all versions during tenant deprovisioning.
// Idempotent: safe to call even if the tenant was never stored.
err = provisioner.Revoke(ctx, client, "acme-corp")
```

---

## Files

| File | Purpose |
|------|---------|
| `doc.go` | Package-level documentation |
| `cnp.go` | `TenantIsolationPolicy`, `PlatformIsolationPolicy`, namespace validation |
| `cnp_test.go` | Unit tests for CNP rendering (no Docker required) |
| `kubeconfig.go` | `Store`, `Retrieve`, `Revoke`, `ErrNotFound`, path helpers |
| `kubeconfig_test.go` | Unit tests — validation paths only (no Docker required) |
| `kubeconfig_whitebox_test.go` | Whitebox unit tests for `isNotFound` and `kvPath` (no Docker required) |
| `kubeconfig_integration_test.go` | Integration tests — individual functions against real OpenBao container |
| `lifecycle_integration_test.go` | End-to-end scenario tests — full provisioner workflows (see below) |

---

## Running tests

### Prerequisites

| Requirement | Why | How to verify |
|-------------|-----|---------------|
| **Docker running** | Integration + lifecycle tests start an OpenBao container via testcontainers-go | `docker info` |
| **No Kubernetes cluster needed** | CNP rendering is pure Go; no `kubectl apply` is performed | — |
| **No Cilium needed** | CNPs are validated structurally, not applied | — |
| **No vCluster needed** | Kubeconfigs stored in tests are static YAML fixtures | — |
| First run: ~50 MB Docker pull | `quay.io/openbao/openbao:latest` image | Cached after first run |

### Commands

```bash
# Unit tests only — fast, no Docker (~2s)
make provisioner-test

# Integration tests (functions) + lifecycle scenarios — requires Docker (~15-20s)
make provisioner-test-integration

# Run only the end-to-end lifecycle scenarios
go test -tags=integration -v -run "TestLifecycle" ./internal/provisioner/...

# Unit test coverage report (opens provisioner-coverage.html)
make provisioner-coverage

# Full coverage: unit + integration + lifecycle (authoritative ≥90% target)
make provisioner-coverage-integration
```

### End-to-end lifecycle scenarios (`lifecycle_integration_test.go`)

| Test | Scenario | What it proves |
|------|----------|---------------|
| `TestLifecycle_FullTenantOnboardingAndDeprovisioning` | CNP render → Store → Retrieve → Revoke → ErrNotFound | Complete tenant lifecycle works end-to-end |
| `TestLifecycle_PlatformNamespaceSetup` | `PlatformIsolationPolicy` vs `TenantIsolationPolicy` | Platform and tenant CNPs are distinct; no kubeconfig for host namespaces |
| `TestLifecycle_ConcurrentTenantProvisioning` | 5 tenants provisioned in parallel goroutines | No data races, no cross-contamination under concurrency |
| `TestLifecycle_ZeroDowntimeKubeconfigRotation` | Store v1 → Store v2 → Store v3 | Retrieve always returns latest; old versions are not surfaced |
| `TestLifecycle_DeprovisioningAfterPartialFailure` | Revoke (never stored) → Revoke again → retry Store → Retrieve | Cleanup is idempotent; retry after failure succeeds |

---

## Using OpenBao in the dev cluster

`make dev-up` deploys OpenBao in **dev mode** to the `cf-security` namespace. This gives every developer a real OpenBao API endpoint that mirrors the production service address, without requiring a production-grade setup.

### What dev mode gives you

| Feature | Dev cluster | Production |
|---------|-------------|------------|
| Real OpenBao API | ✅ Same URL | ✅ |
| KV v2 path structure | ✅ Identical | ✅ |
| Cilium CNP (cf-system → port 8200 only) | ✅ Enforced | ✅ |
| Persistent storage | ❌ In-memory (lost on restart) | ✅ Raft / PostgreSQL |
| Auth method | ❌ Root token | ✅ Kubernetes auth (ServiceAccount JWT) |
| TLS | ❌ Plain HTTP | ✅ mTLS |
| High availability | ❌ Single replica | ✅ 3-node HA |

### Accessing OpenBao from your terminal

```bash
# Forward OpenBao API to localhost (keep this terminal open)
make openbao-port-forward

# In another terminal — set env vars and use the bao/vault CLI:
export VAULT_ADDR=http://localhost:8200
export VAULT_TOKEN=dev-root-token

# Write a test kubeconfig:
vault kv put secret/cf/tenants/acme-corp/kubeconfig \
  kubeconfig="$(cat ~/.kube/config)"

# Read it back:
vault kv get secret/cf/tenants/acme-corp/kubeconfig

# Or use the Go provisioner package directly in a test binary.
```

### Pointing the provisioner at the cluster OpenBao

When you run the provisioner service locally (outside the cluster), point it at the port-forwarded address:

```bash
export OPENBAO_ADDR=http://localhost:8200
export OPENBAO_TOKEN=dev-root-token
go run ./cmd/cf-provisioner
```

When the provisioner runs inside the cluster (as a Kubernetes pod in `cf-system`), it uses the in-cluster DNS address — the same URL as production:

```
http://openbao.cf-security.svc.cluster.local:8200
```

### What is NOT tested against the cluster OpenBao

Integration tests (`make provisioner-test-integration`) still use `StartOpenBao(t)` testcontainers. This is intentional:
- Tests are isolated, repeatable, and do not depend on a running cluster
- The testcontainer root token matches `internal/testutil/openbao.go:openBaoRootToken`
- CI does not require a running k3d cluster

The cluster OpenBao is for developer exploration, manual provisioner testing, and eventually the real provisioner service.

---

## Design decisions and observations

### 1. Tenant ID = Kubernetes namespace name

Tenant IDs follow Kubernetes namespace naming rules (RFC 1123 DNS label: lowercase alphanumeric and hyphens). This is enforced by `validateTenantID`, which delegates to the same `validateNamespace` function used by CNP rendering. The same string is used as:

- The Kubernetes host namespace name (`tenant-{id}`)
- The OpenBao KV path segment (`cf/tenants/{id}/kubeconfig`)
- The `CiliumNetworkPolicy` namespace field

Keeping these identical eliminates a class of bugs where the provisioner applies a CNP to the wrong namespace or looks up the wrong secret path.

### 2. Hard delete vs soft delete on Revoke

OpenBao KV v2 has two delete operations:
- `Delete` — soft-deletes the latest version; metadata and older versions remain
- `DeleteMetadata` — deletes all versions and the metadata key entirely

`Revoke` uses `DeleteMetadata` (hard delete). After a tenant is deprovisioned, there must be no recovery path for their credentials. Using soft delete would leave data recoverable by anyone with metadata-read access, which contradicts the access revocation guarantee.

### 3. Why the provisioner does not store a client per tenant

The provisioner holds **one** OpenBao client authenticating as the platform provisioner service account. It uses path-based access — the service account's policy covers `cf/tenants/*/kubeconfig`, allowing it to manage all tenants. Individual tenants never receive an OpenBao token; their kubeconfigs are only used server-side by the provisioner itself.

This differs from the **per-tenant token** shown in `TestCrossTenant_PolicyIsolation`, which tests OpenBao's access control model in isolation. In production, per-tenant tokens would be used if the provisioner were split into per-tenant worker processes — an option for future horizontal scaling.

### 4. What "Retrieve returns ErrNotFound" means in practice

`ErrNotFound` means one of:
- The tenant was never provisioned (Store was never called)
- `Revoke` was called (deprovisioning complete)
- The OpenBao path was manually deleted

It does **not** mean the vCluster is gone. The provisioner must treat `ErrNotFound` as a hard failure for any job that requires reaching the tenant's API server, and report it to the operator rather than silently skipping work.

### 5. Coverage strategy

Unit tests cover all validation error paths (empty ID, invalid characters, empty kubeconfig YAML). These exercise every `return err` in `Store`, `Retrieve`, and `Revoke` that fires before the OpenBao client is called — using a `nil` client, which is safe because validation returns before any method call.

Integration tests cover all post-validation paths (successful write, read, delete, not-found, overwrite, cross-tenant policy isolation). Together they reach the ≥90% target.

---

## Validation status

| Claim | How validated |
|-------|---------------|
| CNP renders correctly for any valid namespace | `cnp_test.go` — 10 unit tests |
| Store → Retrieve is byte-for-byte faithful | `TestStore_And_Retrieve_Roundtrip` |
| Retrieve returns ErrNotFound for new/revoked tenants | `TestRetrieve_ReturnsErrNotFound_*` |
| Revoke hard-deletes all versions | `TestRevoke_*` |
| Multiple tenants stored independently | `TestStore_MultiTenant_Isolation` |
| Scoped token cannot cross tenant boundary | `TestCrossTenant_PolicyIsolation` |
| Invalid tenant IDs rejected before touching OpenBao | `kubeconfig_test.go` — 6 unit tests |

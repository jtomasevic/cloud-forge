# cmd/cf-provisioner

The `cf-provisioner` binary is the CloudForge VPC provisioning service.

It accepts HTTP requests to provision tenant private networks and orchestrates
the 10-step workflow described in `docs/CF-VPC-Service-Proposal.md`.

---

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/vpc/provision` | Provision a new tenant VPC (async, returns job_id) |
| `GET` | `/api/v1/vpc/jobs/{id}` | Poll a provisioning job for status and result |
| `DELETE` | `/api/v1/vpc/{tenant_id}` | Deprovision a tenant VPC (async) |

### Example: provision a tenant

```bash
# Start provisioning
curl -X POST http://localhost:8080/api/v1/vpc/provision \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"acme-corp","display_name":"Acme Corp","plan":"starter"}'

# Response: 202 Accepted
# { "job_id": "550e8400-e29b-41d4-a716-446655440000", "status": "QUEUED" }

# Poll until READY
curl http://localhost:8080/api/v1/vpc/jobs/550e8400-e29b-41d4-a716-446655440000

# Response when complete:
# {
#   "job_id": "550e8400-e29b-41d4-a716-446655440000",
#   "status": "READY",
#   "api_key": "cf_live_a1b2c3...",   ← store this, never shown again
#   "api_key_id": "uuid",
#   "vpc_info": {
#     "pod_cidr": "10.100.1.0/24",
#     "service_cidr": "10.200.1.0/24",
#     "vcluster_ready": true
#   }
# }
```

```bash
# Makefile shortcuts (requires service running at localhost:8080)
make vpc-provision TENANT=acme-corp DISPLAY="Acme Corp" PLAN=starter
make vpc-status JOB_ID=550e8400-e29b-41d4-a716-446655440000
make vpc-deprovision TENANT=acme-corp
```

## Provisioning sequence (10 steps)

```
Step 1:  Validate request (tenant_id format, plan, display_name)
Step 2:  Create tenant record in ScyllaDB (status: PROVISIONING, LWT IF NOT EXISTS)
Step 3:  Allocate pod CIDR (10.100.n.0/24) and service CIDR (10.200.n.0/24) with LWT
Step 4:  Create host Kubernetes namespace (tenant-{tenant-id})
Step 5:  Apply Cilium network policies:
           - TenantIsolationPolicy (default-deny ingress, same-namespace only)
           - ProvisionerAccessPolicy (allow cf-system → vCluster port 6443)
Step 6:  Create vCluster in the host namespace (–pod-cidr, –service-cidr)
         Wait for API server ready (up to 90s)
Step 7:  Export vCluster kubeconfig and store in OpenBao
         (secret/cf/tenants/{tenant-id}/kubeconfig)
Step 8:  Generate API key (cf_live_xxx), compute BLAKE2b-256 hash,
         store hash in ScyllaDB cf.api_keys
Step 9:  Update tenant record to ACTIVE, write CIDRs
Step 10: Mark job READY, store result JSON (api_key in result for one-time retrieval)
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `SCYLLA_HOSTS` | `127.0.0.1` | Comma-separated ScyllaDB contact points |
| `SCYLLA_PORT` | `19042` | CQL port (dev: kubectl port-forwarded) |
| `SCYLLA_USER` | — | ScyllaDB username (optional) |
| `SCYLLA_PASS` | — | ScyllaDB password (optional) |
| `OPENBAO_ADDR` | `http://localhost:8200` | OpenBao API address |
| `OPENBAO_TOKEN` | `dev-root-token` | Root or provisioner token |
| `LISTEN_ADDR` | `:8080` | HTTP bind address |

## Local dev quick-start

```bash
# 1. Start the cluster with all dependencies
make dev-up

# 2. Set up port-forwards (background processes)
make scylladb-port-forward &   # CQL on localhost:19042
make openbao-port-forward &    # Vault API on localhost:8200

# 3. Apply CF schema (first time only)
make scylladb-apply-schema

# 4. Build and run
make provisioner-run
# → listening on http://localhost:8080

# 5. Provision a tenant
make vpc-provision TENANT=acme-corp DISPLAY="Acme Corp" PLAN=starter
```

## Package structure

The binary contains **minimum code** — it only wires up configuration, connects
to dependencies, and orchestrates internal packages.

| Location | Responsibility |
|----------|---------------|
| `internal/accounts/` | ScyllaDB data access (tenants, API keys, jobs) |
| `internal/provisioner/cnp.go` | Cilium policy rendering |
| `internal/provisioner/cidr.go` | CIDR allocation with LWT |
| `internal/provisioner/vcluster.go` | vCluster lifecycle (create, wait, delete) |
| `internal/provisioner/apikey.go` | API key generation + BLAKE2b hashing |
| `internal/provisioner/kubeconfig.go` | OpenBao Store/Retrieve/Revoke |
| `cmd/cf-provisioner/main.go` | HTTP server, request handlers, workflow orchestration |

## Deprovisioning

```
DELETE /api/v1/vpc/{tenant_id}

Step 1: Revoke kubeconfig from OpenBao (idempotent)
Step 2: Delete vCluster + host namespace (idempotent)
Step 3: Mark tenant DELETED in ScyllaDB
```

All steps are idempotent — safe to retry after a partial failure.

## Job persistence and crash recovery

Job state is written to `cf.provisioning_jobs` *before* the background goroutine
starts. If the service restarts mid-provisioning, a startup worker can pick up
`QUEUED` jobs and resume from the last completed step. The LWT `IF status = 'QUEUED'`
claim prevents multiple replicas from executing the same job.

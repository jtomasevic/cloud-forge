# internal/accounts

The `accounts` package is the CloudForge control plane's data access layer,
backed by ScyllaDB CQL.

---

## What it owns

| Entity | Table | Used by |
|--------|-------|---------|
| Tenant records | `cf.tenants` + MV `cf.tenants_by_slug` | CF-Provisioner (create/update), CF-Router (slug resolution) |
| API keys (hash-only) | `cf.api_keys` | CF-Provisioner (create), CF-Router (hot-path lookup) |
| Provisioning jobs | `cf.provisioning_jobs` + `cf.provisioning_jobs_by_idem` | CF-Provisioner (async job queue) |

## Schema

The CQL DDL lives in `schema/schema.cql`. Apply it once at service startup:

```bash
# Via cqlsh (after make scylladb-port-forward)
make scylladb-apply-schema

# Or programmatically at service startup
accounts.ApplySchema(sess, schemaContent)
```

## Connecting

```go
cfg := accounts.DefaultConfig()                  // dev defaults: 127.0.0.1:19042
cfg.Username = os.Getenv("SCYLLA_USER")          // optional
cfg.Password = os.Getenv("SCYLLA_PASS")
sess, err := accounts.NewSession(cfg)
defer sess.Close()
```

## Key design decisions

### LWT for concurrency safety

All state-changing writes use Lightweight Transactions (IF NOT EXISTS, IF status = '...'):

```go
// Create tenant — IF NOT EXISTS prevents duplicate slugs
tenant, err := ts.Create(ctx, "acme-corp", "Acme Corp", "starter")

// Claim job — IF status = 'QUEUED' prevents duplicate execution
claimed, err := js.Claim(ctx, tenantID, jobID)
```

Validated by: `spikes/scylladb-accounts/` Benchmark 2 (exactly 1 winner across 20
concurrent goroutines).

### API key hash-only storage

The raw API key (`cf_live_xxxxxxx`) is **never stored anywhere**. Only the
BLAKE2b-256 hex digest (`key_hash`) is persisted. CF-Router hashes the
presented bearer token and does a single-partition QUORUM lookup:

```
Bearer: cf_live_abc123 → BLAKE2b-256 → hex → SELECT FROM cf.api_keys WHERE key_hash = ?
```

Lookup p99 ~1ms QUORUM (validated in `spikes/scylladb-accounts/` Benchmark 1).

### Materialized view for slug resolution

CF-Router translates a tenant slug to its UUID via the `cf.tenants_by_slug` MV:

```go
tenant, err := ts.GetBySlug(ctx, "acme-corp")
// Uses MV: p99 ~2.71ms QUORUM (scylladb-accounts spike Benchmark 3)
```

## Running tests

```bash
# Unit tests (no Docker required) — covers constants, helpers, validation
go test ./internal/accounts/...

# Integration tests (requires Docker) — covers all DB operations
go test -tags integration -timeout 300s ./internal/accounts/...

# With coverage
make accounts-coverage-integration
```

## Package structure

| File | Contents |
|------|----------|
| `doc.go` | Package documentation |
| `session.go` | `Config`, `NewSession`, `ApplySchema`, `splitStatements` |
| `tenant.go` | `TenantStore`: Create, Get, GetBySlug, UpdateStatus, SetCIDRs |
| `apikey.go` | `APIKeyStore`: Store, Lookup, RevokeByHash, RevokeByID |
| `job.go` | `JobStore`: Enqueue, Claim, Complete, Fail, Get |
| `schema/schema.cql` | Full CQL DDL (keyspace + all tables + MVs) |

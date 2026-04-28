# Spike: ScyllaDB as Control Plane Account Store

**Reference:** `docs/3-Introduce-CF-VPC.md` §6.3, §10.2, §10.5  
**Depends on:** Task 0.7 — ScyllaDB deployed in k3d dev cluster  
**Category:** Backend, Platform

---

## Purpose

This spike validates that **ScyllaDB with native CQL** is the right datastore for
**CF-Accounts** — the Cloud Forge service that owns tenant records, user accounts,
API keys, and provisioned service inventory.

Three concrete questions are answered:

| # | Question | Target |
|---|----------|--------|
| 1 | Can CF-Router look up an API key hash at < 2 ms p99 under 50 concurrent requests? | p99 < 2 ms |
| 2 | Do LWT-based state transitions prevent duplicate provisioning jobs under concurrent load? | exactly 1 winner per race |
| 3 | Do materialized-view lookups (tenant slug, user email) complete at < 5 ms p99? | p99 < 5 ms |

---

## Architecture context

Every inbound API request goes through **CF-Router**, which performs:

```
1. Strip "cf_live_" prefix from the Authorization header
2. BLAKE2b-256 hash the raw key bytes → hex string (< 0.1 ms, pure CPU)
3. SELECT * FROM cf.api_keys WHERE key_hash = ?   (target: ~1 ms QUORUM)
4. Validate status=ACTIVE, expiry not passed, scope includes the required right
5. Forward request to the backend service with injected X-CF-Tenant-ID header
```

The ScyllaDB read in step 3 is **the only stateful operation** on the hot path.
This spike measures its latency and validates it fits within the 2 ms budget.

**CF-Provisioner** creates provisioning jobs using Lightweight Transactions:

```cql
-- Idempotent job creation (IF NOT EXISTS):
INSERT INTO cf.provisioning_jobs_by_idem (tenant_id, idempotency_key, job_id)
VALUES (?, ?, ?) IF NOT EXISTS;

-- State-machine transition (only one worker can claim a job):
UPDATE cf.provisioning_jobs
  SET status = 'PROVISIONING', started_at = ?
  WHERE tenant_id = ? AND job_id = ?
  IF status = 'QUEUED';
```

---

## Schema

The full CQL schema is in [`schema/schema.cql`](schema/schema.cql). It creates:

| Table | Purpose |
|-------|---------|
| `cf.tenants` | Tenant accounts (status, plan, CIDR allocation) |
| `cf.tenants_by_slug` | **MV** — resolves JWT slug → tenant_id |
| `cf.users` | User records (Argon2id password hash, role, status) |
| `cf.users_by_email` | **MV** — resolves email → user record (login flow) |
| `cf.api_keys` | API key hashes — CF-Router hot path table |
| `cf.service_instances` | Provisioned service inventory per tenant |
| `cf.provisioning_jobs` | Job log + distributed worker queue (LWT transitions) |
| `cf.provisioning_jobs_by_idem` | Idempotency dedup table (LWT IF NOT EXISTS) |

The `cf.api_keys` table uses `key_hash TEXT PRIMARY KEY` so every lookup targets
a single shard and requires no scatter-gather.

---

## Prerequisites

- k3d dev cluster running (`make dev-up` from repo root)
- ScyllaDB deployed (`make deploy-scylladb` from repo root)  
- Port 9042 forwarded to `127.0.0.1:9042` (configured in `deploy/k3d/cluster.yaml`)
- Go 1.22+

Verify:
```bash
kubectl get pods -n scylla
cqlsh 127.0.0.1 9042 -e "SHOW VERSION"
```

---

## Running the spike

### Full benchmark run (seed + all three benchmarks)

```bash
cd spikes/scylladb-accounts
make run
```

This will:
1. Connect to ScyllaDB at `127.0.0.1:9042`
2. Drop and re-create the `cf` keyspace with the full schema
3. Wait for materialized views to be ready (up to 30 s)
4. Seed 1 000 API keys, 1 000 tenants, 1 000 users
5. Run all three benchmark suites
6. Print a results table to stdout

### Custom workload size

```bash
# 10 000 seeded rows, 5 000 ops per benchmark, 100 concurrent goroutines
make run SEED=10000 OPS=5000 CONC=100
```

### Re-run benchmarks without re-seeding

```bash
make measure          # uses existing cf keyspace data
make measure OPS=5000 CONC=100
```

### Clean up

```bash
make destroy          # drops the cf keyspace
```

---

## Commands reference

| Command | Description |
|---------|-------------|
| `make run` | Seed + full benchmark suite (drops cf keyspace first) |
| `make measure` | Re-run benchmarks on existing data |
| `make destroy` | Drop cf keyspace |
| `make build` | Compile `bin/bench` |
| `make test` | Unit tests (no ScyllaDB needed) |
| `make test-coverage` | Unit tests + HTML coverage report |
| `make test-integration` | Integration tests (requires ScyllaDB) |
| `make apply-schema` | Apply schema.cql via cqlsh |
| `make describe-schema` | Describe the cf keyspace |

---

## Expected terminal output

```
── Benchmark 1: API key lookup (routing hot path) ──────────────────────────
   2000 ops  ·  50 goroutines  ·  1000 seeded keys

┌──────────────────────────────────────┬──────┬─────────┬─────────┬─────────┬────────────────┬──────┬──────────┐
│ Benchmark                            │  Ops │     p50 │     p95 │     p99 │     Throughput │  Err │  Verdict │
├──────────────────────────────────────┼──────┼─────────┼─────────┼─────────┼────────────────┼──────┼──────────┤
│ api_key_lookup (QUORUM)              │ 2000 │  0.85ms │  1.20ms │  1.48ms │    3500 ops/s  │    0 │     PASS │
│ api_key_lookup (ONE)                 │ 2000 │  0.60ms │  0.90ms │  1.10ms │    4800 ops/s  │    0 │     PASS │
└──────────────────────────────────────┴──────┴─────────┴─────────┴─────────┴────────────────┴──────┴──────────┘

── Benchmark 2: LWT idempotency (provisioning job state machine) ──────────
   20 concurrent writers racing on the same idempotency key

  lwt_job_claim                         p50=2.10ms   p99=4.80ms   winners=1    losers=19    correct=YES ✓
  lwt_state_transition                  p50=2.30ms   p99=5.10ms   winners=1    losers=19    correct=YES ✓

── Benchmark 3: Materialized view queries ───────────────────────────────────
   2000 ops  ·  50 goroutines  ·  1000 tenants / 1000 users

┌──────────────────────────────────────┬──────┬─────────┬─────────┬─────────┬────────────────┬──────┬──────────┐
│ Benchmark                            │  Ops │     p50 │     p95 │     p99 │     Throughput │  Err │  Verdict │
├──────────────────────────────────────┼──────┼─────────┼─────────┼─────────┼────────────────┼──────┼──────────┤
│ mv_tenant_by_slug (QUORUM)           │ 2000 │  0.90ms │  1.50ms │  2.10ms │    3200 ops/s  │    0 │     PASS │
│ mv_user_by_email (QUORUM)            │ 2000 │  0.95ms │  1.60ms │  2.20ms │    3100 ops/s  │    0 │     PASS │
└──────────────────────────────────────┴──────┴─────────┴─────────┴─────────┴────────────────┴──────┴──────────┘

── Summary ──────────────────────────────────────────────────────────────────
  Overall: PASS — ScyllaDB meets all CF-Accounts requirements.
```

---

## Code structure

```
spikes/scylladb-accounts/
├── schema/
│   └── schema.cql              Full CQL DDL: keyspace, tables, MVs
├── cmd/bench/
│   └── main.go                 CLI entry point (flags, orchestration)
├── internal/bench/
│   ├── doc.go                  Package-level documentation
│   ├── types.go                Config, Result, LWTResult, Samples
│   ├── stats.go                Percentile, Min, Max, BuildResult
│   ├── setup.go                NewSession, ApplySchema, DropSchema, WaitForMVReady
│   ├── seed.go                 SeedAPIKeys, SeedTenants, SeedUsers, HashAPIKey
│   ├── apikey.go               BenchAPIKeyLookup (QUORUM and ONE)
│   ├── lwt.go                  BenchLWTJobClaim, BenchLWTStateTransition
│   ├── mv.go                   BenchMVSlugLookup, BenchMVEmailLookup
│   ├── table.go                PrintResults, PrintLWTResult terminal formatter
│   ├── stats_test.go           Unit tests: percentiles, BuildResult, Config
│   ├── table_test.go           Unit tests: table formatter, PASS/FAIL verdicts
│   ├── seed_test.go            Unit tests: HashAPIKey, splitStatements, RandomRawKey
│   └── integration_test.go     Integration tests (skipped without ScyllaDB)
├── Makefile
├── go.mod
└── FINDINGS.md                 Benchmark results and architectural decisions
```

---

## How the BLAKE2b hash lookup works

```
CF-Router hot path (every request):

raw_key = strip_prefix(Authorization, "cf_live_")    // ~1 µs
hash    = hex(BLAKE2b-256(raw_key))                  // ~40 µs

SELECT key_id, tenant_id, status, scopes
  FROM cf.api_keys
 WHERE key_hash = hash;                              // ~1 ms QUORUM
```

The hash is the **sole partition key**. ScyllaDB's consistent hashing routes
this read to exactly one shard on exactly one node — no coordination, no scatter.

QUORUM consistency (2/3 nodes for RF=3) ensures the read reflects the most recent
write (e.g., a revocation) within one round-trip, making it safe to skip caching.

---

## Findings

See [`FINDINGS.md`](FINDINGS.md) for benchmark results, architectural decisions,
and the go/no-go recommendation.

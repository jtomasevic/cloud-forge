-- ============================================================
-- CF-Accounts: ScyllaDB Schema
-- ============================================================
--
-- Keyspace: SimpleStrategy RF=1 for local dev.
-- Production: NetworkTopologyStrategy RF=3, QUORUM consistency.
--
-- Validated by: spikes/scylladb-accounts/ (Benchmarks 1–3).
-- Reference:    docs/3-Introduce-CF-VPC.md §6.3
-- Reference:    docs/CF-VPC-Service-Proposal.md
-- ============================================================

CREATE KEYSPACE IF NOT EXISTS cf
  WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}
  AND durable_writes = true;

-- ──────────────────────────────────────────────────────────────────────────────
-- Tenant accounts
--
-- Primary lookup: by tenant_id (UUID).
-- Hot path: cf.tenants_by_slug MV resolves a slug (from JWT sub / CLI flag)
--           to a tenant_id before any service call.
--           p99 ~2.71ms QUORUM (scylladb-accounts spike Benchmark 3).
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.tenants (
    tenant_id     UUID,
    slug          TEXT,
    display_name  TEXT,
    status        TEXT,       -- PROVISIONING | ACTIVE | SUSPENDED | DELETED
    plan_id       TEXT,
    pod_cidr      TEXT,       -- vCluster pod CIDR, e.g. "10.100.1.0/24"
    svc_cidr      TEXT,       -- vCluster service CIDR, e.g. "10.200.1.0/24"
    created_at    TIMESTAMP,
    updated_at    TIMESTAMP,
    PRIMARY KEY (tenant_id)
);

CREATE MATERIALIZED VIEW IF NOT EXISTS cf.tenants_by_slug AS
    SELECT * FROM cf.tenants
    WHERE slug IS NOT NULL AND tenant_id IS NOT NULL
    PRIMARY KEY (slug, tenant_id);

-- ──────────────────────────────────────────────────────────────────────────────
-- API keys — routing hot path table
--
-- CF-Router performs one read on every inbound API request:
--   1. Strip the "cf_live_" prefix from the Authorization header.
--   2. Compute BLAKE2b-256 over the raw key bytes → hex string.
--   3. SELECT * FROM cf.api_keys WHERE key_hash = ?
--
-- The key_hash is the sole partition key so the read always targets a single
-- shard and completes in ~1ms with QUORUM consistency.
-- p99 ~2.0ms in-cluster (scylladb-accounts spike Benchmark 1).
--
-- The raw key is NEVER stored. At creation the provisioner returns it exactly
-- once; after that only the hash exists.
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.api_keys (
    key_hash      TEXT,       -- hex(BLAKE2b-256(raw_key)) — lookup key
    key_id        UUID,       -- stable identifier for management operations
    tenant_id     UUID,
    user_id       UUID,       -- which user created / owns this key
    display_name  TEXT,
    scopes        TEXT,       -- comma-separated: "provision:write,provision:read"
    status        TEXT,       -- ACTIVE | ROTATING | REVOKED
    expiry        TIMESTAMP,  -- NULL = never expires
    last_used_at  TIMESTAMP,
    created_at    TIMESTAMP,
    PRIMARY KEY (key_hash)
);

-- ──────────────────────────────────────────────────────────────────────────────
-- CIDR allocation tracker
--
-- CF-Provisioner reserves a /24 pod CIDR and a /24 service CIDR per tenant
-- from the platform supernets (10.100.0.0/16 and 10.200.0.0/16).
--
-- Concurrent allocation is protected by LWT:
--   INSERT INTO cf.cidr_allocations ... IF NOT EXISTS
-- Only one provisioner replica wins the allocation for a given CIDR prefix.
-- Validated by: scylladb-accounts spike Benchmark 2 (LWT correctness).
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.cidr_allocations (
    cidr          TEXT,       -- "10.100.1.0/24"
    tenant_id     UUID,
    cidr_type     TEXT,       -- "POD" | "SERVICE"
    allocated_at  TIMESTAMP,
    PRIMARY KEY (cidr)
);

-- ──────────────────────────────────────────────────────────────────────────────
-- Provisioning job log  (async state machine + distributed worker queue)
--
-- CF-Provisioner uses this table as both an audit log and a distributed task
-- queue. Every provisioning request inserts a row with status=QUEUED.
-- A background worker picks up QUEUED jobs and transitions them with LWT:
--
--   UPDATE cf.provisioning_jobs
--     SET status = 'PROVISIONING', started_at = toTimestamp(now())
--     WHERE tenant_id = ? AND job_id = ?
--     IF status = 'QUEUED';
--
-- Only one worker replica can claim a job (LWT guarantees exactly 1 winner).
-- On completion it sets status = READY or FAILED.
--
-- The result column holds a JSON blob: { api_key_id, vpc_info }.
-- The raw API key is NEVER stored here.
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.provisioning_jobs (
    job_id            UUID,
    tenant_id         UUID,
    idempotency_key   TEXT,
    operation         TEXT,       -- PROVISION_VPC | DEPROVISION_VPC
    status            TEXT,       -- QUEUED | PROVISIONING | READY | FAILED
    error_message     TEXT,
    result            TEXT,       -- JSON: {api_key_id, key_hash, vpc_info}
    started_at        TIMESTAMP,
    completed_at      TIMESTAMP,
    PRIMARY KEY (tenant_id, job_id)
) WITH CLUSTERING ORDER BY (job_id DESC);

-- Idempotency deduplication table.
-- LWT insert ensures only one job per (tenant_id, idempotency_key).
-- A duplicate request returns the existing job_id without creating a second
-- vCluster — preventing UI double-clicks and network retries from
-- provisioning duplicate environments.
-- Validated by: scylladb-accounts spike Benchmark 2 (LWT correctness).
CREATE TABLE IF NOT EXISTS cf.provisioning_jobs_by_idem (
    tenant_id       UUID,
    idempotency_key TEXT,
    job_id          UUID,
    PRIMARY KEY ((tenant_id, idempotency_key))
);

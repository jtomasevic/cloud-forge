-- ============================================================
-- CF-Accounts: ScyllaDB Schema
-- Spike: ScyllaDB as Control Plane Account Store
-- ============================================================
--
-- Keyspace: SimpleStrategy RF=1 for local dev / spike.
-- Production: NetworkTopologyStrategy RF=3, QUORUM consistency.
--
-- Reference: docs/3-Introduce-CF-VPC.md § 6.3
-- ============================================================

CREATE KEYSPACE IF NOT EXISTS cf
  WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}
  AND durable_writes = true;

-- ──────────────────────────────────────────────────────────────────────────────
-- Tenant accounts
--
-- Primary lookup: by tenant_id (UUID) — used internally after slug resolution.
-- Hot path: cf.tenants_by_slug MV resolves a slug (from JWT sub / CLI flag)
--           to a tenant_id before any service call.
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.tenants (
    tenant_id     UUID,
    slug          TEXT,
    display_name  TEXT,
    status        TEXT,      -- PROVISIONING | ACTIVE | SUSPENDED | DELETED
    plan_id       TEXT,
    pod_cidr      TEXT,      -- vCluster pod CIDR, e.g. "10.100.0.0/20"
    svc_cidr      TEXT,      -- vCluster service CIDR
    created_at    TIMESTAMP,
    updated_at    TIMESTAMP,
    PRIMARY KEY (tenant_id)
);

-- Materialized view: resolve slug → tenant_id.
-- CF-Router and CF-IAM use this to translate the slug carried in JWT
-- into the internal tenant_id before forwarding requests downstream.
CREATE MATERIALIZED VIEW IF NOT EXISTS cf.tenants_by_slug AS
    SELECT * FROM cf.tenants
    WHERE slug IS NOT NULL AND tenant_id IS NOT NULL
    PRIMARY KEY (slug, tenant_id);

-- ──────────────────────────────────────────────────────────────────────────────
-- User records
--
-- Partitioned by tenant_id so all users of a tenant land on the same shard.
-- The users_by_email MV supports login flow (look up a user by email across
-- tenants, or within a tenant using the composite key).
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.users (
    user_id       UUID,
    tenant_id     UUID,
    email         TEXT,
    password_hash TEXT,      -- Argon2id hash (never plaintext)
    role          TEXT,      -- OWNER | ADMIN | MEMBER
    mfa_enabled   BOOLEAN,
    status        TEXT,      -- ACTIVE | SUSPENDED | DELETED
    created_at    TIMESTAMP,
    PRIMARY KEY (tenant_id, user_id)
);

-- Materialized view: look up a user by email (login / forgot-password flows).
CREATE MATERIALIZED VIEW IF NOT EXISTS cf.users_by_email AS
    SELECT * FROM cf.users
    WHERE email IS NOT NULL
      AND tenant_id IS NOT NULL
      AND user_id IS NOT NULL
    PRIMARY KEY (email, tenant_id, user_id);

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
--
-- The raw key is never stored. At creation the CF-Accounts service returns it
-- exactly once; after that only the hash exists.
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.api_keys (
    key_hash      TEXT,      -- hex(BLAKE2b-256(raw_key)) — lookup key
    key_id        UUID,      -- stable identifier for management operations
    tenant_id     UUID,
    user_id       UUID,      -- which user created / owns this key
    display_name  TEXT,
    scopes        TEXT,      -- comma-separated: "provision:write,provision:read"
    status        TEXT,      -- ACTIVE | ROTATED | REVOKED
    expiry        TIMESTAMP, -- NULL = never expires
    last_used_at  TIMESTAMP,
    created_at    TIMESTAMP,
    PRIMARY KEY (key_hash)
);

-- ──────────────────────────────────────────────────────────────────────────────
-- Provisioned service instances
--
-- Authoritative inventory of every service a tenant has provisioned.
-- CF-Router uses status to gate requests; CF-Provisioner updates endpoints
-- and status after successful provisioning.
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.service_instances (
    tenant_id     UUID,
    instance_id   UUID,
    service_type  TEXT,      -- NATS | MINIO | POSTGRESQL | KNATIVE | SCYLLADB
    display_name  TEXT,
    status        TEXT,      -- PENDING | PROVISIONING | READY | ERROR | DELETED
    version       TEXT,
    parameters    TEXT,      -- JSON blob: service-specific provisioning request
    endpoints     TEXT,      -- JSON blob: internal/external connection strings
    created_at    TIMESTAMP,
    updated_at    TIMESTAMP,
    PRIMARY KEY (tenant_id, instance_id)
);

-- ──────────────────────────────────────────────────────────────────────────────
-- Provisioning job log  (state machine + distributed worker queue)
--
-- CF-Provisioner uses this table as both an audit log and a distributed task
-- queue.  Every provisioning request inserts a row with status=QUEUED.  A
-- background worker picks up QUEUED jobs and transitions them with LWT:
--
--   UPDATE cf.provisioning_jobs
--     SET status = 'PROVISIONING', started_at = toTimestamp(now())
--     WHERE tenant_id = ? AND job_id = ?
--     IF status = 'QUEUED';
--
-- Only one worker replica can claim a job (the LWT guarantees this).  On
-- completion it sets status=SUCCEEDED or FAILED.
--
-- Idempotency: the client includes an idempotency_key (e.g. UUID).  Before
-- inserting a new job the handler checks cf.provisioning_jobs_by_idem for an
-- existing record with the same (tenant_id, idempotency_key).  If one exists
-- the existing job_id is returned without creating a duplicate.
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cf.provisioning_jobs (
    job_id            UUID,
    tenant_id         UUID,
    idempotency_key   TEXT,
    operation         TEXT,      -- PROVISION | DEPROVISION | MODIFY
    service_type      TEXT,
    instance_id       UUID,
    status            TEXT,      -- QUEUED | PROVISIONING | SUCCEEDED | FAILED
    error_message     TEXT,
    started_at        TIMESTAMP,
    completed_at      TIMESTAMP,
    PRIMARY KEY (tenant_id, job_id)
) WITH CLUSTERING ORDER BY (job_id DESC);

-- Secondary lookup table for idempotency-key deduplication.
-- LWT insert: INSERT ... IF NOT EXISTS ensures only one row per
-- (tenant_id, idempotency_key), preventing duplicate provisioning jobs from
-- UI double-clicks or network retries.
CREATE TABLE IF NOT EXISTS cf.provisioning_jobs_by_idem (
    tenant_id       UUID,
    idempotency_key TEXT,
    job_id          UUID,
    PRIMARY KEY ((tenant_id, idempotency_key))
);

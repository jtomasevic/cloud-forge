-- =============================================================================
-- CloudForge Platform Keyspace and Schema
-- Applied by the scylladb-schema-init Kubernetes Job on first cluster startup.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Keyspace
--
-- NetworkTopologyStrategy with the datacenter named "local" matches the
-- ScyllaCluster manifest (datacenter.name = local).
-- RF=1 for dev; CF-DBController sets RF=3 in production.
-- -----------------------------------------------------------------------------
CREATE KEYSPACE IF NOT EXISTS cloudforge_platform
    WITH REPLICATION = {
        'class': 'NetworkTopologyStrategy',
        'local': 1
    }
    AND DURABLE_WRITES = true;

-- -----------------------------------------------------------------------------
-- Resource Capability Bindings
--
-- Stores platform resource-to-resource permission records.
-- See pkg/authz/README.md for the full design rationale.
--
-- Primary access pattern (hot path — runs on every event route decision):
--   CHECK  SELECT id FROM cloudforge_platform.resource_bindings
--          WHERE tenant_id = ? AND binding_key = ?
--
-- binding_key is assembled by the Go layer as a composite string:
--   <subject_kind>#<subject_name>#<permission>#<target_kind>#<target_name>
--
--   Example: "function#process-orders#consume#queue#order-events"
--
-- This makes the CHECK query a single-partition point lookup (O(1) in ScyllaDB)
-- and "list all bindings for subject X" a narrow prefix range scan on the same
-- partition — no scatter reads, no secondary index fan-out.
--
-- The unpacked columns (subject_kind, subject_name, …) duplicate data from
-- binding_key for readability and direct Go struct mapping via gocqlx.
-- ScyllaDB stores them on the same SSTable row at no extra I/O cost.
--
-- DURABLE_WRITES = true: commit log is flushed before acknowledging a write.
-- Required for permission records — a lost create or revoke silently grants
-- or retains access.
--
-- LeveledCompactionStrategy: minimises read amplification for uniform
-- point-lookup workloads (each tenant's bindings form a narrow partition).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS cloudforge_platform.resource_bindings (
    -- Partition key: all bindings for a tenant land on the same shard.
    -- Correct for SME scale (tens to hundreds of tenants).
    tenant_id    TEXT,

    -- Clustering key: the full binding identity encoded as a single string.
    -- Prefix scans ("all bindings for function X") work without a separate index.
    binding_key  TEXT,

    -- Surrogate UUID used by the binding management API for create/revoke calls.
    id           UUID,

    -- Unpacked binding fields for direct struct mapping and human readability.
    subject_kind TEXT,
    subject_name TEXT,
    permission   TEXT,
    target_kind  TEXT,
    target_name  TEXT,

    -- Audit columns: who created this binding and when.
    created_at   TIMESTAMP,
    created_by   TEXT,

    PRIMARY KEY (tenant_id, binding_key)
) WITH CLUSTERING ORDER BY (binding_key ASC)
  AND gc_grace_seconds = 86400
  AND compaction = {'class': 'LeveledCompactionStrategy'}
  AND comment = 'Platform resource-to-resource capability bindings. See pkg/authz/README.md.';

-- -----------------------------------------------------------------------------
-- Materialized View: look up bindings by target
--
-- Secondary access pattern:
--   LIST BY TARGET  SELECT * FROM cloudforge_platform.resource_bindings_by_target
--                   WHERE tenant_id = ? AND target_kind = ? AND target_name = ?
--
-- Serves the "what resources have permission over this Queue / Storage / …?"
-- query used by:
--   - the binding management API list-by-target endpoint
--   - the admission webhook that validates new resource bindings at creation time
--
-- A Materialized View is used instead of a secondary index because ScyllaDB
-- secondary indexes are shard-local and require scatter reads across all shards.
-- A MV is a full replica of the data with a different partition key — point
-- lookups remain single-partition regardless of cluster size.
-- -----------------------------------------------------------------------------
-- ScyllaDB MV constraint: only ONE non-primary-key column from the base table
-- may be added to the MV primary key.  The base table PK is (tenant_id,
-- binding_key), so we can only add target_kind here.
-- Queries that also need to filter by target_name do so in application code
-- after a range scan on (tenant_id, target_kind).
CREATE MATERIALIZED VIEW IF NOT EXISTS cloudforge_platform.resource_bindings_by_target AS
    SELECT *
    FROM cloudforge_platform.resource_bindings
    WHERE tenant_id   IS NOT NULL
      AND binding_key  IS NOT NULL
      AND target_kind  IS NOT NULL
    PRIMARY KEY (
        (tenant_id, target_kind),
        binding_key
    )
    WITH CLUSTERING ORDER BY (binding_key ASC);

-- -----------------------------------------------------------------------------
-- Platform service account
--
-- All CloudForge platform services authenticate with cloudforge_svc.
-- The default cassandra/cassandra superuser is used only for this one-time
-- bootstrap and must be disabled (or have its password changed) after init.
-- The password here is the dev default; production must inject it from a
-- Kubernetes Secret (CF_SCYLLA_PASSWORD env var).
-- -----------------------------------------------------------------------------
CREATE ROLE IF NOT EXISTS cloudforge_svc
    WITH PASSWORD = 'cf-dev-secret-change-in-prod'
    AND LOGIN     = true
    AND SUPERUSER = false;

GRANT ALL PERMISSIONS ON KEYSPACE cloudforge_platform TO cloudforge_svc;

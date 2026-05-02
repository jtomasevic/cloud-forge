// Package accounts provides the CloudForge control plane's account data layer,
// backed by ScyllaDB CQL.
//
// # Responsibility
//
// The accounts package owns the authoritative records for:
//   - Tenant accounts (tenant_id, slug, status, CIDR allocation, plan)
//   - API keys (hash-only storage — the raw key is never persisted)
//   - Provisioning jobs (async job queue + state machine log)
//
// It does NOT own user records, service instances, or credentials for services
// provisioned inside tenant vClusters. Those belong to separate packages.
//
// # ScyllaDB connection
//
// Callers obtain a *gocql.Session using [NewSession] and pass it to [TenantStore]
// and [APIKeyStore]. The session must be closed by the caller when the process
// exits.
//
// # Consistency model
//
// All hot-path reads (API key lookup, tenant slug resolution) use QUORUM
// consistency to guarantee that a revoked key is invisible within one
// replication round-trip. LWT (lightweight transactions, IF NOT EXISTS /
// IF status='...') are used for all state-changing writes to prevent races
// between concurrent CF-Provisioner replicas.
//
// Validated by: spikes/scylladb-accounts/ (Benchmarks 1–3).
//
// # Schema
//
// The CQL DDL lives in internal/accounts/schema/schema.cql and is applied
// at service startup via [ApplySchema].
package accounts

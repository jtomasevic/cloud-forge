// Package bench contains the benchmark harness for the ScyllaDB as
// Control Plane Account Store spike.
//
// It validates three things described in docs/3-Introduce-CF-VPC.md:
//
//  1. API key lookup latency — the CF-Router hot path hashes an incoming
//     bearer token with BLAKE2b-256 and does a single ScyllaDB partition
//     read.  The target is p99 < 2 ms at 50 concurrent goroutines.
//
//  2. LWT idempotency — CF-Provisioner uses lightweight transactions
//     (IF NOT EXISTS, IF status = 'QUEUED') to prevent duplicate
//     provisioning jobs under concurrent load.  We verify correctness
//     (exactly one winner) and measure write latency.
//
//  3. Materialized view query performance — CF-Router and CF-IAM resolve
//     tenant slugs (from JWTs) and user emails (login flow) through MV
//     lookups.  We benchmark both and compare with primary-key reads.
package bench

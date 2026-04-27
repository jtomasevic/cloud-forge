// Package spike contains the business logic for Spike 0.6 —
// NATS JetStream Multi-Tenant Routing.
//
// Each source file is scoped to one concern so individual functions can be
// unit-tested without requiring a running NATS cluster:
//
//	types.go        — shared data types
//	events.go       — CloudEvent construction helpers
//	stats.go        — latency percentile computation
//	dispatcher.go   — content-based routing dispatch (pure Go)
//	routing.go      — runContentBasedRouting (JetStream-backed)
//	isolation.go    — runIsolationTest (cross-account isolation)
//	benchmark.go    — runLatencyBenchmark (JetStream publish loop)
//	provisioning.go — runProvisioningTest + demonstrateConfigReload
//	connect.go      — NATS connection helpers
//	results.go      — result printing and pass/fail labelling
//	run.go          — Run() — top-level orchestration
package spike

// Command bench is the CLI entry point for the ScyllaDB Account Store spike.
//
// It connects to a running ScyllaDB instance, applies the CF-Accounts schema,
// seeds synthetic data, runs three benchmark suites (API key lookup, LWT
// idempotency, and materialized view queries), and prints a results table.
//
// Usage:
//
//	bench [flags]
//
// Flags:
//
//	-host     string   ScyllaDB host (default: 127.0.0.1)
//	-port     int      CQL port (default: 9042)
//	-keyspace string   Target keyspace (default: cf)
//	-seed     int      Rows to seed before benchmarking (default: 1000)
//	-ops      int      Operations per benchmark (default: 2000)
//	-conc     int      Concurrent goroutines (default: 50)
//	-writers  int      Concurrent LWT writers (default: 20)
//	-drop     bool     Drop the keyspace before applying schema (default: false)
//
// The benchmark connects to the ScyllaDB node deployed by Task 0.7 via the
// k3d port mapping 9042:9042.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cloud-forge/spikes/scylladb-accounts/internal/bench"
	"github.com/gocql/gocql"
)

func main() {
	// ── Flags ─────────────────────────────────────────────────────────────────
	host := flag.String("host", "127.0.0.1", "ScyllaDB CQL host")
	port := flag.Int("port", 9042, "ScyllaDB CQL port")
	keyspace := flag.String("keyspace", "cf", "CQL keyspace name")
	user := flag.String("user", "cassandra", "CQL username")
	password := flag.String("password", "cassandra", "CQL password")
	seedRows := flag.Int("seed", 1000, "Rows to insert before benchmarking")
	ops := flag.Int("ops", 2000, "Read operations per benchmark")
	conc := flag.Int("conc", 50, "Concurrent goroutines per benchmark")
	writers := flag.Int("writers", 20, "Concurrent LWT writer goroutines")
	dropFirst := flag.Bool("drop", false, "Drop and recreate the keyspace before running")
	schemaPath := flag.String("schema", "schema/schema.cql", "Path to the CQL schema file")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := bench.Config{
		Hosts:          []string{*host},
		Port:           *port,
		Keyspace:       *keyspace,
		Username:       *user,
		Password:       *password,
		SeedRows:       *seedRows,
		Ops:            *ops,
		Concurrency:    *conc,
		LWTWriters:     *writers,
		ConnectTimeout: 15 * time.Second,
		QueryTimeout:   10 * time.Second,
	}

	// ── Connect ───────────────────────────────────────────────────────────────
	log.Info("connecting to ScyllaDB", "host", cfg.Hosts[0], "port", cfg.Port)
	sess, err := bench.NewSession(cfg)
	if err != nil {
		log.Error("connection failed", "err", err)
		os.Exit(1)
	}
	defer sess.Close()
	log.Info("connected")

	// ── Schema ────────────────────────────────────────────────────────────────
	if *dropFirst {
		log.Info("dropping keyspace", "keyspace", cfg.Keyspace)
		if err := bench.DropSchema(sess); err != nil {
			log.Error("drop failed", "err", err)
			os.Exit(1)
		}
	}

	log.Info("applying schema")
	schemaContent, err := os.ReadFile(*schemaPath)
	if err != nil {
		log.Error("read schema file failed", "path", *schemaPath, "err", err)
		os.Exit(1)
	}
	if err := bench.ApplySchema(sess, string(schemaContent)); err != nil {
		log.Error("schema apply failed", "err", err)
		os.Exit(1)
	}

	// ScyllaDB builds materialized views asynchronously. Wait up to 30s before
	// starting MV benchmarks to avoid misleading latency spikes during build.
	log.Info("waiting for MV readiness", "timeout", "30s")
	if err := bench.WaitForMVReady(sess, 30*time.Second); err != nil {
		log.Warn("MV not ready — MV benchmarks may show elevated latency", "err", err)
	}

	// ── Seed ──────────────────────────────────────────────────────────────────
	log.Info("seeding API keys", "count", cfg.SeedRows)
	hashes, err := bench.SeedAPIKeys(sess, cfg.SeedRows)
	if err != nil {
		log.Error("seed api_keys failed", "err", err)
		os.Exit(1)
	}

	log.Info("seeding tenants", "count", cfg.SeedRows)
	tenants, err := bench.SeedTenants(sess, cfg.SeedRows)
	if err != nil {
		log.Error("seed tenants failed", "err", err)
		os.Exit(1)
	}

	log.Info("seeding users", "count", cfg.SeedRows)
	users, err := bench.SeedUsers(sess, tenants, cfg.SeedRows)
	if err != nil {
		log.Error("seed users failed", "err", err)
		os.Exit(1)
	}

	// ── Benchmark 1: API key lookup (QUORUM vs ONE) ───────────────────────────
	fmt.Println("\n── Benchmark 1: API key lookup (routing hot path) ──────────────────────────")
	fmt.Printf("   %d ops  ·  %d goroutines  ·  %d seeded keys\n\n",
		cfg.Ops, cfg.Concurrency, len(hashes))

	apiKeyQuorum := bench.BenchAPIKeyLookup(sess, cfg, hashes, gocql.Quorum, bench.BenchAPIKeyQuorum)
	apiKeyOne := bench.BenchAPIKeyLookup(sess, cfg, hashes, gocql.One, bench.BenchAPIKeyOne)

	bench.PrintResults(os.Stdout, []bench.Result{apiKeyQuorum, apiKeyOne})

	// ── Benchmark 2: LWT idempotency ─────────────────────────────────────────
	fmt.Println("\n── Benchmark 2: LWT idempotency (provisioning job state machine) ──────────")
	fmt.Printf("   %d concurrent writers racing on the same idempotency key\n\n", cfg.LWTWriters)

	claimResult := bench.BenchLWTJobClaim(sess, cfg)
	bench.PrintLWTResult(os.Stdout, claimResult)
	if !claimResult.Correct() {
		log.Error("LWT job-claim correctness FAILED",
			"winners", claimResult.Winners, "expected", 1)
	}

	transResult, err := bench.BenchLWTStateTransition(sess, cfg)
	if err != nil {
		log.Error("LWT state-transition benchmark failed", "err", err)
		os.Exit(1)
	}
	bench.PrintLWTResult(os.Stdout, transResult)
	if !transResult.Correct() {
		log.Error("LWT state-transition correctness FAILED",
			"winners", transResult.Winners, "expected", 1)
	}

	// ── Benchmark 3: Materialized view queries ────────────────────────────────
	fmt.Println("\n── Benchmark 3: Materialized view queries ───────────────────────────────────")
	fmt.Printf("   %d ops  ·  %d goroutines  ·  %d tenants / %d users\n\n",
		cfg.Ops, cfg.Concurrency, len(tenants), len(users))

	mvSlug := bench.BenchMVSlugLookup(sess, cfg, tenants)
	mvEmail := bench.BenchMVEmailLookup(sess, cfg, users)

	bench.PrintResults(os.Stdout, []bench.Result{mvSlug, mvEmail})

	// ── Summary ───────────────────────────────────────────────────────────────
	fmt.Println("\n── Summary ──────────────────────────────────────────────────────────────────")
	allResults := []bench.Result{apiKeyQuorum, apiKeyOne, mvSlug, mvEmail}
	pass := true
	for _, r := range allResults {
		if r.Errors > 0 {
			pass = false
		}
	}
	if !claimResult.Correct() || !transResult.Correct() {
		pass = false
	}
	if pass {
		fmt.Println("  Overall: PASS — ScyllaDB meets all CF-Accounts requirements.")
	} else {
		fmt.Println("  Overall: FAIL — see individual results above.")
		os.Exit(1)
	}
}

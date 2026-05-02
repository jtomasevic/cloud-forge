// cf-provisioner is the CloudForge VPC provisioning service.
//
// It exposes a REST API for tenant network lifecycle management:
//
//	POST   /api/v1/vpc/provision       → 202 Accepted { job_id, status }
//	GET    /api/v1/vpc/jobs/{job_id}   → { status, vpc_info, api_key }
//	DELETE /api/v1/vpc/{tenant_id}     → 202 Accepted { job_id, status }
//
// All provisioning work runs in a background goroutine; the HTTP handler
// returns immediately with a job_id. Clients poll GET /vpc/jobs/{id} until
// status reaches READY or FAILED.
//
// # Architecture
//
// This binary is the thin entry point. All logic lives in the packages below:
//   - [services/provisioner]         — REST API layer (handler, router, models, errors)
//   - [services/provisioner/service] — Service layer (workflows, business logic)
//   - [internal/accounts]            — DB layer: ScyllaDB CQL access
//   - [internal/provisioner]         — DB/external layer: OpenBao, vCluster, Cilium
//
// # Configuration (environment variables)
//
//	SCYLLA_HOSTS      Comma-separated ScyllaDB contact points  (default: 127.0.0.1)
//	SCYLLA_PORT       CQL port                                 (default: 19042)
//	SCYLLA_USER       ScyllaDB username (optional)
//	SCYLLA_PASS       ScyllaDB password (optional)
//	OPENBAO_ADDR      OpenBao API address                      (default: http://localhost:8200)
//	OPENBAO_TOKEN     OpenBao root or provisioner token        (default: dev-root-token)
//	LISTEN_ADDR       HTTP bind address                        (default: :8080)
//
// # Local dev quick-start
//
//	make dev-up                          # start k3d cluster with all deps
//	make scylladb-port-forward &         # CQL on localhost:19042
//	make openbao-port-forward &          # Vault API on localhost:8200
//	go run ./cmd/cf-provisioner          # start the service
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// osExit is called by main() to terminate the process on error.
// Replaced in tests to capture the exit code without calling os.Exit.
var osExit = os.Exit //nolint:gochecknoglobals // test seam for os.Exit

func main() {
	if err := run(); err != nil {
		osExit(1)
	}
}

// wireFunc is the application factory called by run(). It can be replaced in
// tests to inject a mock App without requiring live ScyllaDB / OpenBao.
var wireFunc = Wire

// run contains all startup and serving logic. Using a separate function ensures
// that deferred cleanup (app.Shutdown) always executes, even when an error
// forces an early return — os.Exit in main() bypasses defer.
func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := configFromEnv()
	if err != nil {
		log.Error("invalid configuration", "err", err)
		return err
	}

	app, err := wireFunc(context.Background(), &cfg, log)
	if err != nil {
		log.Error("wire application", "err", err)
		return err
	}
	defer app.Shutdown()

	// ── HTTP server with graceful shutdown ───────────────────────────────────
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      app.Router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		log.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	log.Info("cf-provisioner started", "addr", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("ListenAndServe", "err", err)
		return err
	}
	return nil
}

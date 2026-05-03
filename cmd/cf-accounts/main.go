// cf-accounts is the CloudForge control plane accounts and tenant lifecycle service.
//
// It exposes a REST API for managing tenant accounts and API keys:
//
//	POST   /api/v1/accounts                          → 202 Accepted { tenant_id, job_id }
//	GET    /api/v1/accounts/{tenant_slug}            → { tenant_id, status, cidr, ... }
//	DELETE /api/v1/accounts/{tenant_slug}            → 202 Accepted { job_id }
//	POST   /api/v1/accounts/{tenant_slug}/keys       → 201 Created { raw_key (once) }
//	DELETE /api/v1/accounts/{tenant_slug}/keys/{id}  → 204 No Content
//
// Account creation is asynchronous: POST /accounts returns immediately with a
// job_id. The VPC provisioning workflow (vCluster, Cilium policies, kubeconfig,
// API key) runs in a background goroutine via the embedded CFProvisionerService.
// Poll GET /accounts/{slug} for status=ACTIVE.
//
// # Architecture
//
// This binary is the thin entry point (< 60 lines). All logic lives in:
//   - [services/accounts]         — REST API layer (handler, router, models, errors)
//   - [services/accounts/service] — Service layer (AccountsService, business logic)
//   - [services/provisioner/service] — VPC provisioner (used as a Go library, same process)
//   - [internal/accounts]         — DB layer: ScyllaDB CQL stores
//   - [internal/provisioner]      — DB/external: OpenBao, vCluster, Cilium
//
// # Configuration
//
//	SCYLLA_HOSTS   Comma-separated contact points  (default: 127.0.0.1)
//	SCYLLA_PORT    CQL port                        (default: 19042)
//	SCYLLA_USER    ScyllaDB username (optional)
//	SCYLLA_PASS    ScyllaDB password (optional)
//	OPENBAO_ADDR   OpenBao API address             (default: http://localhost:8200)
//	OPENBAO_TOKEN  OpenBao token                   (default: dev-root-token)
//	LISTEN_ADDR    HTTP bind address               (default: :8082)
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
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Info("cf-accounts started", "addr", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("ListenAndServe", "err", err)
		return err
	}
	return nil
}

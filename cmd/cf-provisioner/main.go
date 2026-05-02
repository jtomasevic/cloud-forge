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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	openbao "github.com/openbao/openbao/api/v2"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	provisionersvc "github.com/jtomasevic/cloud-forge/services/provisioner"
	"github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := configFromEnv()
	if err != nil {
		log.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	// ── ScyllaDB ─────────────────────────────────────────────────────────────
	sess, err := accounts.NewSession(cfg.Scylla)
	if err != nil {
		log.Error("connect ScyllaDB", "err", err)
		os.Exit(1)
	}
	defer sess.Close()

	// ── OpenBao ──────────────────────────────────────────────────────────────
	baoCfg := openbao.DefaultConfig()
	baoCfg.Address = cfg.OpenBaoAddr
	baoClient, err := openbao.NewClient(baoCfg)
	if err != nil {
		log.Error("create OpenBao client", "err", err)
		os.Exit(1)
	}
	baoClient.SetToken(cfg.OpenBaoToken)

	// ── Wire service and handler ─────────────────────────────────────────────
	svc := service.New(service.Deps{
		Tenants: accounts.NewTenantStore(sess),
		Keys:    accounts.NewAPIKeyStore(sess),
		Jobs:    accounts.NewJobStore(sess),
		Bao:     baoClient,
	})

	reg := prometheus.NewRegistry()
	router := provisionersvc.NewRouter(
		provisionersvc.NewHandler(svc),
		log,
		reg,
		"provisioner_svc",
	)

	// ── HTTP server with graceful shutdown ───────────────────────────────────
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
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
		os.Exit(1)
	}
}

// ── Configuration ─────────────────────────────────────────────────────────────

type appConfig struct {
	Scylla       accounts.Config
	OpenBaoAddr  string
	OpenBaoToken string
	ListenAddr   string
}

func configFromEnv() (appConfig, error) {
	hosts := os.Getenv("SCYLLA_HOSTS")
	if hosts == "" {
		hosts = "127.0.0.1"
	}

	port := 19042
	if p := os.Getenv("SCYLLA_PORT"); p != "" {
		if _, err := fmt.Sscanf(p, "%d", &port); err != nil {
			return appConfig{}, fmt.Errorf("invalid SCYLLA_PORT %q: %w", p, err)
		}
	}

	baoAddr := os.Getenv("OPENBAO_ADDR")
	if baoAddr == "" {
		baoAddr = "http://localhost:8200"
	}
	baoToken := os.Getenv("OPENBAO_TOKEN")
	if baoToken == "" {
		baoToken = "dev-root-token"
	}

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	return appConfig{
		Scylla: accounts.Config{
			Hosts:          strings.Split(hosts, ","),
			Port:           port,
			Username:       os.Getenv("SCYLLA_USER"),
			Password:       os.Getenv("SCYLLA_PASS"),
			ConnectTimeout: 10 * time.Second,
			QueryTimeout:   5 * time.Second,
		},
		OpenBaoAddr:  baoAddr,
		OpenBaoToken: baoToken,
		ListenAddr:   listenAddr,
	}, nil
}

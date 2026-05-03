package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gocql/gocql"
	openbao "github.com/openbao/openbao/api/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	provisionersvc "github.com/jtomasevic/cloud-forge/services/provisioner"
	"github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// App is the fully-wired provisioner application: a composed HTTP handler and
// a shutdown function. main.go only starts the server and handles OS signals.
type App struct {
	// Router is the fully-composed HTTP handler ready to be served.
	Router http.Handler

	// Shutdown must be called after the HTTP server stops accepting connections
	// to release all held resources (DB sessions, etc.).
	Shutdown func()

	// Log is the structured logger shared across all components.
	Log *slog.Logger
}

// Test seams — replaced in unit tests to avoid live infrastructure dependencies.
var (
	// newScyllaSessionFn is called by Wire to open a ScyllaDB session.
	newScyllaSessionFn = accounts.NewSession
	// newBaoClientFn is called by Wire to create an OpenBao API client.
	newBaoClientFn = openbao.NewClient
	// sessionCloseFn is called by the closeSession closure when a real
	// *gocql.Session was opened. Replaced in tests to avoid closing a
	// zero-value session (which would panic).
	sessionCloseFn func(*gocql.Session) = func(s *gocql.Session) { s.Close() } //nolint:gochecknoglobals // test seam
)

// Wire constructs and connects all application dependencies.
//
// Dependency graph:
//
//	accounts.NewSession()        → *gocql.Session
//	accounts.NewTenantStore()    → *accounts.TenantStore
//	accounts.NewAPIKeyStore()    → *accounts.APIKeyStore
//	accounts.NewJobStore()       → *accounts.JobStore
//	openbao.NewClient()          → *openbao.Client
//	service.New()                → ProvisionerService  (VPC library)
//	provisionersvc.NewHandler()  → *Handler            (REST adapter)
//	provisionersvc.NewRouter()   → http.Handler        (mux + middleware)
//
// Wire is intentionally called only from main to keep boot concerns in one place.
func Wire(_ context.Context, cfg *appConfig, log *slog.Logger) (*App, error) {
	// ── ScyllaDB session ─────────────────────────────────────────────────────
	sess, err := newScyllaSessionFn(&cfg.Scylla)
	if err != nil {
		return nil, err
	}

	// closeSession is nil-safe so that test seams returning (nil, nil) from
	// newScyllaSessionFn don't panic when the session is "closed".
	closeSession := func() {}
	if sess != nil {
		closeSession = func() { sessionCloseFn(sess) }
	}

	// ── OpenBao client ────────────────────────────────────────────────────────
	baoCfg := openbao.DefaultConfig()
	baoCfg.Address = cfg.OpenBaoAddr
	baoClient, err := newBaoClientFn(baoCfg)
	if err != nil {
		closeSession()
		return nil, err
	}
	baoClient.SetToken(cfg.OpenBaoToken) //nolint:gosec // token is a dev default read from env; not a hardcoded secret

	return assembleApp(sess, baoClient, closeSession, log, cfg.DevCORSOrigins), nil
}

// assembleApp wires stores, services, and the HTTP handler from already-opened
// infrastructure clients. Separated from Wire so unit tests can exercise the
// full assembly path without real infrastructure (no queries during construction).
func assembleApp(
	sess *gocql.Session,
	baoClient *openbao.Client,
	closeSession func(),
	log *slog.Logger,
	corsOrigins []string,
) *App {
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
		corsOrigins,
	)

	return &App{
		Router:   router,
		Log:      log,
		Shutdown: closeSession,
	}
}

package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gocql/gocql"
	openbao "github.com/openbao/openbao/api/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	accountsapi "github.com/jtomasevic/cloud-forge/services/accounts"
	accountssvc "github.com/jtomasevic/cloud-forge/services/accounts/service"
	provisionersvc "github.com/jtomasevic/cloud-forge/services/provisioner/service"
)

// App is the fully-wired application: a composed HTTP handler and a shutdown
// function. main.go only starts the server and handles OS signals.
type App struct {
	// Router is the fully-composed HTTP handler ready to be served.
	Router http.Handler

	// Shutdown must be called after the HTTP server stops accepting connections
	// to release all held resources (DB sessions, watchers, etc.).
	Shutdown func()

	// Log is the structured logger shared across all components.
	Log *slog.Logger
}

// Test seams — replaced in unit tests to avoid live infrastructure dependencies.
// Production code always uses the real constructors.
var (
	// newScyllaSessionFn is called by Wire to open a ScyllaDB session.
	newScyllaSessionFn = accounts.NewSession
	// newBaoClientFn is called by Wire to create an OpenBao API client.
	newBaoClientFn = openbao.NewClient
)

// Wire constructs and connects all application dependencies.
//
// Dependency graph:
//
//	accounts.NewSession()        → *gocql.Session
//	accounts.NewTenantStore()    → *accounts.TenantStore
//	accounts.NewUserStore()      → *accounts.UserStore
//	accounts.NewAPIKeyStore()    → *accounts.APIKeyStore
//	accounts.NewJobStore()       → *accounts.JobStore
//	openbao.NewClient()          → *openbao.Client
//	provisionersvc.New()         → ProvisionerService  (VPC library, same process)
//	accountssvc.New()            → AccountsService     (business logic)
//	accountsapi.NewHandler()     → *accountsapi.Handler (REST adapter)
//	accountsapi.NewRouter()      → http.Handler        (mux + middleware)
//
// Wire is intentionally called only from main to keep boot concerns in one place.
func Wire(_ context.Context, cfg *appConfig, log *slog.Logger) (*App, error) {
	// ── ScyllaDB session ─────────────────────────────────────────────────────
	sess, err := newScyllaSessionFn(&cfg.Scylla)
	if err != nil {
		return nil, err
	}

	// closeSession is built separately so it is nil-safe when a test injects a
	// factory that returns (nil, nil) — a valid test seam that avoids spawning
	// a real database but still exercises the wiring path.
	closeSession := func() {}
	if sess != nil {
		closeSession = func() { sess.Close() }
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

	return assembleApp(sess, baoClient, closeSession, log), nil
}

// assembleApp wires stores, services, and the HTTP handler from already-opened
// infrastructure clients. It is separated from Wire so that unit tests can
// exercise the full assembly path with a nil ScyllaDB session (no queries are
// issued during construction — only at request time).
func assembleApp(
	sess *gocql.Session,
	baoClient *openbao.Client,
	closeSession func(),
	log *slog.Logger,
) *App {
	tenants := accounts.NewTenantStore(sess)
	users := accounts.NewUserStore(sess)
	keys := accounts.NewAPIKeyStore(sess)
	jobs := accounts.NewJobStore(sess)

	// Provisioner service (VPC library — same process, no HTTP)
	provSvc := provisionersvc.New(provisionersvc.Deps{
		Tenants: tenants,
		Keys:    keys,
		Jobs:    jobs,
		Bao:     baoClient,
	})

	// Accounts service
	svc := accountssvc.New(accountssvc.Deps{
		Tenants:      tenants,
		Users:        users,
		Keys:         keys,
		Provisioner:  provSvc,
		KeyGenerator: keys, // *accounts.APIKeyStore implements provisioner.APIKeyStorer
	})

	// REST layer
	reg := prometheus.NewRegistry()
	handler := accountsapi.NewHandler(svc, log)
	router := accountsapi.NewRouter(handler, log, reg, "accounts_svc")

	return &App{
		Router:   router,
		Log:      log,
		Shutdown: closeSession,
	}
}

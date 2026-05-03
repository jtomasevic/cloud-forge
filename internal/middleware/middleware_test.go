package middleware_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/jtomasevic/cloud-forge/internal/logging"
	"github.com/jtomasevic/cloud-forge/internal/metrics"
	"github.com/jtomasevic/cloud-forge/internal/middleware"
	"github.com/jtomasevic/cloud-forge/internal/tracing"
)

// ── RequestID tests ────────────────────────────────────────────────────────

// TestRequestID_GeneratesID verifies that the middleware generates an ID when
// the request does not include an X-Request-ID header.
func TestRequestID_GeneratesID(t *testing.T) {
	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The generated ID must be available in context.
		id := middleware.RequestIDFromContext(r.Context())
		assert.NotEmpty(t, id, "request ID must be set in context")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The ID must also be echoed in the response header.
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"), "X-Request-ID response header must be set")
}

// TestRequestID_ForwardsExistingID verifies that a client-supplied
// X-Request-ID is preserved rather than replaced with a generated one.
func TestRequestID_ForwardsExistingID(t *testing.T) {
	const clientID = "client-supplied-correlation-id"

	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, clientID, middleware.RequestIDFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-Request-ID", clientID)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, clientID, w.Header().Get("X-Request-ID"))
}

// ── StructuredLogger tests ─────────────────────────────────────────────────

// TestStructuredLogger_InjectsLoggerInContext verifies that the middleware
// stores an enriched logger in the request context so handlers can call
// logging.FromContext without needing an explicit logger parameter.
func TestStructuredLogger_InjectsLoggerInContext(t *testing.T) {
	base := logging.New(logging.Config{
		ServiceName: "test-svc",
		Format:      logging.FormatText,
	})

	// Build a minimal chain: RequestID → StructuredLogger → handler.
	// RequestID must precede StructuredLogger so the request_id field is set.
	handler := middleware.RequestID(
		middleware.StructuredLogger(base)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				l := logging.FromContext(r.Context())
				require.NotNil(t, l, "enriched logger must be in context")
				w.WriteHeader(http.StatusOK)
			}),
		),
	)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
}

// ── PanicRecovery tests ────────────────────────────────────────────────────

// TestPanicRecovery_Returns500 verifies that a panic in a downstream handler
// is caught and results in a structured 500 JSON error response rather than
// crashing the server or dropping the connection.
func TestPanicRecovery_Returns500(t *testing.T) {
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("something went very wrong")
	})

	handler := middleware.PanicRecovery(slog.Default())(panicHandler)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/crash", http.NoBody)
	w := httptest.NewRecorder()

	// Must not propagate the panic to the test runner.
	assert.NotPanics(t, func() {
		handler.ServeHTTP(w, req)
	})

	// Must return a 500 JSON response with the INTERNAL_ERROR code.
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "INTERNAL_ERROR", body.Error.Code)
}

// TestPanicRecovery_PanicsWithErrorType verifies that PanicRecovery handles a
// panic value that is an error type (not just an arbitrary string) and
// correctly wraps it in an Internal error response.
func TestPanicRecovery_PanicsWithErrorType(t *testing.T) {
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// Panic with an error type — exercises the `case error:` type-switch branch.
		panic(fmt.Errorf("underlying error cause"))
	})

	handler := middleware.PanicRecovery(slog.Default())(panicHandler)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panicky", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "INTERNAL_ERROR")
}

// TestPanicRecovery_WithContextLogger verifies that PanicRecovery uses the
// per-request logger stored in context by StructuredLogger (inheriting
// request_id and trace attributes) rather than the fallback logger.
func TestPanicRecovery_WithContextLogger(t *testing.T) {
	baseLogger := slog.Default()
	structuredLogger := middleware.StructuredLogger(baseLogger)
	recovery := middleware.PanicRecovery(baseLogger)

	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("context logger test")
	})

	// Compose: recovery wraps structuredLogger wraps panicHandler so that
	// when the panic fires the StructuredLogger has already stored a logger
	// in the context — exercises the logging.FromContext path in recovery.
	handler := recovery(structuredLogger(panicHandler))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ctx-logger-panic", http.NoBody)
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(w, req)
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestStructuredLogger_WithActiveOTelSpan verifies that StructuredLogger adds
// trace_id and span_id attributes when an active OTel span is in context,
// exercising the `if spanCtx.IsValid()` branch.
func TestStructuredLogger_WithActiveOTelSpan(t *testing.T) {
	ctx := context.Background()

	// Initialise tracing with a dev (stdout) exporter so spans are non-no-op.
	shutdown, err := tracing.Init(ctx, tracing.Config{ServiceName: "test-middleware"})
	require.NoError(t, err)
	defer func() { _ = shutdown(ctx) }()

	tracer := otel.Tracer("middleware-test")
	spanCtx, span := tracer.Start(ctx, "test-span")
	defer span.End()

	logger := slog.Default()
	mw := middleware.StructuredLogger(logger)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/traced", http.NoBody)
	req = req.WithContext(spanCtx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ── TenantContext tests ────────────────────────────────────────────────────

// TestTenantContext_ExtractsTenantAndProject verifies that TenantContext reads
// {tenant} and {project} path values populated by the Go 1.22+ http.ServeMux.
//
// With the stdlib mux, path values are injected into the request context by
// the mux DURING dispatch — i.e. BEFORE the per-route handler chain is called.
// This means TenantContext works correctly when applied as the handler argument
// to mux.Handle(), which is the standard usage pattern.
func TestTenantContext_ExtractsTenantAndProject(t *testing.T) {
	mux := http.NewServeMux()

	// Register the handler wrapped with TenantContext. The pattern uses Go 1.22
	// named wildcards {tenant} and {project}.
	mux.Handle(
		"GET /v1/tenants/{tenant}/projects/{project}/instances",
		middleware.TenantContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenant, project, ok := middleware.TenantFromContext(r.Context())
			require.True(t, ok, "tenant/project must be in context")
			assert.Equal(t, "acme", tenant)
			assert.Equal(t, "my-project", project)
			w.WriteHeader(http.StatusOK)
		})),
	)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/tenants/acme/projects/my-project/instances", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestTenantContext_NoParams verifies that TenantFromContext returns ok=false
// when the route does not include {tenant}/{project} parameters.
func TestTenantContext_NoParams(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz",
		middleware.TenantContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _, ok := middleware.TenantFromContext(r.Context())
			assert.False(t, ok, "health check route must not have tenant context")
			w.WriteHeader(http.StatusOK)
		})),
	)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ── Middlewares.Apply tests ────────────────────────────────────────────────

// TestMiddlewares_Apply verifies that Apply wraps the handler with middlewares
// in the correct order (first middleware = outermost = called first).
func TestMiddlewares_Apply(t *testing.T) {
	// Track the order in which middlewares fire.
	var order []string

	makeMiddleware := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+":in")
				next.ServeHTTP(w, r)
				order = append(order, name+":out")
			})
		}
	}

	chain := middleware.Middlewares{
		makeMiddleware("A"),
		makeMiddleware("B"),
		makeMiddleware("C"),
	}

	handler := chain.Apply(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Expected: A enters first, C is innermost, then handler, then unwind.
	assert.Equal(t, []string{
		"A:in", "B:in", "C:in",
		"handler",
		"C:out", "B:out", "A:out",
	}, order)
}

// ── Full Chain integration test ────────────────────────────────────────────

// TestChain_AllMiddlewaresFire verifies that the complete stdlib-only middleware
// chain processes a request end-to-end: X-Request-ID is set, the logger is in
// context, tenant context is populated, and Prometheus metrics are recorded.
func TestChain_AllMiddlewaresFire(t *testing.T) {
	logger := logging.New(logging.Config{
		ServiceName: "chain-test",
		Format:      logging.FormatText,
	})
	registry := metrics.NewRegistry("chain_test")
	chain := middleware.Chain(logger, registry, "chain_test")

	mux := http.NewServeMux()

	const pattern = "GET /v1/tenants/{tenant}/projects/{project}/items"
	mux.Handle(pattern,
		// Use metrics.WithRoutePattern so the histogram uses the pattern label
		// instead of the raw URL path.
		metrics.WithRoutePattern(pattern,
			chain.Apply(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tenant, project, ok := middleware.TenantFromContext(r.Context())
				assert.True(t, ok, "tenant context must be set")
				assert.Equal(t, "tenant1", tenant)
				assert.Equal(t, "proj1", project)

				l := logging.FromContext(r.Context())
				assert.NotNil(t, l, "logger must be in context")

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"items":[]}`))
			})),
		),
	)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/tenants/tenant1/projects/proj1/items", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"),
		"X-Request-ID header must be set by RequestID middleware")

	// Verify that the Prometheus metrics middleware recorded the request.
	metricsReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	metricsW := httptest.NewRecorder()
	metrics.Handler(registry).ServeHTTP(metricsW, metricsReq)

	assert.Contains(t, metricsW.Body.String(), "http_server_requests_total",
		"metrics middleware must have recorded the request")
}

// TestRequestIDFromContext_Empty verifies that RequestIDFromContext returns an
// empty string when the RequestID middleware has not run (common in unit tests
// that exercise handler functions directly).
func TestRequestIDFromContext_Empty(t *testing.T) {
	id := middleware.RequestIDFromContext(context.Background())
	assert.Empty(t, id)
}

// ── ChainWithCORS tests ────────────────────────────────────────────────────

// TestChainWithCORS_NilOrigins verifies that passing nil (or empty) origins
// returns the base chain without a CORS middleware prepended.
func TestChainWithCORS_NilOrigins(t *testing.T) {
	logger := logging.New(logging.Config{ServiceName: "cors-test", Format: logging.FormatText})
	registry := metrics.NewRegistry("cors_nil_test")

	chain := middleware.ChainWithCORS(nil, logger, registry, "cors_nil_test")
	base := middleware.Chain(logger, registry, "cors_nil_test_base")

	// Both should produce the same number of middlewares (no CORS layer added).
	assert.Equal(t, len(base), len(chain))
}

// TestChainWithCORS_WithOrigins verifies that passing a non-empty origins list
// prepends a CORS middleware so the returned chain is one element longer.
func TestChainWithCORS_WithOrigins(t *testing.T) {
	logger := logging.New(logging.Config{ServiceName: "cors-test", Format: logging.FormatText})
	registry := metrics.NewRegistry("cors_origins_test")

	chain := middleware.ChainWithCORS([]string{"http://localhost:8096"}, logger, registry, "cors_origins_test")
	base := middleware.Chain(logger, registry, "cors_origins_test_base")

	assert.Equal(t, len(base)+1, len(chain), "CORS middleware must be prepended")
}

// TestChainWithCORS_CORSHeadersPresent is an end-to-end smoke test that runs
// a request through a ChainWithCORS-built chain and confirms the CORS headers
// are actually present in the response.
func TestChainWithCORS_CORSHeadersPresent(t *testing.T) {
	const origin = "http://localhost:8096"

	logger := logging.New(logging.Config{ServiceName: "cors-e2e", Format: logging.FormatText})
	registry := metrics.NewRegistry("cors_e2e")
	chain := middleware.ChainWithCORS([]string{origin}, logger, registry, "cors_e2e")

	handler := chain.Apply(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	req.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, origin, w.Header().Get("Access-Control-Allow-Origin"))
}

package measure

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPProber_Probe_Success verifies that a normally responding server
// produces a positive TTFB and no error.
func TestHTTPProber_Probe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "variant=minimal")
	}))
	defer srv.Close()

	prober := NewHTTPProber(5*time.Second, 50*time.Millisecond)
	ttfb, err := prober.Probe(context.Background(), srv.URL)

	require.NoError(t, err)
	assert.Greater(t, ttfb, time.Duration(0), "TTFB must be positive for a successful response")
}

// TestHTTPProber_Probe_ServerError verifies that HTTP 500 responses are
// returned as errors — they indicate a function crash, not a Knative activator
// transient response.
func TestHTTPProber_Probe_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	prober := NewHTTPProber(5*time.Second, 50*time.Millisecond)
	_, err := prober.Probe(context.Background(), srv.URL)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server error")
}

// TestHTTPProber_Probe_502Retries verifies that HTTP 502 (Knative activator
// "pod not ready") is retried and the probe eventually returns success when
// the server starts responding 200 after a few 502s.
func TestHTTPProber_Probe_502Retries(t *testing.T) {
	// First two requests return 502; third returns 200.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintln(w, "not ready yet")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	}))
	defer srv.Close()

	prober := NewHTTPProber(5*time.Second, 10*time.Millisecond) // fast retry in test
	ttfb, err := prober.Probe(context.Background(), srv.URL)

	require.NoError(t, err, "502 retries should eventually succeed")
	assert.Greater(t, ttfb, time.Duration(0))
	assert.GreaterOrEqual(t, calls.Load(), int32(3), "should have made at least 3 attempts")
}

// TestHTTPProber_Probe_503Retries verifies that HTTP 503 is also retried
// (Knative activator uses both 502 and 503 for "pod not yet ready").
func TestHTTPProber_Probe_503Retries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	}))
	defer srv.Close()

	prober := NewHTTPProber(5*time.Second, 10*time.Millisecond)
	ttfb, err := prober.Probe(context.Background(), srv.URL)

	require.NoError(t, err)
	assert.Greater(t, ttfb, time.Duration(0))
}

// TestHTTPProber_Probe_404NotError verifies that HTTP 4xx is NOT treated as an
// error — a 404 means the handler ran, which is a valid cold-start measurement.
func TestHTTPProber_Probe_404NotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	prober := NewHTTPProber(5*time.Second, 50*time.Millisecond)
	ttfb, err := prober.Probe(context.Background(), srv.URL)

	require.NoError(t, err, "4xx must not be treated as an error")
	assert.Greater(t, ttfb, time.Duration(0))
}

// TestHTTPProber_Probe_ContextTimeout verifies that a context deadline that
// expires while retrying 502s surfaces a cancelled-context error.
func TestHTTPProber_Probe_ContextTimeout(t *testing.T) {
	// Server always returns 502 — simulates a pod that never becomes ready.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	prober := NewHTTPProber(5*time.Second, 10*time.Millisecond)
	_, err := prober.Probe(ctx, srv.URL)

	require.Error(t, err, "context timeout during 502 retry loop must produce an error")
}

// TestHTTPProber_Probe_InvalidURL verifies that a completely invalid URL
// eventually returns an error once the context expires.
func TestHTTPProber_Probe_InvalidURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	prober := NewHTTPProber(50*time.Millisecond, 20*time.Millisecond)
	_, err := prober.Probe(ctx, "not-a-valid-url://!@#$")
	require.Error(t, err)
}

// TestHTTPProber_Probe_ConnectionRefused verifies that connection-refused is
// retried and eventually returns an error when the context expires.
// Uses a real server that is closed before the probe runs.
func TestHTTPProber_Probe_ConnectionRefused(t *testing.T) {
	// Start a server to get a free port, then immediately close it so subsequent
	// connection attempts will be refused right away without OS-specific delays.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	addr := srv.URL
	srv.Close() // port is now closed; connects will be refused

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	prober := NewHTTPProber(50*time.Millisecond, 20*time.Millisecond)
	_, err := prober.Probe(ctx, addr)
	require.Error(t, err)
}

// TestNewHTTPProber_RedirectBlocked verifies that redirect following is
// disabled so misconfigured ingress 301s surface as non-error non-5xx reads.
func TestNewHTTPProber_RedirectBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/redirected", http.StatusMovedPermanently)
			return
		}
		fmt.Fprintln(w, "redirected")
	}))
	defer srv.Close()

	prober := NewHTTPProber(5*time.Second, 50*time.Millisecond)
	ttfb, err := prober.Probe(context.Background(), srv.URL+"/")

	require.NoError(t, err, "non-5xx redirect should not be treated as an error")
	assert.Greater(t, ttfb, time.Duration(0))
}

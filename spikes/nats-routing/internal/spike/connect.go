package spike

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

// ConnectWithRetry attempts to connect to NATS up to 10 times with a 2s
// backoff between attempts.
//
// The retry loop is necessary because the Docker Compose cluster may still be
// in JetStream leader election when the program starts.  The attempt count and
// delay are intentionally high for production-like scenarios; test code should
// use [ConnectWithRetryN] with smaller values.
func ConnectWithRetry(url, user, password string, logger *slog.Logger) (*nats.Conn, error) {
	// 10 retries × 2s backoff = up to 20s total wait, enough for a 3-node
	// NATS cluster to finish leader election in any realistic environment.
	return ConnectWithRetryN(url, user, password, 10, 2*time.Second, logger)
}

// ConnectWithRetryN is the underlying parameterised connector used by both
// production code ([ConnectWithRetry]) and tests (where maxRetries=1,
// retryDelay=0 keeps tests fast).
//
// When ctx is cancelled during a retry sleep the function returns immediately
// with the context error rather than waiting out the full retryDelay.
func ConnectWithRetryN(
	url, user, password string,
	maxRetries int,
	retryDelay time.Duration,
	logger *slog.Logger,
) (*nats.Conn, error) {
	return connectWithRetryCtx(context.Background(), url, user, password, maxRetries, retryDelay, logger)
}

// ConnectWithRetryCtx is the context-aware variant used by [Run] so that a
// short RunWithTimeout timeout can interrupt in-progress retries.
func ConnectWithRetryCtx(
	ctx context.Context,
	url, user, password string,
	maxRetries int,
	retryDelay time.Duration,
	logger *slog.Logger,
) (*nats.Conn, error) {
	return connectWithRetryCtx(ctx, url, user, password, maxRetries, retryDelay, logger)
}

// connectWithRetryCtx is the shared implementation that respects ctx for early
// cancellation of the inter-attempt sleep.
func connectWithRetryCtx(
	ctx context.Context,
	url, user, password string,
	maxRetries int,
	retryDelay time.Duration,
	logger *slog.Logger,
) (*nats.Conn, error) {
	var (
		nc  *nats.Conn
		err error
	)
	for attempt := range maxRetries {
		nc, err = nats.Connect(url,
			nats.UserInfo(user, password),
			nats.Timeout(5*time.Second),
			// Reconnect silently during the test (e.g. JetStream re-election).
			nats.MaxReconnects(5),
			nats.ReconnectWait(500*time.Millisecond),
		)
		if err == nil {
			logger.Info("connected to NATS", "user", user, "attempt", attempt+1)
			return nc, nil
		}
		logger.Warn("NATS connection attempt failed",
			"user", user,
			"attempt", attempt+1,
			"max", maxRetries,
			"error", err,
		)
		if retryDelay > 0 {
			// Respect context cancellation during the sleep so a short
			// RunWithTimeout deadline can abort the retry loop early.
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled after attempt %d: %w", attempt+1, ctx.Err())
			case <-time.After(retryDelay):
			}
		}
	}
	return nil, fmt.Errorf("all %d NATS connection attempts failed for user %q: %w", maxRetries, user, err)
}

// GetEnvOrDefault returns the value of the environment variable key, or
// defaultVal when the variable is absent or empty.
func GetEnvOrDefault(key, defaultVal string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return defaultVal
}

package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	cferrors "github.com/jtomasevic/cloud-forge/internal/errors"
	"github.com/jtomasevic/cloud-forge/internal/logging"
)

// PanicRecovery returns an HTTP middleware that recovers from panics in
// downstream handlers, writes a 500 JSON error response, and logs the panic
// with a full goroutine stack trace.
//
// Without this middleware, an unhandled panic in a handler goroutine would
// cause the Go http.Server to silently close the connection, giving the
// client no indication of what went wrong.
//
// The logger parameter should be the service's structured logger (obtained
// from logging.FromContext or passed explicitly) so that the panic log record
// inherits the service and environment attributes.
func PanicRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//nolint:contextcheck // The deferred func correctly closes over r and calls
			// r.Context() at execution time (not capture time). The context captured
			// in the closure is the same request context the handler sees.
			defer func() {
				if rec := recover(); rec != nil {
					// Capture the full stack trace at the point of the panic.
					// debug.Stack() returns the goroutine's stack as a byte slice;
					// converting to string makes it slog-serialisable.
					stack := string(debug.Stack())

					// Convert the recovered value to a standard error so we can
					// pass it to the platform error constructor.
					var cause error
					switch v := rec.(type) {
					case error:
						cause = v
					default:
						cause = fmt.Errorf("panic: %v", v)
					}

					// Retrieve the per-request logger from context so the panic
					// record inherits request_id, trace_id, and span_id attributes
					// set by StructuredLogger. logging.FromContext falls back to a
					// no-op logger when StructuredLogger has not run (e.g. health
					// checks that bypass the full middleware chain).
					reqLogger := logging.FromContext(r.Context())
					if reqLogger == nil {
						reqLogger = logger
					}

					reqLogger.ErrorContext(r.Context(), "panic recovered",
						slog.String("error", cause.Error()),
						slog.String("stack", stack),
					)

					// Write a generic 500 JSON response. We deliberately do NOT
					// include the panic message or stack trace in the response to
					// avoid leaking internal implementation details to callers.
					cferrors.WriteJSON(w, r, cferrors.Internal(cause))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// requestIDHeader is the HTTP header name used to propagate the request ID.
// Clients may send this header to supply their own correlation ID; if absent,
// the middleware generates a new UUID.
const requestIDHeader = "X-Request-ID"

// requestIDContextKey is the context key type for the request ID stored by
// this middleware. Using a dedicated unexported type prevents key collisions
// with other packages that also use context values.
type requestIDContextKey struct{}

// RequestID is an HTTP middleware that ensures every request carries a
// unique X-Request-ID.
//
// Behaviour:
//  1. If the incoming request already has an X-Request-ID header, its value
//     is used as-is (allowing client-side correlation IDs to flow through).
//  2. If the header is absent, a new UUID v4 is generated.
//
// The request ID is:
//   - Stored in the request context (retrieve via [RequestIDFromContext]).
//   - Written to the X-Request-ID response header so clients can correlate
//     their request with the platform's log entries.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the caller supplied their own correlation ID.
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			// Generate a new UUID v4. uuid.NewString() is safe for concurrent
			// use and does not allocate beyond the returned string.
			id = uuid.NewString()
		}

		// Store the ID in the context so downstream handlers and the logger
		// can access it without parsing the response header.
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, id)

		// Echo the request ID in the response header so clients can see it
		// in the response even without access to the platform logs.
		w.Header().Set(requestIDHeader, id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext retrieves the request ID stored in ctx by the
// [RequestID] middleware. Returns an empty string if the middleware has not
// run (e.g. in direct unit tests of handler functions).
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return id
	}
	return ""
}

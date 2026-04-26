// Package errors provides platform-wide error types with HTTP status mapping.
//
// Every CloudForge service uses this package to produce consistent, structured
// error responses. Callers construct errors with the named constructors
// (NotFound, Unauthorized, etc.) and write them to HTTP responses with WriteJSON.
//
// Error codes follow the ALL_CAPS_SNAKE_CASE convention so that API consumers
// can switch on them programmatically without parsing the message string.
//
// Example usage in an HTTP handler:
//
//	func (h *Handler) GetInstance(w http.ResponseWriter, r *http.Request) {
//	    inst, err := h.store.Get(r.Context(), id)
//	    if err != nil {
//	        errors.WriteJSON(w, r, errors.NotFound("database-instance", id))
//	        return
//	    }
//	    // ...
//	}
package errors

import "net/http"

// Error is the canonical platform error type. It satisfies the standard
// error interface and carries enough information to produce both a
// structured HTTP response and a meaningful log entry.
//
// Fields are ordered so that pointer-bearing fields (strings, error interface)
// appear before the plain int, grouping them together and reducing the GC
// scan region from 48 bytes to 40 bytes.
type Error struct {
	// Code is a machine-readable ALL_CAPS_SNAKE_CASE identifier.
	// API consumers should use this to branch on error conditions,
	// never the human-readable Message.
	Code string

	// Cause is the underlying error that triggered this platform error,
	// if any. It is used for logging and unwrapping only — it is never
	// exposed in API responses.
	Cause error

	// Message is a human-readable description of what went wrong.
	// It is safe to return to callers in API responses.
	Message string

	// Status is the HTTP status code that should accompany this error.
	// Placed last so all pointer words are contiguous at the front.
	Status int
}

// errorResponse is the JSON envelope written to HTTP responses.
// It is intentionally unexported; callers use WriteJSON instead.
type errorResponse struct {
	Error errorDetail `json:"error"`
}

// errorDetail carries the fields that appear inside the "error" envelope.
type errorDetail struct {
	// Code matches Error.Code and is the primary field for programmatic use.
	Code string `json:"code"`

	// Message is the human-readable description.
	Message string `json:"message"`

	// RequestID correlates this error response with platform logs.
	// It is read from the X-Request-ID response header that the
	// RequestID middleware sets before calling downstream handlers.
	// When the middleware has not run the field is omitted.
	RequestID string `json:"request_id,omitempty"`
}

// Standard error codes. Services may define additional domain-specific codes
// using the same ALL_CAPS_SNAKE_CASE format.
const (
	CodeNotFound     = "RESOURCE_NOT_FOUND"
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"
	CodeBadRequest   = "BAD_REQUEST"
	CodeInternal     = "INTERNAL_ERROR"
	CodeConflict     = "CONFLICT"
)

// requestIDHeader is the HTTP header name used to propagate the correlation ID.
// This constant mirrors the one in internal/middleware to avoid an import cycle
// while still reading the ID from the same well-known header location.
const requestIDHeader = "X-Request-ID"

// HTTPStatusFor returns the HTTP status code for an *Error.
// If e is nil it returns 200 OK. If e is not an *Error it returns 500.
func HTTPStatusFor(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if e, ok := err.(*Error); ok {
		return e.Status
	}
	return http.StatusInternalServerError
}

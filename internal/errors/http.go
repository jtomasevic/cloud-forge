package errors

import (
	"encoding/json"
	"net/http"
)

// requestIDFromResponse reads the X-Request-ID value that the RequestID
// middleware writes to the response headers before calling downstream handlers.
// By the time WriteJSON is called from a handler, that header is already set.
// Returns an empty string when the middleware has not run (e.g. in tests that
// call handlers directly without the full middleware chain).
func requestIDFromResponse(w http.ResponseWriter) string {
	return w.Header().Get(requestIDHeader)
}

// WriteJSON writes a structured JSON error response to w, setting the
// correct Content-Type and HTTP status code from e.Status.
//
// The X-Request-ID header (set by the RequestID middleware) is read from the
// response writer and included in the body as "request_id" so that callers can
// correlate an error response with platform logs without inspecting headers.
//
// If e is nil, WriteJSON writes 200 OK with an empty body — this handles
// the defensive-nil case so callers do not need to guard every call.
func WriteJSON(w http.ResponseWriter, r *http.Request, e *Error) {
	// Suppress unused parameter warning — r is retained in the signature so
	// callers can pass the request naturally and we can extend this in the
	// future without a breaking API change.
	_ = r

	if e == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	resp := errorResponse{
		Error: errorDetail{
			Code:      e.Code,
			Message:   e.Message,
			RequestID: requestIDFromResponse(w),
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// Disable content sniffing so browsers do not misinterpret error payloads.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	w.WriteHeader(e.Status)

	// Encode directly to the response writer so we never buffer the full
	// response body in memory. Encoding errors are ignored here — if we
	// cannot write the error body there is nothing sensible to do and the
	// HTTP status has already been set.
	_ = json.NewEncoder(w).Encode(resp)
}

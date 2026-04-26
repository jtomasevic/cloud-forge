package middleware

import "net/http"

// responseWriter wraps http.ResponseWriter to capture the HTTP status code
// and the number of bytes written after the underlying handler returns.
// This is used by both [StructuredLogger] and other middleware that need to
// observe the response without modifying it.
type responseWriter struct {
	http.ResponseWriter

	// status is the HTTP status code written by WriteHeader.
	// Defaults to 200 if WriteHeader is never explicitly called.
	status int

	// written is the total number of bytes written to the response body.
	written int
}

// newResponseWriter wraps w and sets the default status to 200 OK.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

// WriteHeader captures the status code before forwarding the call.
func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write counts bytes written while forwarding to the underlying writer.
func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += n
	return n, err
}

// Status returns the captured HTTP status code.
func (rw *responseWriter) Status() int { return rw.status }

// BytesWritten returns the number of bytes written to the response body.
func (rw *responseWriter) BytesWritten() int { return rw.written }

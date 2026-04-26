package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// Error implements the standard error interface so that *Error values can
// be used anywhere a plain error is expected and tested with errors.Is /
// errors.As.
func (e *Error) Error() string {
	if e.Cause != nil {
		// Include the underlying cause so that log lines that print the
		// raw error string still show why the platform error occurred.
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause so that errors.Is and errors.As
// can traverse the error chain across wrapped platform errors.
func (e *Error) Unwrap() error {
	return e.Cause
}

// NotFound returns a 404 platform error indicating that the identified
// resource does not exist.
//
//	resource — the resource type (e.g. "database-instance", "bucket")
//	id       — the identifier that was not found
func NotFound(resource, id string) *Error {
	return &Error{
		Code:    CodeNotFound,
		Message: fmt.Sprintf("%s %q not found", resource, id),
		Status:  http.StatusNotFound,
	}
}

// Unauthorized returns a 401 platform error. Use this when the caller
// has not authenticated (no token, expired token, invalid token).
// For "authenticated but not allowed" errors use Forbidden instead.
func Unauthorized(reason string) *Error {
	return &Error{
		Code:    CodeUnauthorized,
		Message: reason,
		Status:  http.StatusUnauthorized,
	}
}

// Forbidden returns a 403 platform error. Use this when the caller is
// authenticated but their IAM policy does not permit the requested action.
//
//	principal — the identity attempting the action (e.g. "user:alice@example.com")
//	action    — the action being denied  (e.g. "storage:write")
//	resource  — the resource being acted on (e.g. "cf://tenant-1/project-1/buckets/my-bucket")
func Forbidden(principal, action, resource string) *Error {
	return &Error{
		Code:    CodeForbidden,
		Message: fmt.Sprintf("principal %q is not allowed to perform %q on %q", principal, action, resource),
		Status:  http.StatusForbidden,
	}
}

// BadRequest returns a 400 platform error for requests that fail
// input validation before any business logic is reached.
func BadRequest(message string) *Error {
	return &Error{
		Code:    CodeBadRequest,
		Message: message,
		Status:  http.StatusBadRequest,
	}
}

// Internal returns a 500 platform error wrapping an unexpected internal
// failure. The cause is recorded for logging but never exposed in the
// API response — callers only see the generic INTERNAL_ERROR code.
func Internal(cause error) *Error {
	return &Error{
		Code:    CodeInternal,
		Message: "an unexpected internal error occurred",
		Status:  http.StatusInternalServerError,
		Cause:   cause,
	}
}

// Conflict returns a 409 platform error when an operation cannot proceed
// because the target resource already exists or is in a conflicting state.
//
//	resource — the resource type (e.g. "tenant", "bucket")
//	id       — the identifier that conflicted
func Conflict(resource, id string) *Error {
	return &Error{
		Code:    CodeConflict,
		Message: fmt.Sprintf("%s %q already exists", resource, id),
		Status:  http.StatusConflict,
	}
}

// IsNotFound returns true if err is (or wraps) a platform NotFound error.
// This is a convenience wrapper over errors.As for the common case.
func IsNotFound(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == CodeNotFound
	}
	return false
}

// IsForbidden returns true if err is (or wraps) a platform Forbidden error.
func IsForbidden(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == CodeForbidden
	}
	return false
}

// IsUnauthorized returns true if err is (or wraps) a platform Unauthorized error.
func IsUnauthorized(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == CodeUnauthorized
	}
	return false
}

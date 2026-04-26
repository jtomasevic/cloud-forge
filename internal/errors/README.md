# `internal/errors`

Platform-wide error types with HTTP status mapping. Every CloudForge service uses this package to produce consistent, structured JSON error responses with machine-readable error codes.

---

## File layout

| File | Contents |
|------|----------|
| `errors.go` | `Error` struct, JSON response types, `HTTPStatusFor` |
| `constructors.go` | Constructor functions, `IsNotFound` / `IsForbidden` / `IsUnauthorized` |
| `http.go` | `WriteJSON` — writes a structured error response to `http.ResponseWriter` |

---

## Quick start

```go
import cferrors "github.com/jtomasevic/cloud-forge/internal/errors"

func (h *Handler) GetInstance(w http.ResponseWriter, r *http.Request) {
    inst, err := h.store.Get(r.Context(), id)
    if err != nil {
        cferrors.WriteJSON(w, r, cferrors.NotFound("database-instance", id))
        return
    }
    // happy path ...
}
```

---

## Constructors

| Function | HTTP status | Code |
|----------|-------------|------|
| `NotFound(resource, id)` | 404 | `RESOURCE_NOT_FOUND` |
| `Unauthorized(reason)` | 401 | `UNAUTHORIZED` |
| `Forbidden(principal, action, resource)` | 403 | `FORBIDDEN` |
| `BadRequest(message)` | 400 | `BAD_REQUEST` |
| `Conflict(resource, id)` | 409 | `CONFLICT` |
| `Internal(cause)` | 500 | `INTERNAL_ERROR` |

### `Internal` hides the root cause from callers

```go
row, err := db.QueryRow(ctx, query, id)
if err != nil {
    // `cause` is logged internally but never exposed in the API response.
    return cferrors.Internal(err)
}
```

---

## JSON response format

Every error written by `WriteJSON` produces the following envelope:

```json
{
  "error": {
    "code":       "RESOURCE_NOT_FOUND",
    "message":    "database-instance \"my-db\" not found",
    "request_id": "3f7a1b2c-..."
  }
}
```

- `code` — machine-readable identifier; use this to branch in client code, never parse `message`.
- `message` — human-readable description, safe to display in UI.
- `request_id` — correlation ID injected from the request context (set by the `RequestID` middleware). Present only when the middleware chain has run.

---

## Checking error types

```go
// Check a specific type:
if cferrors.IsNotFound(err) {
    // handle not-found
}

// Or use errors.As for full access to the Error struct:
var cfErr *cferrors.Error
if errors.As(err, &cfErr) {
    log.Println(cfErr.Code, cfErr.Status)
}
```

`*Error` implements the standard `error` interface and supports `errors.Is` / `errors.As` unwrapping through the `Cause` field.

---

## HTTP status helper

```go
// Useful in generic middleware that needs the status code without a type assertion:
status := cferrors.HTTPStatusFor(err) // 404, 500, etc. — 200 when err == nil
```

---

## Naming convention

Import this package with the alias `cferrors` to avoid shadowing the standard `errors` package:

```go
import (
    "errors"                                                    // stdlib
    cferrors "github.com/jtomasevic/cloud-forge/internal/errors" // platform
)

if errors.Is(err, sql.ErrNoRows) {
    return cferrors.NotFound("instance", id)
}
```

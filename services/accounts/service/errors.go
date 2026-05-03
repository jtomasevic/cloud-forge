package service

import "errors"

// ErrAccountNotFound is returned when no tenant record exists for the given slug.
// Maps to HTTP 404 Not Found at the REST layer.
var ErrAccountNotFound = errors.New("accounts: account not found")

// ErrAccountAlreadyExists is returned by CreateAccount when a tenant with the
// same slug is already present (LWT CAS rejected the insert).
// Maps to HTTP 409 Conflict at the REST layer.
var ErrAccountAlreadyExists = errors.New("accounts: account slug already exists")

// ErrAccountNotActive is returned by IssueKey when the tenant's status is not
// ACTIVE (e.g. still PROVISIONING or SUSPENDED).
// Maps to HTTP 422 Unprocessable Entity at the REST layer.
var ErrAccountNotActive = errors.New("accounts: account is not in ACTIVE status")

// ErrKeyNotFound is returned by RevokeKey when no API key record exists for
// the given key_id. Maps to HTTP 404 Not Found.
var ErrKeyNotFound = errors.New("accounts: api key not found")

// ErrEmailAlreadyRegistered is returned by Register when a user with the same
// email address already exists. Maps to HTTP 409 Conflict at the REST layer.
var ErrEmailAlreadyRegistered = errors.New("accounts: email already registered")

package accounts

// This file contains unit tests for UserStore that use a fake userQuerier
// (no live ScyllaDB required).  Integration tests with a real container live
// in integration_test.go.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── fake userQuerier ──────────────────────────────────────────────────────────

// fakeUserQuerier is a controlled stub for the CQL session operations used by
// UserStore.  Fields specify what each operation should return.
type fakeUserQuerier struct { //nolint:govet // field alignment; readability preferred
	// execCAS controls
	casApplied bool
	casErr     error

	// execScan controls
	scanErr  error
	scanUser *User // if non-nil, dest fields are populated on a successful scan
}

func (f *fakeUserQuerier) execCAS(_ context.Context, _ string, _ ...interface{}) (bool, error) {
	return f.casApplied, f.casErr
}

func (f *fakeUserQuerier) execScan(_ context.Context, _ string, _ []interface{}, dest ...interface{}) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	// Populate the dest pointers that GetByEmail / GetByID pass in.
	if f.scanUser != nil && len(dest) >= 7 {
		*dest[0].(*gocql.UUID) = gocql.UUID(f.scanUser.UserID)
		*dest[1].(*string) = f.scanUser.Email
		*dest[2].(*string) = f.scanUser.PasswordHash
		*dest[3].(*gocql.UUID) = gocql.UUID(f.scanUser.TenantID)
		*dest[4].(*string) = string(f.scanUser.Status)
		*dest[5].(*time.Time) = f.scanUser.CreatedAt
		*dest[6].(*time.Time) = f.scanUser.UpdatedAt
	}
	return nil
}

// newStoreWithFake creates a UserStore backed by the given fakeUserQuerier.
func newStoreWithFake(f *fakeUserQuerier) *UserStore {
	return &UserStore{db: f, newUUID: uuid.NewRandom}
}

// sampleUser builds a deterministic User for assertions.
func sampleUser() *User {
	uid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	return &User{
		UserID:       uid,
		TenantID:     tid,
		Email:        "alice@example.com",
		PasswordHash: "$2a$10$fakehash",
		Status:       UserStatusActive,
		CreatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestUserStore_Create_Success(t *testing.T) {
	tid := uuid.New()
	store := newStoreWithFake(&fakeUserQuerier{casApplied: true})

	u, err := store.Create(context.Background(), "alice@example.com", "$2a$10$hash", tid)

	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "alice@example.com", u.Email)
	assert.Equal(t, "$2a$10$hash", u.PasswordHash)
	assert.Equal(t, tid, u.TenantID)
	assert.Equal(t, UserStatusActive, u.Status)
	assert.False(t, u.UserID == uuid.Nil, "UserID must be a generated non-nil UUID")
	assert.False(t, u.CreatedAt.IsZero())
}

func TestUserStore_Create_LWT_Rejected_ReturnsErrEmailAlreadyRegistered(t *testing.T) {
	// casApplied=false means the LWT was rejected (email already exists).
	store := newStoreWithFake(&fakeUserQuerier{casApplied: false, casErr: nil})

	_, err := store.Create(context.Background(), "dup@example.com", "hash", uuid.New())

	require.ErrorIs(t, err, ErrEmailAlreadyRegistered)
}

func TestUserStore_Create_CAS_Error_IsWrapped(t *testing.T) {
	dbErr := errors.New("scylladb: write timeout")
	store := newStoreWithFake(&fakeUserQuerier{casApplied: false, casErr: dbErr})

	_, err := store.Create(context.Background(), "fail@example.com", "hash", uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "create user")
}

func TestUserStore_Create_UUIDGenError(t *testing.T) {
	genErr := errors.New("entropy exhausted")
	store := newStoreWithFake(&fakeUserQuerier{})
	store.newUUID = func() (uuid.UUID, error) { return uuid.Nil, genErr }

	_, err := store.Create(context.Background(), "test@example.com", "hash", uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, genErr)
	assert.Contains(t, err.Error(), "generate user_id")
}

// ── GetByEmail ────────────────────────────────────────────────────────────────

func TestUserStore_GetByEmail_Success(t *testing.T) {
	want := sampleUser()
	store := newStoreWithFake(&fakeUserQuerier{scanUser: want})

	got, err := store.GetByEmail(context.Background(), want.Email)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.UserID, got.UserID)
	assert.Equal(t, want.TenantID, got.TenantID)
	assert.Equal(t, want.Email, got.Email)
	assert.Equal(t, want.PasswordHash, got.PasswordHash)
	assert.Equal(t, want.Status, got.Status)
}

func TestUserStore_GetByEmail_NotFound_ReturnsErrUserNotFound(t *testing.T) {
	store := newStoreWithFake(&fakeUserQuerier{scanErr: gocql.ErrNotFound})

	_, err := store.GetByEmail(context.Background(), "ghost@example.com")

	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestUserStore_GetByEmail_DBError_IsWrapped(t *testing.T) {
	dbErr := errors.New("scylladb: read timeout")
	store := newStoreWithFake(&fakeUserQuerier{scanErr: dbErr})

	_, err := store.GetByEmail(context.Background(), "fail@example.com")

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "get user by email")
}

// ── GetByID ───────────────────────────────────────────────────────────────────

func TestUserStore_GetByID_Success(t *testing.T) {
	want := sampleUser()
	store := newStoreWithFake(&fakeUserQuerier{scanUser: want})

	got, err := store.GetByID(context.Background(), want.UserID)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.UserID, got.UserID)
	assert.Equal(t, want.TenantID, got.TenantID)
	assert.Equal(t, want.Email, got.Email)
	assert.Equal(t, want.Status, got.Status)
}

func TestUserStore_GetByID_NotFound_ReturnsErrUserNotFound(t *testing.T) {
	store := newStoreWithFake(&fakeUserQuerier{scanErr: gocql.ErrNotFound})

	_, err := store.GetByID(context.Background(), uuid.New())

	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestUserStore_GetByID_DBError_IsWrapped(t *testing.T) {
	dbErr := errors.New("scylladb: read timeout")
	store := newStoreWithFake(&fakeUserQuerier{scanErr: dbErr})

	_, err := store.GetByID(context.Background(), uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "get user")
}

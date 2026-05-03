package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserStatus_Constants verifies that the UserStatus constants are stable
// and not accidentally changed. The values are stored as TEXT in ScyllaDB —
// changing them would require a data migration.
func TestUserStatus_Constants(t *testing.T) {
	if UserStatusActive != "ACTIVE" {
		t.Errorf("UserStatusActive: got %q, want %q", UserStatusActive, "ACTIVE")
	}
	if UserStatusSuspended != "SUSPENDED" {
		t.Errorf("UserStatusSuspended: got %q, want %q", UserStatusSuspended, "SUSPENDED")
	}
}

// TestErrUserNotFound verifies that the sentinel error string is stable.
func TestErrUserNotFound(t *testing.T) {
	if ErrUserNotFound.Error() != "accounts: user not found" {
		t.Errorf("ErrUserNotFound: got %q", ErrUserNotFound.Error())
	}
}

// TestErrEmailAlreadyRegistered verifies that the sentinel error string is stable.
func TestErrEmailAlreadyRegistered(t *testing.T) {
	if ErrEmailAlreadyRegistered.Error() != "accounts: email already registered" {
		t.Errorf("ErrEmailAlreadyRegistered: got %q", ErrEmailAlreadyRegistered.Error())
	}
}

// TestNewUserStore_ReturnsNonNil verifies that NewUserStore always returns a
// non-nil *UserStore. We pass nil because the store is only used when its
// methods are called; construction itself must not panic.
func TestNewUserStore_ReturnsNonNil(t *testing.T) {
	store := NewUserStore(nil)
	if store == nil {
		t.Fatal("expected non-nil UserStore")
	}
}

// TestUser_ZeroValue verifies that the zero value of User is safe to read
// (no hidden panics on field access). This is a compile-time and runtime
// sanity check rather than a functional assertion.
func TestUser_ZeroValue(_ *testing.T) {
	var u User
	_ = u.UserID.String()
	_ = u.TenantID.String()
	_ = u.Email
	_ = u.PasswordHash
	_ = string(u.Status)
	_ = u.CreatedAt.IsZero()
	_ = u.UpdatedAt.IsZero()
}

// ── UserStore.Create ──────────────────────────────────────────────────────────

func TestUser_Create_Success(t *testing.T) {
	tid := uuid.New()
	store := newStoreWithFake(&fakeUserQuerier{casApplied: true})

	u, err := store.Create(context.Background(), "bob@example.com", "$2a$10$hash", tid)

	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "bob@example.com", u.Email)
	assert.Equal(t, "$2a$10$hash", u.PasswordHash)
	assert.Equal(t, tid, u.TenantID)
	assert.Equal(t, UserStatusActive, u.Status)
	assert.NotEqual(t, uuid.Nil, u.UserID, "UserID must be a generated non-nil UUID")
	assert.False(t, u.CreatedAt.IsZero())
	assert.False(t, u.UpdatedAt.IsZero())
}

func TestUser_Create_LWT_Rejected_ReturnsErrEmailAlreadyRegistered(t *testing.T) {
	store := newStoreWithFake(&fakeUserQuerier{casApplied: false, casErr: nil})

	_, err := store.Create(context.Background(), "dup@example.com", "hash", uuid.New())

	require.ErrorIs(t, err, ErrEmailAlreadyRegistered)
}

func TestUser_Create_CAS_DBError_IsWrapped(t *testing.T) {
	dbErr := errors.New("scylladb: write timeout")
	store := newStoreWithFake(&fakeUserQuerier{casApplied: false, casErr: dbErr})

	_, err := store.Create(context.Background(), "fail@example.com", "hash", uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "create user")
}

func TestUser_Create_UUIDGenError(t *testing.T) {
	genErr := errors.New("entropy exhausted")
	store := newStoreWithFake(&fakeUserQuerier{})
	store.newUUID = func() (uuid.UUID, error) { return uuid.Nil, genErr }

	_, err := store.Create(context.Background(), "test@example.com", "hash", uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, genErr)
	assert.Contains(t, err.Error(), "generate user_id")
}

// ── UserStore.GetByEmail ──────────────────────────────────────────────────────

func TestUser_GetByEmail_Success(t *testing.T) {
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
	assert.Equal(t, want.CreatedAt, got.CreatedAt)
	assert.Equal(t, want.UpdatedAt, got.UpdatedAt)
}

func TestUser_GetByEmail_NotFound_ReturnsErrUserNotFound(t *testing.T) {
	store := newStoreWithFake(&fakeUserQuerier{scanErr: gocql.ErrNotFound})

	_, err := store.GetByEmail(context.Background(), "ghost@example.com")

	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestUser_GetByEmail_DBError_IsWrapped(t *testing.T) {
	dbErr := errors.New("scylladb: read timeout")
	store := newStoreWithFake(&fakeUserQuerier{scanErr: dbErr})

	_, err := store.GetByEmail(context.Background(), "fail@example.com")

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "get user by email")
}

// ── UserStore.GetByID ─────────────────────────────────────────────────────────

func TestUser_GetByID_Success(t *testing.T) {
	want := sampleUser()
	store := newStoreWithFake(&fakeUserQuerier{scanUser: want})

	got, err := store.GetByID(context.Background(), want.UserID)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.UserID, got.UserID)
	assert.Equal(t, want.TenantID, got.TenantID)
	assert.Equal(t, want.Email, got.Email)
	assert.Equal(t, want.PasswordHash, got.PasswordHash)
	assert.Equal(t, want.Status, got.Status)
	assert.Equal(t, want.CreatedAt, got.CreatedAt)
	assert.Equal(t, want.UpdatedAt, got.UpdatedAt)
}

func TestUser_GetByID_NotFound_ReturnsErrUserNotFound(t *testing.T) {
	store := newStoreWithFake(&fakeUserQuerier{scanErr: gocql.ErrNotFound})

	_, err := store.GetByID(context.Background(), uuid.New())

	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestUser_GetByID_DBError_IsWrapped(t *testing.T) {
	dbErr := errors.New("scylladb: read timeout")
	store := newStoreWithFake(&fakeUserQuerier{scanErr: dbErr})

	_, err := store.GetByID(context.Background(), uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "get user")
}

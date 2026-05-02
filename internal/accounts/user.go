package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
)

// UserStatus represents the lifecycle state of a CloudForge human user.
type UserStatus string

const (
	// UserStatusActive means the user can authenticate and manage their tenant.
	UserStatusActive UserStatus = "ACTIVE"

	// UserStatusSuspended means the user's access has been disabled by an admin.
	UserStatusSuspended UserStatus = "SUSPENDED"
)

// User holds the control plane record for a registered human operator.
// It maps 1:1 to a row in cf.users.
//
// Password handling: the password field is intentionally absent from this
// struct.  The raw password is accepted in the registration request, bcrypt-
// hashed at the service layer, and stored in the password_hash column.  The
// hash is read back only during future login operations (not yet implemented).
type User struct {
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Email        string
	PasswordHash string // bcrypt hash — NEVER the raw password
	Status       UserStatus
	UserID       uuid.UUID
	TenantID     uuid.UUID
}

// ErrUserNotFound is returned by UserStore.GetByID / GetByEmail when no
// user record exists for the requested identifier.
var ErrUserNotFound = errors.New("accounts: user not found")

// ErrEmailAlreadyRegistered is returned by UserStore.Create when a user with
// the same email address already exists (LWT CAS rejected the insert).
// Maps to HTTP 409 Conflict at the REST layer.
var ErrEmailAlreadyRegistered = errors.New("accounts: email already registered")

// userQuerier abstracts the CQL session operations needed by UserStore.
// The production implementation wraps *gocql.Session; tests inject a fake.
// Having a thin interface here prevents the test tree from depending on a live
// ScyllaDB while still testing all error branches in Create, GetByEmail,
// and GetByID.
type userQuerier interface {
	// execCAS executes a lightweight-transaction (IF NOT EXISTS / IF …)
	// statement and reports whether the row was applied.
	execCAS(ctx context.Context, stmt string, args ...interface{}) (applied bool, err error)

	// execScan executes a SELECT and scans the first returned row into dest.
	// Must return gocql.ErrNotFound when no rows match.
	execScan(ctx context.Context, stmt string, args []interface{}, dest ...interface{}) error
}

// gocqlUserQuerier adapts a real *gocql.Session to the userQuerier interface.
type gocqlUserQuerier struct{ sess *gocql.Session }

func (g *gocqlUserQuerier) execCAS(ctx context.Context, stmt string, args ...interface{}) (bool, error) {
	dest := make(map[string]interface{})
	return g.sess.Query(stmt, args...).WithContext(ctx).MapScanCAS(dest)
}

func (g *gocqlUserQuerier) execScan(ctx context.Context, stmt string, args []interface{}, dest ...interface{}) error {
	return g.sess.Query(stmt, args...).WithContext(ctx).Scan(dest...)
}

// UserStore provides CRUD access to cf.users and the users_by_email
// materialized view.
type UserStore struct {
	db      userQuerier
	newUUID func() (uuid.UUID, error) // injectable for testing; defaults to uuid.NewRandom
}

// NewUserStore returns a UserStore backed by the given session.
func NewUserStore(sess *gocql.Session) *UserStore {
	return &UserStore{db: &gocqlUserQuerier{sess: sess}, newUUID: uuid.NewRandom}
}

// Create inserts a new user record with status ACTIVE.
//
// It uses an LWT (IF NOT EXISTS) keyed on user_id (UUID) to guard against
// phantom duplicate concurrent inserts. Email uniqueness is enforced by the
// service layer, which queries cf.users_by_email before calling Create; the
// LWT here is a belt-and-suspenders safety net.
//
// The user_id is generated here (UUID v4) and returned inside the User struct.
func (s *UserStore) Create(ctx context.Context, email, passwordHash string, tenantID uuid.UUID) (*User, error) {
	id, err := s.newUUID()
	if err != nil {
		return nil, fmt.Errorf("accounts: generate user_id: %w", err)
	}
	now := time.Now().UTC()
	u := &User{
		UserID:       id,
		Email:        email,
		PasswordHash: passwordHash,
		TenantID:     tenantID,
		Status:       UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	applied, err := s.db.execCAS(ctx, `
		INSERT INTO cf.users
		  (user_id, email, password_hash, tenant_id, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		IF NOT EXISTS`,
		gocql.UUID(id), email, passwordHash, gocql.UUID(tenantID),
		string(u.Status), now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("accounts: create user %q: %w", email, err)
	}
	if !applied {
		// The probability of a UUID v4 collision is negligible; reaching here
		// indicates a genuine duplicate email won the LWT race.
		return nil, ErrEmailAlreadyRegistered
	}
	return u, nil
}

// GetByEmail resolves an email address to its full user record via the
// users_by_email materialized view.
//
// Returns ErrUserNotFound if no user with that email exists.
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	var id, tid gocql.UUID
	var status string
	err := s.db.execScan(ctx,
		`SELECT user_id, email, password_hash, tenant_id, status, created_at, updated_at
		   FROM cf.users_by_email WHERE email = ? LIMIT 1`,
		[]interface{}{email},
		&id, &u.Email, &u.PasswordHash, &tid, &status, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("accounts: get user by email %q: %w", email, err)
	}
	u.UserID = uuid.UUID(id)
	u.TenantID = uuid.UUID(tid)
	u.Status = UserStatus(status)
	return &u, nil
}

// GetByID returns the user record for the given userID.
// Returns ErrUserNotFound if no row exists.
func (s *UserStore) GetByID(ctx context.Context, userID uuid.UUID) (*User, error) {
	var u User
	var id, tid gocql.UUID
	var status string
	err := s.db.execScan(ctx,
		`SELECT user_id, email, password_hash, tenant_id, status, created_at, updated_at
		   FROM cf.users WHERE user_id = ?`,
		[]interface{}{gocql.UUID(userID)},
		&id, &u.Email, &u.PasswordHash, &tid, &status, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("accounts: get user %s: %w", userID, err)
	}
	u.UserID = uuid.UUID(id)
	u.TenantID = uuid.UUID(tid)
	u.Status = UserStatus(status)
	return &u, nil
}

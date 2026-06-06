package tenancy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GetUserByEmail returns the SSO user with the given email, or ErrNotFound.
func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, tenant_id, role, created_at FROM users WHERE email = ?`, email)
	return scanUserRow(row)
}

// CreateUser provisions a new SSO user mapped to a tenant + role.
func (s *SQLiteStore) CreateUser(ctx context.Context, email, tenantID string, role Role) (*User, error) {
	if email == "" {
		return nil, errors.New("user email is required")
	}
	if !role.IsValid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	if _, err := s.GetTenant(ctx, tenantID); err != nil {
		return nil, fmt.Errorf("tenant %s: %w", tenantID, err)
	}
	id, err := randID("usr")
	if err != nil {
		return nil, err
	}
	u := &User{ID: id, Email: email, TenantID: tenantID, Role: role, CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, tenant_id, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.TenantID, string(u.Role), u.CreatedAt.Format(time.RFC3339))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: user email %q", ErrConflict, email)
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return u, nil
}

// CreateSession issues an opaque session token for a user and persists its hash.
func (s *SQLiteStore) CreateSession(ctx context.Context, userID, tenantID string, role Role, ttl time.Duration) (string, *Session, error) {
	token, hash, err := generateSessionToken()
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	sess := &Session{UserID: userID, TenantID: tenantID, Role: role, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, tenant_id, role, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		hash, userID, tenantID, string(role), now.Format(time.RFC3339), sess.ExpiresAt.Format(time.RFC3339))
	if err != nil {
		return "", nil, fmt.Errorf("insert session: %w", err)
	}
	return token, sess, nil
}

// ResolveSession validates a session token and returns the derived Principal.
func (s *SQLiteStore) ResolveSession(ctx context.Context, token string) (*Principal, error) {
	if !strings.HasPrefix(token, sessionTokenPrefix) {
		return nil, ErrInvalidSession
	}
	hash := hashToken(token)
	var userID, tenantID, role, expStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, tenant_id, role, expires_at FROM sessions WHERE token_hash = ?`, hash,
	).Scan(&userID, &tenantID, &role, &expStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidSession
		}
		return nil, err
	}
	if exp, perr := time.Parse(time.RFC3339, expStr); perr != nil || time.Now().After(exp) {
		// Expired (or corrupt expiry): drop it best-effort and reject.
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash)
		return nil, ErrInvalidSession
	}
	return &Principal{TenantID: tenantID, KeyID: userID, Role: Role(role)}, nil
}

// DeleteSession revokes a session by token. Missing tokens are not an error.
func (s *SQLiteStore) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// scanUserRow scans a users row into a *User, mapping no-rows to ErrNotFound.
// Shared by both store backends (the *sql.Row API is identical).
func scanUserRow(row *sql.Row) (*User, error) {
	var u User
	var role, created string
	if err := row.Scan(&u.ID, &u.Email, &u.TenantID, &role, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Role = Role(role)
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &u, nil
}

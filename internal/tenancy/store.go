package tenancy

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/mockagents/mockagents/internal/quota"
	"golang.org/x/crypto/bcrypt"
	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// bcryptCost is the cost factor for every API-key hash in this package, named
// so the choice is reviewable in one place rather than repeated as a magic
// value (F-ST-002). DefaultCost (10) is appropriate: the key secret is 192
// bits of entropy, so bcrypt cost is not the weak link, and the auth hot path
// is latency-sensitive.
const bcryptCost = bcrypt.DefaultCost

// isUniqueViolation reports whether err is a SQLite UNIQUE/PRIMARY KEY
// constraint failure, so callers can return ErrConflict (→ 409) instead of a
// generic internal error. It type-matches the modernc driver's error rather
// than its message text so it is locale- and wrapping-robust.
func isUniqueViolation(err error) bool {
	var se *sqlitedriver.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return true
		}
	}
	return false
}

// Store owns the persistence surface for tenants and API keys.
// Implementation is SQLite-only for v0.1; the Store interface keeps a
// future Postgres swap mechanical.
type Store interface {
	CreateTenant(ctx context.Context, name string) (*Tenant, error)
	GetTenant(ctx context.Context, id string) (*Tenant, error)
	ListTenants(ctx context.Context) ([]*Tenant, error)
	DeleteTenant(ctx context.Context, id string) error

	CreateAPIKey(ctx context.Context, tenantID, name string, role Role) (*NewAPIKeyResult, error)
	ListAPIKeys(ctx context.Context, tenantID string) ([]*APIKey, error)
	// DeleteAPIKey removes a key, scoped to tenantID: a key id that
	// belongs to a different tenant returns ErrNotFound, so the API
	// layer cannot delete another tenant's credential (X-SEC-001).
	DeleteAPIKey(ctx context.Context, tenantID, id string) error
	// RotateAPIKey atomically regenerates the secret behind an
	// existing key. The id, name, role, and tenant_id are preserved
	// so every caller that still holds the key id (audit events,
	// CI configs that reference by id, etc.) keeps working — only
	// the plaintext changes. The returned NewAPIKeyResult carries
	// the previous prefix in its Key.Prefix-less variant so audit
	// logs can correlate a rotation with the specific secret that
	// was burned. The auth cache is flushed on success so cached
	// Principals cannot outlive the old hash.
	// Scoped to tenantID (X-SEC-001): a key id outside that tenant
	// returns ErrNotFound. Self-service callers pass their own
	// Principal.TenantID + Principal.KeyID.
	RotateAPIKey(ctx context.Context, tenantID, id string) (result *NewAPIKeyResult, oldPrefix string, err error)
	// BulkRotateTenantKeys atomically regenerates every key in a tenant
	// inside a single database transaction. Returns one NewAPIKeyResult per
	// key plus a parallel slice of old prefixes so audit logs can correlate
	// each rotation with the specific secret that was burned. A tenant with
	// zero keys is a no-op that returns empty slices (no error). On any
	// per-key failure the transaction is rolled back and NONE of the keys are
	// rotated — the whole point of a bulk operation: all-or-nothing, so an
	// operator responding to a suspected compromise never ends up with a mix
	// of rotated and unrotated credentials. Optional excludeKeyIDs preserve
	// specific keys (e.g. the caller's own credential via ?except=self).
	BulkRotateTenantKeys(ctx context.Context, tenantID string, excludeKeyIDs ...string) (results []*NewAPIKeyResult, oldPrefixes []string, err error)
	// UpdateAPIKeyRole atomically promotes or demotes a key. Returns
	// the previous role alongside the new one so callers (and the
	// audit recorder) can log the transition without a second read.
	// Scoped to tenantID (X-SEC-001): a key id in another tenant
	// returns ErrNotFound rather than being mutated.
	UpdateAPIKeyRole(ctx context.Context, tenantID, id string, role Role) (prev Role, new Role, err error)

	// Resolve looks up an API key by its plaintext value, verifies the
	// bcrypt hash, bumps last_used, and returns the derived Principal.
	Resolve(ctx context.Context, plaintext string) (*Principal, error)

	// --- SSO users + sessions (REF-08 slice D) ---

	// GetUserByEmail returns the user with the given email, or ErrNotFound.
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	// CreateUser provisions a new SSO user mapped to a tenant + role. Returns
	// ErrConflict if the email already exists.
	CreateUser(ctx context.Context, email, tenantID string, role Role) (*User, error)
	// CreateSession issues an opaque session token for a user (valid for ttl)
	// and persists only its hash. Returns the plaintext token once.
	CreateSession(ctx context.Context, userID, tenantID string, role Role, ttl time.Duration) (token string, session *Session, err error)
	// ResolveSession validates a session token (hash lookup + expiry) and
	// returns the derived Principal, or ErrInvalidSession.
	ResolveSession(ctx context.Context, token string) (*Principal, error)
	// DeleteSession revokes a session by its token (logout). A missing token
	// is not an error (idempotent logout).
	DeleteSession(ctx context.Context, token string) error

	// --- Per-tenant quota overrides (REF-08 slice C follow-on) ---

	// GetTenantQuota returns a tenant's persisted quota override, or (nil, nil)
	// when none is set (the enforcer then uses the env defaults).
	GetTenantQuota(ctx context.Context, tenantID string) (*quota.Config, error)
	// SetTenantQuota upserts a tenant's quota override so it survives restarts.
	SetTenantQuota(ctx context.Context, tenantID string, c quota.Config) error

	// AddSpend atomically increments a tenant's spend for the given UTC month
	// and returns the new running total — a shared ledger so the spend cap is
	// accurate across replicas (implements quota.SpendBackend).
	AddSpend(ctx context.Context, tenantID, month string, usd float64) (float64, error)
	// GetSpend returns a tenant's spend total for the month (0 if none).
	GetSpend(ctx context.Context, tenantID, month string) (float64, error)

	Close() error
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS tenants (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS api_keys (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    prefix     TEXT NOT NULL,
    hash       TEXT NOT NULL,
    role       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_used  TEXT
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id  TEXT NOT NULL,
    role       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS tenant_quotas (
    tenant_id         TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rate_per_sec      REAL    NOT NULL DEFAULT 0,
    rate_burst        INTEGER NOT NULL DEFAULT 0,
    monthly_spend_usd REAL    NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tenant_spend (
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    month     TEXT NOT NULL,
    usd       REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, month)
);
`

// SQLiteStore implements Store against a pure-Go SQLite driver.
// authCache is optional and only installed after EnableAuthCache is
// called; the zero-value store behaves exactly like v0.1 and runs
// bcrypt on every Resolve call.
type SQLiteStore struct {
	db    *sql.DB
	cache *authCache
}

// NewSQLiteStore opens or creates the tenancy database at the given path.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// synchronous(normal) under WAL trades a full fsync per commit for the
	// WAL's checkpoint durability — appropriate for API-key metadata, which is
	// not a hard system-of-record, and the win that makes the per-auth
	// last_used bump (and key creates/rotations) cheap on the single
	// connection (PERF-03). Matches the log/audit stores.
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=synchronous(normal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open tenancy db %s: %w", dbPath, err)
	}
	// Single connection: serializes all access (so SELECT-then-UPDATE pairs
	// like UpdateAPIKeyRole are effectively atomic) AND guarantees the
	// foreign_keys(on) DSN pragma is active on the one connection that ever
	// runs DeleteTenant's ON DELETE CASCADE. Raising this without setting the
	// pragma via a connector would silently drop FK enforcement on some
	// connections under modernc/sqlite (F-ST-004).
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(sqliteSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply tenancy schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// EnableAuthCache installs a bounded TTL cache in front of the
// bcrypt compare in Resolve. The cache is keyed on sha256(plaintext)
// so no plaintext is retained, and is flushed whenever any
// mutation (key delete, role change) runs.
//
// Returns the receiver so callers can chain it in NewSQLiteStore
// wiring: `store := (&SQLiteStore{...}).EnableAuthCache(...)`.
// Passing ttl <= 0 uses 5 minutes; maxSize <= 0 uses 1024.
func (s *SQLiteStore) EnableAuthCache(ttl time.Duration, maxSize int) *SQLiteStore {
	s.cache = newAuthCache(ttl, maxSize)
	return s
}

// Close releases the underlying database handle.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// CreateTenant inserts a new tenant row.
func (s *SQLiteStore) CreateTenant(ctx context.Context, name string) (*Tenant, error) {
	if name == "" {
		return nil, errors.New("tenant name is required")
	}
	id, err := randID("ten")
	if err != nil {
		return nil, err
	}
	tenant := &Tenant{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)`,
		tenant.ID, tenant.Name, tenant.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: tenant name %q", ErrConflict, name)
		}
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	return tenant, nil
}

// GetTenant looks up a tenant by id.
func (s *SQLiteStore) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_at FROM tenants WHERE id = ?`, id,
	)
	var t Tenant
	var created string
	if err := row.Scan(&t.ID, &t.Name, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// Timestamp columns are always written by this package in RFC3339, so a
	// parse failure means DB corruption. We tolerate it as the zero time
	// rather than failing the read: these are display-only fields (created_at,
	// last_used), and surfacing them as the zero time is more useful than
	// erroring out the whole lookup. Same policy at the other parse sites
	// (F-ST-005).
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &t, nil
}

// GetTenantByName is a concrete-only bootstrap helper (cmd/mockagents calls it
// on *SQLiteStore, not through the Store interface) for the path where we only
// know the human-readable name. It is deliberately NOT on the Store interface
// (X-TN-003/F-ST-008); a future Postgres impl need not provide it.
func (s *SQLiteStore) GetTenantByName(ctx context.Context, name string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_at FROM tenants WHERE name = ?`, name,
	)
	var t Tenant
	var created string
	if err := row.Scan(&t.ID, &t.Name, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &t, nil
}

// ListTenants returns every tenant ordered by creation time ascending.
func (s *SQLiteStore) ListTenants(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, created_at FROM tenants ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tenant
	for rows.Next() {
		var t Tenant
		var created string
		if err := rows.Scan(&t.ID, &t.Name, &created); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, &t)
	}
	return out, rows.Err()
}

// DeleteTenant removes a tenant and cascades to its api_keys rows.
func (s *SQLiteStore) DeleteTenant(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	// ON DELETE CASCADE wipes the tenant's api_keys rows; flush the
	// auth cache so cached Principals from the deleted tenant cannot
	// authenticate past the TTL.
	s.cache.Invalidate()
	return nil
}

// CreateAPIKey generates a fresh plaintext key, bcrypt-hashes it, stores
// the hash alongside a short non-secret prefix, and returns the plaintext
// exactly once to the caller. The prefix format is `mak_<8hex>`.
func (s *SQLiteStore) CreateAPIKey(ctx context.Context, tenantID, name string, role Role) (*NewAPIKeyResult, error) {
	if !role.IsValid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	// Pre-check the tenant for a clean ErrNotFound. This duplicates the
	// api_keys → tenants FK (which would also reject an unknown tenant), but
	// only when foreign_keys(on) is active — see the MaxOpenConns note in
	// NewSQLiteStore (F-ST-012).
	if _, err := s.GetTenant(ctx, tenantID); err != nil {
		return nil, fmt.Errorf("tenant %s: %w", tenantID, err)
	}
	plaintext, prefix, err := generateAPIKey()
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("bcrypt hash: %w", err)
	}
	keyID, err := randID("key")
	if err != nil {
		return nil, err
	}
	key := APIKey{
		ID:        keyID,
		TenantID:  tenantID,
		Name:      name,
		Prefix:    prefix,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, tenant_id, name, prefix, hash, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.TenantID, key.Name, key.Prefix, string(hash),
		string(key.Role), key.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("insert api_key: %w", err)
	}
	return &NewAPIKeyResult{Key: key, Plaintext: plaintext}, nil
}

// ListAPIKeys returns the metadata for every key belonging to a tenant.
// The hash is deliberately not returned.
func (s *SQLiteStore) ListAPIKeys(ctx context.Context, tenantID string) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, prefix, role, created_at, COALESCE(last_used, '')
		 FROM api_keys WHERE tenant_id = ? ORDER BY created_at ASC`, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		var k APIKey
		var created, lastUsed, role string
		if err := rows.Scan(&k.ID, &k.TenantID, &k.Name, &k.Prefix, &role, &created, &lastUsed); err != nil {
			return nil, err
		}
		k.Role = Role(role)
		k.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if lastUsed != "" {
			if t, err := time.Parse(time.RFC3339, lastUsed); err == nil {
				k.LastUsed = &t
			}
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

// UpdateAPIKeyRole atomically changes an existing key's role. It
// returns ErrNotFound when the key id does not exist and a validation
// error when the requested role is not one of viewer/editor/admin.
// The previous role is returned on success so the caller can log a
// transition (viewer -> admin, etc.) without a second query.
func (s *SQLiteStore) UpdateAPIKeyRole(ctx context.Context, tenantID, id string, role Role) (Role, Role, error) {
	if !role.IsValid() {
		return "", "", fmt.Errorf("invalid role %q", role)
	}
	var prev string
	// The SELECT-then-UPDATE below is a non-atomic read/modify on its own, but
	// MaxOpenConns(1) serializes all store access, so no other statement can
	// interleave between them and `prev` is accurate (F-ST-014).
	// Scope the lookup to tenantID (X-SEC-001): a key in another tenant
	// must look like it doesn't exist, not get mutated.
	err := s.db.QueryRowContext(ctx, `SELECT role FROM api_keys WHERE id = ? AND tenant_id = ?`, id, tenantID).Scan(&prev)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE api_keys SET role = ? WHERE id = ? AND tenant_id = ?`, string(role), id, tenantID); err != nil {
		return "", "", err
	}
	// Flush the auth cache so a previously cached Principal for this
	// key cannot linger with the old role past the change.
	s.cache.Invalidate()
	return Role(prev), role, nil
}

// RotateAPIKey regenerates the secret for an existing key without
// changing its id, name, role, or tenant_id. Returns the new
// plaintext alongside the prefix the old secret was using so the
// caller can emit an audit trail that correlates the rotation with
// the specific compromised credential.
//
// Implementation note: the operation is performed inside a SQLite
// transaction so a crash or context cancellation cannot leave the
// row with a broken hash/prefix pair.
func (s *SQLiteStore) RotateAPIKey(ctx context.Context, callerTenantID, id string) (*NewAPIKeyResult, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort on error path

	// Read existing row so we can preserve the immutable fields and
	// surface the old prefix back to the caller for audit purposes.
	// Scoped to callerTenantID (X-SEC-001): rotating a key in another
	// tenant returns ErrNotFound (→ 404) rather than invalidating it.
	var (
		tenantID    string
		name        string
		oldPrefix   string
		role        string
		createdStr  string
		lastUsedStr sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT tenant_id, name, prefix, role, created_at, last_used
		 FROM api_keys WHERE id = ? AND tenant_id = ?`, id, callerTenantID,
	).Scan(&tenantID, &name, &oldPrefix, &role, &createdStr, &lastUsedStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}

	plaintext, newPrefix, err := generateAPIKey()
	if err != nil {
		return nil, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return nil, "", fmt.Errorf("bcrypt hash: %w", err)
	}
	// Reset last_used — a rotated key has no prior usage of its
	// new plaintext, and preserving the timestamp would confuse
	// "when did this credential last work?" investigations.
	_, err = tx.ExecContext(ctx,
		`UPDATE api_keys SET prefix = ?, hash = ?, last_used = NULL WHERE id = ?`,
		newPrefix, string(hash), id,
	)
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}

	// Flush the auth cache so the old plaintext cannot resolve to
	// a cached Principal after the rotation commits.
	s.cache.Invalidate()

	created, _ := time.Parse(time.RFC3339, createdStr)
	key := APIKey{
		ID:        id,
		TenantID:  tenantID,
		Name:      name,
		Prefix:    newPrefix,
		Role:      Role(role),
		CreatedAt: created,
	}
	return &NewAPIKeyResult{Key: key, Plaintext: plaintext}, oldPrefix, nil
}

// BulkRotateTenantKeys rotates every key belonging to a tenant
// inside a single SQLite transaction. On any per-key error the
// whole transaction is rolled back so callers never end up with a
// partially-rotated tenant (which would be the worst possible
// state after a suspected credential compromise).
//
// The returned slices are parallel: results[i] and oldPrefixes[i]
// describe the same key. Order matches the underlying SELECT's
// ORDER BY created_at ASC so two calls on the same tenant produce
// the same ordering, which helps audit-log correlation.
func (s *SQLiteStore) BulkRotateTenantKeys(ctx context.Context, tenantID string, excludeKeyIDs ...string) ([]*NewAPIKeyResult, []string, error) {
	// Fast-path existence check on the tenant itself so callers get a clean
	// ErrNotFound instead of an empty-result on a typo. In the (tiny,
	// single-conn-serialized) window where the tenant is deleted between here
	// and the SELECT below, that read simply finds zero keys and returns an
	// empty success — harmless, since the tenant and its keys are already gone
	// (F-ST-013).
	if _, err := s.GetTenant(ctx, tenantID); err != nil {
		return nil, nil, err
	}

	// Build the exclude set for the WHERE clause. When non-empty
	// we add a NOT IN filter so the excluded keys survive the
	// rotation unchanged — the caller typically passes their own
	// key id so they don't lock themselves out.
	excludeSet := make(map[string]struct{}, len(excludeKeyIDs))
	for _, kid := range excludeKeyIDs {
		if kid != "" {
			excludeSet[kid] = struct{}{}
		}
	}
	query := `SELECT id, name, prefix, role, created_at
		 FROM api_keys WHERE tenant_id = ?`
	args := []any{tenantID}
	if len(excludeSet) > 0 {
		placeholders := ""
		for kid := range excludeSet {
			if placeholders != "" {
				placeholders += ", "
			}
			placeholders += "?"
			args = append(args, kid)
		}
		query += " AND id NOT IN (" + placeholders + ")"
	}
	query += " ORDER BY created_at ASC"

	// Read the key set OUTSIDE any transaction. This is a plain read, and the
	// per-key bcrypt below must NOT run while the write tx is open (PERF-10):
	// bcrypt.GenerateFromPassword is ~50–100 ms/key, and with the single
	// tenancy connection (MaxOpenConns=1) a 20-key tenant would otherwise pin
	// the connection for >1 s inside the tx, stalling every concurrent auth
	// Resolve. We hash first, then hold the tx only for the cheap UPDATEs.
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	type existing struct {
		id, name, prefix, role, createdStr string
	}
	var existingKeys []existing
	for rows.Next() {
		var e existing
		if err := rows.Scan(&e.id, &e.name, &e.prefix, &e.role, &e.createdStr); err != nil {
			rows.Close()
			return nil, nil, err
		}
		existingKeys = append(existingKeys, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	// Empty tenant (or everything excluded): nothing to rotate, no tx needed.
	if len(existingKeys) == 0 {
		return nil, nil, nil
	}

	// Pre-compute the new plaintext + bcrypt hash for every key BEFORE opening
	// the transaction, so the tx that follows issues only fast UPDATEs.
	type rotation struct {
		e         existing
		newPrefix string
		plaintext string
		hash      []byte
	}
	rotations := make([]rotation, 0, len(existingKeys))
	for _, e := range existingKeys {
		plaintext, newPrefix, genErr := generateAPIKey()
		if genErr != nil {
			return nil, nil, genErr
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
		if hashErr != nil {
			return nil, nil, fmt.Errorf("bcrypt hash: %w", hashErr)
		}
		rotations = append(rotations, rotation{e: e, newPrefix: newPrefix, plaintext: plaintext, hash: hash})
	}

	// Now open the write transaction and apply only the UPDATEs — N cheap row
	// writes, not N bcrypt hashes.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort on error path

	results := make([]*NewAPIKeyResult, 0, len(rotations))
	oldPrefixes := make([]string, 0, len(rotations))
	for _, rot := range rotations {
		res, execErr := tx.ExecContext(ctx,
			`UPDATE api_keys SET prefix = ?, hash = ?, last_used = NULL WHERE id = ?`,
			rot.newPrefix, string(rot.hash), rot.e.id,
		)
		if execErr != nil {
			return nil, nil, execErr
		}
		// A key deleted between the SELECT and here updates zero rows. Skip it so
		// we never hand back a plaintext for a key that no longer exists. The
		// read happened outside the tx, so this narrow race is possible even
		// though the single connection serializes individual statements.
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}

		created, _ := time.Parse(time.RFC3339, rot.e.createdStr)
		results = append(results, &NewAPIKeyResult{
			Key: APIKey{
				ID:        rot.e.id,
				TenantID:  tenantID,
				Name:      rot.e.name,
				Prefix:    rot.newPrefix,
				Role:      Role(rot.e.role),
				CreatedAt: created,
			},
			Plaintext: rot.plaintext,
		})
		oldPrefixes = append(oldPrefixes, rot.e.prefix)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	// Flush the auth cache once at the end — every pre-rotation
	// plaintext is now invalid, and any cached Principal would
	// otherwise linger until its TTL expired.
	s.cache.Invalidate()
	return results, oldPrefixes, nil
}

// DeleteAPIKey permanently removes a key. The auth cache is flushed
// on success so a cached Principal cannot outlive its backing row.
func (s *SQLiteStore) DeleteAPIKey(ctx context.Context, tenantID, id string) error {
	// Scoped to tenantID (X-SEC-001): deleting a key in another tenant
	// affects 0 rows and returns ErrNotFound (→ 404).
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND tenant_id = ?`, id, tenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	s.cache.Invalidate()
	return nil
}

// Resolve looks up the key by prefix (cheap) and then verifies the
// bcrypt hash against the plaintext (expensive). Updates last_used on
// success so operators can spot orphaned keys.
//
// When the authCache is enabled (via EnableAuthCache) a successful
// plaintext lookup inside the TTL window returns the previously
// resolved Principal without touching the database or running
// bcrypt, which is the single largest optimization available on the
// authenticated hot path. Cache misses fall through to the original
// prefix-query path unchanged.
//
// SQLite is configured with a single open connection, so we must fully
// drain the SELECT into memory and close the Rows handle BEFORE running
// the UPDATE — otherwise the UPDATE blocks waiting for the connection
// the iterator is still holding.
func (s *SQLiteStore) Resolve(ctx context.Context, plaintext string) (*Principal, error) {
	// A well-formed key is mak_<8hex>_<secret>; reject anything that can't be
	// one before touching the cache or DB (F-ST-003). len > apiKeyPrefixLen
	// guarantees the index accesses below are in range.
	if len(plaintext) <= apiKeyPrefixLen || plaintext[:4] != "mak_" || plaintext[apiKeyPrefixLen] != '_' {
		return nil, ErrInvalidKey
	}

	// Cache hit — skip bcrypt and the SELECT entirely. last_used is
	// intentionally NOT bumped on cache hits: the tradeoff is that
	// an always-hot key will appear stale in the admin console, but
	// auth latency is the dominant operator concern and the counter
	// self-corrects whenever the cache entry expires.
	if cached := s.cache.Get(plaintext); cached != nil {
		return cached, nil
	}

	prefix := plaintext[:apiKeyPrefixLen]

	type candidate struct {
		id, tenantID, role, hash string
		lastUsed                 sql.NullString
	}
	var candidates []candidate

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, role, hash, last_used FROM api_keys WHERE prefix = ?`, prefix,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.tenantID, &c.role, &c.hash, &c.lastUsed); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close() // release the connection BEFORE running the UPDATE.

	// Timing-oracle defense (X-TN-002): a prefix that matches no row would
	// otherwise return without running bcrypt at all, while a prefix that
	// matches a row runs a (slow) compare — letting an attacker distinguish
	// "a key with this prefix exists" by latency. Run one dummy compare on a
	// no-candidate miss so the prefix-miss path costs ~one bcrypt like the
	// wrong-secret path. (Prefixes are 32 random bits, so collisions are
	// astronomically rare and len(candidates) is 0 or 1 in practice.)
	if len(candidates) == 0 {
		_ = bcrypt.CompareHashAndPassword(timingDummyHash, []byte(plaintext))
		return nil, ErrInvalidKey
	}

	now := time.Now().UTC()
	for _, c := range candidates {
		if bcrypt.CompareHashAndPassword([]byte(c.hash), []byte(plaintext)) == nil {
			// Coarsen the last_used write (PERF-03): only bump when it is stale
			// by more than lastUsedResolution. Under the auth cache, a miss
			// happens at most ~once per TTL per key, but cache evictions and
			// invalidations (any key mutation flushes the whole cache) can
			// produce bursts of misses for a hot key — and with the cache
			// disabled every request is a miss. Coarsening keeps a hot key from
			// fsync-ing a redundant timestamp on every one of those. The write
			// stays best-effort; auth never fails because it couldn't write.
			if shouldBumpLastUsed(c.lastUsed, now) {
				_, _ = s.db.ExecContext(ctx,
					`UPDATE api_keys SET last_used = ? WHERE id = ?`,
					now.Format(time.RFC3339), c.id,
				)
			}
			principal := &Principal{TenantID: c.tenantID, KeyID: c.id, Role: Role(c.role)}
			s.cache.Set(plaintext, principal)
			return principal, nil
		}
	}
	return nil, ErrInvalidKey
}

// lastUsedResolution is how stale last_used may be before Resolve refreshes it.
// last_used exists to spot orphaned keys in the admin console, so minute-level
// accuracy is ample, and it bounds the redundant write rate to once per key per
// minute regardless of request volume (PERF-03).
const lastUsedResolution = time.Minute

// shouldBumpLastUsed reports whether a resolved key's last_used column is stale
// enough to be worth rewriting. An unset or unparseable value is always
// refreshed; otherwise it is rewritten only once per lastUsedResolution window.
func shouldBumpLastUsed(lastUsed sql.NullString, now time.Time) bool {
	if !lastUsed.Valid || lastUsed.String == "" {
		return true
	}
	prev, err := time.Parse(time.RFC3339, lastUsed.String)
	if err != nil {
		return true // corrupt/legacy value — refresh it
	}
	return now.Sub(prev) >= lastUsedResolution
}

// GetTenantQuota returns a tenant's persisted quota override, or (nil, nil)
// when none is set.
func (s *SQLiteStore) GetTenantQuota(ctx context.Context, tenantID string) (*quota.Config, error) {
	var c quota.Config
	err := s.db.QueryRowContext(ctx,
		`SELECT rate_per_sec, rate_burst, monthly_spend_usd FROM tenant_quotas WHERE tenant_id = ?`, tenantID,
	).Scan(&c.RatePerSec, &c.RateBurst, &c.MonthlySpendUSD)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// SetTenantQuota upserts a tenant's quota override.
func (s *SQLiteStore) SetTenantQuota(ctx context.Context, tenantID string, c quota.Config) error {
	if _, err := s.GetTenant(ctx, tenantID); err != nil {
		return fmt.Errorf("tenant %s: %w", tenantID, err)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenant_quotas (tenant_id, rate_per_sec, rate_burst, monthly_spend_usd)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(tenant_id) DO UPDATE SET
		   rate_per_sec = excluded.rate_per_sec,
		   rate_burst = excluded.rate_burst,
		   monthly_spend_usd = excluded.monthly_spend_usd`,
		tenantID, c.RatePerSec, c.RateBurst, c.MonthlySpendUSD)
	return err
}

// AddSpend atomically increments a tenant's monthly spend and returns the new
// total. The single upsert-with-RETURNING is atomic, so concurrent writers
// (replicas sharing a Postgres ledger; the same holds for SQLite's serialized
// connection) never lose an increment.
func (s *SQLiteStore) AddSpend(ctx context.Context, tenantID, month string, usd float64) (float64, error) {
	var total float64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO tenant_spend (tenant_id, month, usd) VALUES (?, ?, ?)
		 ON CONFLICT(tenant_id, month) DO UPDATE SET usd = usd + excluded.usd
		 RETURNING usd`,
		tenantID, month, usd).Scan(&total)
	return total, err
}

// GetSpend returns a tenant's spend total for the month, or 0 when none.
func (s *SQLiteStore) GetSpend(ctx context.Context, tenantID, month string) (float64, error) {
	var total float64
	err := s.db.QueryRowContext(ctx,
		`SELECT usd FROM tenant_spend WHERE tenant_id = ? AND month = ?`, tenantID, month).Scan(&total)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return total, err
}

// --- helpers ---

// apiKeyPrefixLen is the length of an API key's public prefix, `mak_<8hex>`
// (4 + 8 = 12). It doubles as the lookup index and is shared by generateAPIKey
// (which builds it) and Resolve (which slices it off) so the two cannot drift
// (F-ST-015).
const apiKeyPrefixLen = 12

// timingDummyHash is a fixed bcrypt hash used to equalize Resolve's latency on
// a prefix miss (X-TN-002). It is computed once at init; GenerateFromPassword
// only errors on an out-of-range cost, which DefaultCost is not.
var timingDummyHash, _ = bcrypt.GenerateFromPassword([]byte("mockagents-timing-equalizer"), bcryptCost)

// randID produces a short, url-safe identifier with a human-readable prefix.
// It returns the crypto/rand error rather than swallowing it (F-ST-001): a
// failed read would otherwise mint a predictable all-zero id.
func randID(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

// generateAPIKey returns a plaintext key and its public prefix. The
// prefix is 12 chars (`mak_<8hex>`) and doubles as the lookup index.
func generateAPIKey() (plaintext, prefix string, err error) {
	var pfxBytes [4]byte
	if _, err = rand.Read(pfxBytes[:]); err != nil {
		return "", "", err
	}
	prefix = "mak_" + hex.EncodeToString(pfxBytes[:])

	var secret [24]byte
	if _, err = rand.Read(secret[:]); err != nil {
		return "", "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(secret[:])
	plaintext = prefix + "_" + encoded
	return plaintext, prefix, nil
}

package tenancy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/mockagents/mockagents/internal/quota"
	"golang.org/x/crypto/bcrypt"
)

// PostgresStore implements Store against PostgreSQL via pgx's database/sql
// driver (pure-Go, no cgo — the project constraint holds). It is the opt-in
// SaaS backend selected by MOCKAGENTS_TENANCY_DSN; SQLite stays the default.
//
// It is behaviorally identical to SQLiteStore — same schema, same key format,
// same bcrypt + auth-cache + timing-oracle defenses, same tenant-scoped error
// semantics — so the Store conformance suite runs against both. The only
// differences are the SQL dialect ($N placeholders, 23505 unique-violation
// detection) and concurrency: Postgres uses a real connection pool with
// SELECT … FOR UPDATE row locks for read-modify-write, instead of SQLite's
// single-connection serialization.
type PostgresStore struct {
	db    *sql.DB
	cache *authCache
}

// Compile-time assertion that PostgresStore satisfies the Store interface.
var _ Store = (*PostgresStore)(nil)

// Timestamps are stored as RFC3339 TEXT (not TIMESTAMPTZ) to keep the schema
// and scan/parse code identical to SQLiteStore — the tenancy store is tiny, so
// native temporal types buy nothing here and would only fork the two impls.
const postgresSchema = `
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
    rate_per_sec      DOUBLE PRECISION NOT NULL DEFAULT 0,
    rate_burst        INTEGER          NOT NULL DEFAULT 0,
    monthly_spend_usd DOUBLE PRECISION NOT NULL DEFAULT 0
);
`

// isPGUniqueViolation reports whether err is a Postgres unique/PK constraint
// violation (SQLSTATE 23505), the analogue of isUniqueViolation for SQLite, so
// CreateTenant can return ErrConflict (→ 409).
func isPGUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// NewPostgresStore opens the tenancy database at the given DSN, applies the
// schema, and returns a ready store. The DSN is the standard libpq/pgx form,
// e.g. "postgres://user:pass@host:5432/db?sslmode=require".
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open tenancy db: %w", err)
	}
	// A modest pool: tenancy traffic is bcrypt-bound on Resolve and otherwise
	// light. The auth cache absorbs the hot path, so a handful of connections
	// covers bursts of cold resolutions and the occasional management write.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping tenancy db: %w", err)
	}
	if _, err := db.ExecContext(ctx, postgresSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply tenancy schema: %w", err)
	}
	return &PostgresStore{db: db}, nil
}

// EnableAuthCache installs the bounded TTL cache in front of bcrypt in Resolve,
// mirroring SQLiteStore.EnableAuthCache. Returns the receiver for chaining.
func (s *PostgresStore) EnableAuthCache(ttl time.Duration, maxSize int) *PostgresStore {
	s.cache = newAuthCache(ttl, maxSize)
	return s
}

// Close releases the underlying pool.
func (s *PostgresStore) Close() error { return s.db.Close() }

// CreateTenant inserts a new tenant row.
func (s *PostgresStore) CreateTenant(ctx context.Context, name string) (*Tenant, error) {
	if name == "" {
		return nil, errors.New("tenant name is required")
	}
	id, err := randID("ten")
	if err != nil {
		return nil, err
	}
	tenant := &Tenant{ID: id, Name: name, CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, created_at) VALUES ($1, $2, $3)`,
		tenant.ID, tenant.Name, tenant.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isPGUniqueViolation(err) {
			return nil, fmt.Errorf("%w: tenant name %q", ErrConflict, name)
		}
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	return tenant, nil
}

// GetTenant looks up a tenant by id.
func (s *PostgresStore) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM tenants WHERE id = $1`, id)
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
func (s *PostgresStore) ListTenants(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at FROM tenants ORDER BY created_at ASC`)
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
func (s *PostgresStore) DeleteTenant(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.cache.Invalidate()
	return nil
}

// CreateAPIKey generates a fresh plaintext key, bcrypt-hashes it, and stores
// the hash + non-secret prefix, returning the plaintext exactly once.
func (s *PostgresStore) CreateAPIKey(ctx context.Context, tenantID, name string, role Role) (*NewAPIKeyResult, error) {
	if !role.IsValid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}
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
	key := APIKey{ID: keyID, TenantID: tenantID, Name: name, Prefix: prefix, Role: role, CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, tenant_id, name, prefix, hash, role, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		key.ID, key.TenantID, key.Name, key.Prefix, string(hash),
		string(key.Role), key.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("insert api_key: %w", err)
	}
	return &NewAPIKeyResult{Key: key, Plaintext: plaintext}, nil
}

// ListAPIKeys returns metadata for every key belonging to a tenant (no hash).
func (s *PostgresStore) ListAPIKeys(ctx context.Context, tenantID string) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, prefix, role, created_at, COALESCE(last_used, '')
		 FROM api_keys WHERE tenant_id = $1 ORDER BY created_at ASC`, tenantID,
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

// UpdateAPIKeyRole atomically changes a key's role, scoped to tenantID. The row
// is locked with SELECT … FOR UPDATE so a concurrent rotate/delete can't
// interleave between the read and the write.
func (s *PostgresStore) UpdateAPIKeyRole(ctx context.Context, tenantID, id string, role Role) (Role, Role, error) {
	if !role.IsValid() {
		return "", "", fmt.Errorf("invalid role %q", role)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort on error path

	var prev string
	err = tx.QueryRowContext(ctx,
		`SELECT role FROM api_keys WHERE id = $1 AND tenant_id = $2 FOR UPDATE`, id, tenantID,
	).Scan(&prev)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE api_keys SET role = $1 WHERE id = $2 AND tenant_id = $3`, string(role), id, tenantID,
	); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	s.cache.Invalidate()
	return Role(prev), role, nil
}

// RotateAPIKey regenerates the secret for a key without changing its id, name,
// role, or tenant, scoped to callerTenantID. The row is locked for the
// read-modify-write.
func (s *PostgresStore) RotateAPIKey(ctx context.Context, callerTenantID, id string) (*NewAPIKeyResult, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort on error path

	var tenantID, name, oldPrefix, role, createdStr string
	var lastUsedStr sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT tenant_id, name, prefix, role, created_at, last_used
		 FROM api_keys WHERE id = $1 AND tenant_id = $2 FOR UPDATE`, id, callerTenantID,
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
	if _, err = tx.ExecContext(ctx,
		`UPDATE api_keys SET prefix = $1, hash = $2, last_used = NULL WHERE id = $3`,
		newPrefix, string(hash), id,
	); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	s.cache.Invalidate()

	created, _ := time.Parse(time.RFC3339, createdStr)
	key := APIKey{ID: id, TenantID: tenantID, Name: name, Prefix: newPrefix, Role: Role(role), CreatedAt: created}
	return &NewAPIKeyResult{Key: key, Plaintext: plaintext}, oldPrefix, nil
}

// BulkRotateTenantKeys rotates every key in a tenant (minus excludeKeyIDs)
// inside a single transaction — all-or-nothing. bcrypt hashing is done BEFORE
// the transaction opens so the write tx only issues fast UPDATEs (the same
// PERF-10 reasoning as the SQLite impl).
func (s *PostgresStore) BulkRotateTenantKeys(ctx context.Context, tenantID string, excludeKeyIDs ...string) ([]*NewAPIKeyResult, []string, error) {
	if _, err := s.GetTenant(ctx, tenantID); err != nil {
		return nil, nil, err
	}

	excludeSet := make(map[string]struct{}, len(excludeKeyIDs))
	for _, kid := range excludeKeyIDs {
		if kid != "" {
			excludeSet[kid] = struct{}{}
		}
	}
	// $1 = tenant_id; excluded ids start at $2.
	query := `SELECT id, name, prefix, role, created_at FROM api_keys WHERE tenant_id = $1`
	args := []any{tenantID}
	if len(excludeSet) > 0 {
		placeholders := make([]string, 0, len(excludeSet))
		n := 2
		for kid := range excludeSet {
			placeholders = append(placeholders, "$"+strconv.Itoa(n))
			args = append(args, kid)
			n++
		}
		query += " AND id NOT IN (" + strings.Join(placeholders, ", ") + ")"
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	type existing struct{ id, name, prefix, role, createdStr string }
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

	if len(existingKeys) == 0 {
		return nil, nil, nil
	}

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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort on error path

	results := make([]*NewAPIKeyResult, 0, len(rotations))
	oldPrefixes := make([]string, 0, len(rotations))
	for _, rot := range rotations {
		res, execErr := tx.ExecContext(ctx,
			`UPDATE api_keys SET prefix = $1, hash = $2, last_used = NULL WHERE id = $3`,
			rot.newPrefix, string(rot.hash), rot.e.id,
		)
		if execErr != nil {
			return nil, nil, execErr
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // deleted between the read and here; skip
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
	s.cache.Invalidate()
	return results, oldPrefixes, nil
}

// GetTenantQuota returns a tenant's persisted quota override, or (nil, nil)
// when none is set.
func (s *PostgresStore) GetTenantQuota(ctx context.Context, tenantID string) (*quota.Config, error) {
	var c quota.Config
	err := s.db.QueryRowContext(ctx,
		`SELECT rate_per_sec, rate_burst, monthly_spend_usd FROM tenant_quotas WHERE tenant_id = $1`, tenantID,
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
func (s *PostgresStore) SetTenantQuota(ctx context.Context, tenantID string, c quota.Config) error {
	if _, err := s.GetTenant(ctx, tenantID); err != nil {
		return fmt.Errorf("tenant %s: %w", tenantID, err)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenant_quotas (tenant_id, rate_per_sec, rate_burst, monthly_spend_usd)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (tenant_id) DO UPDATE SET
		   rate_per_sec = EXCLUDED.rate_per_sec,
		   rate_burst = EXCLUDED.rate_burst,
		   monthly_spend_usd = EXCLUDED.monthly_spend_usd`,
		tenantID, c.RatePerSec, c.RateBurst, c.MonthlySpendUSD)
	return err
}

// DeleteAPIKey permanently removes a key, scoped to tenantID.
func (s *PostgresStore) DeleteAPIKey(ctx context.Context, tenantID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.cache.Invalidate()
	return nil
}

// Resolve looks up a key by prefix and verifies the bcrypt hash, bumping
// last_used on success. Mirrors SQLiteStore.Resolve including the auth-cache
// short-circuit and the timing-oracle defense.
func (s *PostgresStore) Resolve(ctx context.Context, plaintext string) (*Principal, error) {
	if len(plaintext) <= apiKeyPrefixLen || plaintext[:4] != "mak_" || plaintext[apiKeyPrefixLen] != '_' {
		return nil, ErrInvalidKey
	}
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
		`SELECT id, tenant_id, role, hash, last_used FROM api_keys WHERE prefix = $1`, prefix,
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
	rows.Close()

	// Timing-oracle defense (X-TN-002): equalize the prefix-miss latency with
	// one dummy bcrypt compare.
	if len(candidates) == 0 {
		_ = bcrypt.CompareHashAndPassword(timingDummyHash, []byte(plaintext))
		return nil, ErrInvalidKey
	}

	now := time.Now().UTC()
	for _, c := range candidates {
		if bcrypt.CompareHashAndPassword([]byte(c.hash), []byte(plaintext)) == nil {
			if shouldBumpLastUsed(c.lastUsed, now) {
				_, _ = s.db.ExecContext(ctx,
					`UPDATE api_keys SET last_used = $1 WHERE id = $2`, now.Format(time.RFC3339), c.id,
				)
			}
			principal := &Principal{TenantID: c.tenantID, KeyID: c.id, Role: Role(c.role)}
			s.cache.Set(plaintext, principal)
			return principal, nil
		}
	}
	return nil, ErrInvalidKey
}

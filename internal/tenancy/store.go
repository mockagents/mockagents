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

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

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
	DeleteAPIKey(ctx context.Context, id string) error

	// Resolve looks up an API key by its plaintext value, verifies the
	// bcrypt hash, bumps last_used, and returns the derived Principal.
	Resolve(ctx context.Context, plaintext string) (*Principal, error)

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
`

// SQLiteStore implements Store against a pure-Go SQLite driver.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates the tenancy database at the given path.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open tenancy db %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(sqliteSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply tenancy schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database handle.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// CreateTenant inserts a new tenant row.
func (s *SQLiteStore) CreateTenant(ctx context.Context, name string) (*Tenant, error) {
	if name == "" {
		return nil, errors.New("tenant name is required")
	}
	tenant := &Tenant{
		ID:        randID("ten"),
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)`,
		tenant.ID, tenant.Name, tenant.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
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
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &t, nil
}

// GetTenantByName is a convenience for bootstrap where we only know the
// human-readable name.
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
	return nil
}

// CreateAPIKey generates a fresh plaintext key, bcrypt-hashes it, stores
// the hash alongside a short non-secret prefix, and returns the plaintext
// exactly once to the caller. The prefix format is `mak_<8hex>`.
func (s *SQLiteStore) CreateAPIKey(ctx context.Context, tenantID, name string, role Role) (*NewAPIKeyResult, error) {
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
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("bcrypt hash: %w", err)
	}
	key := APIKey{
		ID:        randID("key"),
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
			k.LastUsed, _ = time.Parse(time.RFC3339, lastUsed)
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

// DeleteAPIKey permanently removes a key.
func (s *SQLiteStore) DeleteAPIKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Resolve looks up the key by prefix (cheap) and then verifies the
// bcrypt hash against the plaintext (expensive). Updates last_used on
// success so operators can spot orphaned keys.
//
// SQLite is configured with a single open connection, so we must fully
// drain the SELECT into memory and close the Rows handle BEFORE running
// the UPDATE — otherwise the UPDATE blocks waiting for the connection
// the iterator is still holding.
func (s *SQLiteStore) Resolve(ctx context.Context, plaintext string) (*Principal, error) {
	if len(plaintext) < 13 { // "mak_" + at least 9 chars
		return nil, ErrInvalidKey
	}
	prefix := plaintext[:12]

	type candidate struct {
		id, tenantID, role, hash string
	}
	var candidates []candidate

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, role, hash FROM api_keys WHERE prefix = ?`, prefix,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.tenantID, &c.role, &c.hash); err != nil {
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

	for _, c := range candidates {
		if bcrypt.CompareHashAndPassword([]byte(c.hash), []byte(plaintext)) == nil {
			// Best-effort timestamp bump; ignore errors — auth should
			// not fail because we couldn't write last_used.
			_, _ = s.db.ExecContext(ctx,
				`UPDATE api_keys SET last_used = ? WHERE id = ?`,
				time.Now().UTC().Format(time.RFC3339), c.id,
			)
			return &Principal{TenantID: c.tenantID, KeyID: c.id, Role: Role(c.role)}, nil
		}
	}
	return nil, ErrInvalidKey
}

// --- helpers ---

// randID produces a short, url-safe identifier with a human-readable prefix.
func randID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
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

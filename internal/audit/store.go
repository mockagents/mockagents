package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS audit_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp   TEXT NOT NULL,
    kind        TEXT NOT NULL,
    actor_name  TEXT NOT NULL DEFAULT 'anonymous',
    actor_tenant TEXT NOT NULL DEFAULT '',
    actor_key   TEXT NOT NULL DEFAULT '',
    actor_role  TEXT NOT NULL DEFAULT '',
    actor_ip    TEXT NOT NULL DEFAULT '',
    target      TEXT NOT NULL DEFAULT '',
    details     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_audit_kind      ON audit_events(kind);
CREATE INDEX IF NOT EXISTS idx_audit_actor     ON audit_events(actor_name);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_events(timestamp);
`

// Store is the append-only audit persistence layer. All methods are
// safe for concurrent use.
type Store interface {
	Append(ctx context.Context, e *Event) error
	List(ctx context.Context, q Query) ([]*Event, error)
	Get(ctx context.Context, id int64) (*Event, error)
	Close() error
}

// SQLiteStore is the pure-Go SQLite-backed Store used by default.
// Schema is applied on Open, so simply pointing at a fresh file is
// enough to get a working store.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates the audit database at the given
// path and applies the schema.
//
// Pool sizing matches the interaction log store (MaxOpenConns=8) on
// top of WAL + synchronous=NORMAL. This lets auth-denial bursts
// (which can surge during credential-stuffing probes) append in
// parallel with admin read queries instead of serializing behind a
// single connection. SQLite itself still serializes the write
// transaction under WAL, so concurrent Append callers effectively
// queue at the database layer — the multi-conn pool just prevents
// reads from blocking on in-flight writes and vice versa.
//
// Unlike the tenancy store, the audit store never holds a Rows
// iterator open across a second query (see the comment on
// tenancy.SQLiteStore for why that matters), so raising the pool
// here is safe.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	dsn := path + "?_pragma=journal_mode(wal)&_pragma=synchronous(normal)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open audit db %s: %w", path, err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply audit schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database handle.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// Append inserts a new audit event and assigns its ID from the
// generated rowid. Timestamp defaults to time.Now().UTC() when zero.
func (s *SQLiteStore) Append(ctx context.Context, e *Event) error {
	if e == nil {
		return fmt.Errorf("audit: nil event")
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("audit: invalid event kind %q", e.Kind)
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	// Normalize to UTC unconditionally. Lexical comparison of
	// RFC3339Nano strings only corresponds to chronological order
	// when every value shares the same timezone offset — storing
	// everything in UTC guarantees the Since filter works.
	e.Timestamp = e.Timestamp.UTC()
	if e.Actor.Name == "" {
		e.Actor.Name = "anonymous"
	}

	// No Go-level mutex: database/sql + SQLite's own write
	// serialization under WAL already provide the correctness we
	// need, and holding a mutex across ExecContext would re-serialize
	// everything the new pool just enabled to run in parallel.
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events
		 (timestamp, kind, actor_name, actor_tenant, actor_key, actor_role, actor_ip, target, details)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Timestamp.Format(time.RFC3339Nano),
		string(e.Kind),
		e.Actor.Name,
		e.Actor.TenantID,
		e.Actor.KeyID,
		e.Actor.Role,
		e.Actor.RemoteIP,
		e.Target,
		e.Details,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	e.ID = id
	return nil
}

// Get returns a single event by id.
func (s *SQLiteStore) Get(ctx context.Context, id int64) (*Event, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, timestamp, kind, actor_name, actor_tenant, actor_key,
		        actor_role, actor_ip, target, details
		 FROM audit_events WHERE id = ?`, id)
	e, err := scanEvent(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return e, nil
}

// List returns events matching the query, newest first. The limit is
// defaulted to 100 when unset and clamped to 1000 to protect against
// accidental pagination bombs.
func (s *SQLiteStore) List(ctx context.Context, q Query) ([]*Event, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	var (
		where []string
		args  []any
	)
	if q.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, string(q.Kind))
	}
	if q.Actor != "" {
		where = append(where, "actor_name = ?")
		args = append(args, q.Actor)
	}
	if q.ActorTenant != "" {
		where = append(where, "actor_tenant = ?")
		args = append(args, q.ActorTenant)
	}
	if !q.Since.IsZero() {
		where = append(where, "timestamp >= ?")
		args = append(args, q.Since.UTC().Format(time.RFC3339Nano))
	}

	query := `SELECT id, timestamp, kind, actor_name, actor_tenant, actor_key,
	                actor_role, actor_ip, target, details
	          FROM audit_events`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// scanEvent is shared by Get and List; generic over QueryRow and Rows
// by taking the Scan closure directly.
func scanEvent(scan func(dest ...any) error) (*Event, error) {
	var (
		e      Event
		tsStr  string
	)
	err := scan(
		&e.ID,
		&tsStr,
		&e.Kind,
		&e.Actor.Name,
		&e.Actor.TenantID,
		&e.Actor.KeyID,
		&e.Actor.Role,
		&e.Actor.RemoteIP,
		&e.Target,
		&e.Details,
	)
	if err != nil {
		return nil, err
	}
	ts, perr := time.Parse(time.RFC3339Nano, tsStr)
	if perr != nil {
		// Fall back to the original RFC3339 shape we used earlier.
		ts, _ = time.Parse(time.RFC3339, tsStr)
	}
	e.Timestamp = ts
	return &e, nil
}

// MarshalDetails is a helper for call sites that want to attach
// structured context to an event. Returns an empty string when v is
// nil so Append doesn't persist a pointless "null" literal.
func MarshalDetails(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

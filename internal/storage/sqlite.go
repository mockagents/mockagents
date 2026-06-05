package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Connection-pool sizing for the interaction log store. WAL mode lets
// readers run concurrently with each other and with a single writer,
// so raising MaxOpenConns unblocks parallel GET /api/v1/logs calls
// while the bounded log worker pool is appending rows in the
// background. Eight connections is a conservative ceiling — modest
// enough that file-handle and goroutine pressure stays bounded on
// constrained hosts, generous enough that a typical dashboard/SDK
// mix does not queue behind a single connection.
const (
	maxOpenConns = 8
	maxIdleConns = 8
)

const schema = `
CREATE TABLE IF NOT EXISTS interaction_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT    NOT NULL DEFAULT (datetime('now')),
    tenant_id       TEXT    NOT NULL DEFAULT '',
    agent_name      TEXT    NOT NULL,
    session_id      TEXT    NOT NULL DEFAULT '',
    protocol        TEXT    NOT NULL DEFAULT '',
    request_method  TEXT    NOT NULL DEFAULT 'POST',
    request_path    TEXT    NOT NULL DEFAULT '',
    request_body    TEXT,
    response_status INTEGER NOT NULL DEFAULT 200,
    response_body   TEXT,
    latency_ms      INTEGER NOT NULL DEFAULT 0,
    tool_calls_count INTEGER DEFAULT 0,
    streaming       INTEGER DEFAULT 0,
    error           TEXT,
    scenario_name   TEXT    DEFAULT '',
    truncated       INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_logs_agent     ON interaction_logs(agent_name);
CREATE INDEX IF NOT EXISTS idx_logs_session   ON interaction_logs(session_id);
CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON interaction_logs(timestamp);
`

// SQLiteStore manages SQLite-backed interaction log storage.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates a SQLite database at the given path.
//
// The DSN configures the standard WAL-mode tuning for log-class
// workloads:
//
//   - journal_mode=WAL   — concurrent readers + single writer without
//     blocking each other.
//   - synchronous=NORMAL — standard WAL pairing; durable against
//     process crashes, can lose the last ~millisecond of writes on a
//     hard power-off. Acceptable for interaction logs, which are a
//     debugging aid rather than a system of record.
//   - busy_timeout=5000  — lock-contention backoff in ms.
//
// Connection-pool sizing (MaxOpenConns=8) was raised from the v0.1
// default of 1 so GET /api/v1/logs, GET /api/v1/costs, and the async
// log worker can all make progress in parallel. Note that the
// tenancy store still deliberately uses MaxOpenConns=1 for a
// different reason (the `Resolve` path holds a Rows iterator open
// while issuing an UPDATE on the same connection; see
// internal/tenancy/store.go for the inline comment explaining that
// constraint). Do not copy that pattern here.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=synchronous(normal)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dbPath, err)
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)

	// Apply schema.
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	hasTenant, err := columnExists(db, "interaction_logs", "tenant_id")
	if err != nil {
		return err
	}
	if !hasTenant {
		if _, err := db.Exec(`ALTER TABLE interaction_logs ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	// truncated landed with the log-fidelity slice (REF-04). Additive,
	// defaulted to 0, so pre-existing rows read back as "not truncated".
	hasTruncated, err := columnExists(db, "interaction_logs", "truncated")
	if err != nil {
		return err
	}
	if !hasTruncated {
		if _, err := db.Exec(`ALTER TABLE interaction_logs ADD COLUMN truncated INTEGER DEFAULT 0`); err != nil {
			return err
		}
	}
	// Composite (tenant_id, id DESC) index for the tenant-scoped dashboard query
	// `WHERE tenant_id = ? ORDER BY id DESC LIMIT ?`: it serves both the equality
	// filter and the id-desc ordering from one index, so SQLite skips a separate
	// sort and the LIMIT short-circuits (PERF-13).
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_logs_tenant_id ON interaction_logs(tenant_id, id DESC)`); err != nil {
		return err
	}
	// The old single-column tenant index is now redundant — the composite's
	// leftmost column serves the same `WHERE tenant_id = ?` equality (incl.
	// DeleteForTenant) — so drop it to avoid the extra per-insert write cost.
	_, err = db.Exec(`DROP INDEX IF EXISTS idx_logs_tenant`)
	return err
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Log inserts a single interaction log record.
func (s *SQLiteStore) Log(ctx context.Context, entry *InteractionLog) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO interaction_logs
			(timestamp, tenant_id, agent_name, session_id, protocol, request_method, request_path,
			 request_body, response_status, response_body, latency_ms,
			 tool_calls_count, streaming, error, scenario_name, truncated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.Timestamp, entry.TenantID, entry.AgentName, entry.SessionID, entry.Protocol,
		entry.RequestMethod, entry.RequestPath,
		entry.RequestBody, entry.ResponseStatus, entry.ResponseBody,
		entry.LatencyMs, entry.ToolCallsCount, boolToInt(entry.Streaming),
		entry.Error, entry.ScenarioName, boolToInt(entry.Truncated),
	)
	if err != nil {
		return err
	}
	// Populate the ID on the passed entry so async consumers
	// (LogBroadcaster subscribers, for example) can surface a clickable
	// /logs/{id} link to their clients. LastInsertId failures are not
	// fatal — the row is already persisted.
	if id, lerr := res.LastInsertId(); lerr == nil {
		entry.ID = id
	}
	return nil
}

// Query retrieves interaction logs matching the given filter.
func (s *SQLiteStore) Query(ctx context.Context, filter InteractionFilter) ([]InteractionLog, error) {
	query := `SELECT id, timestamp, tenant_id, agent_name, session_id, protocol,
		request_method, request_path, request_body, response_status,
		response_body, latency_ms, tool_calls_count, streaming, error, scenario_name, truncated
		FROM interaction_logs WHERE 1=1`

	var args []any

	if filter.FilterTenantID {
		query += " AND tenant_id = ?"
		args = append(args, filter.TenantID)
	}
	if filter.AgentName != "" {
		query += " AND agent_name = ?"
		args = append(args, filter.AgentName)
	}
	if filter.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, filter.SessionID)
	}
	if filter.Since != "" {
		query += " AND timestamp >= ?"
		args = append(args, filter.Since)
	}
	if filter.Until != "" {
		query += " AND timestamp <= ?"
		args = append(args, filter.Until)
	}

	query += " ORDER BY id DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	// Pre-size to the page limit so a full page fills without growslice churn
	// (PERF-20). limit is already clamped to [1, MaxLimit] above.
	logs := make([]InteractionLog, 0, limit)
	for rows.Next() {
		var log InteractionLog
		var streaming, truncated int
		var errStr, reqBody, respBody, scenarioName sql.NullString
		if err := rows.Scan(
			&log.ID, &log.Timestamp, &log.TenantID, &log.AgentName, &log.SessionID,
			&log.Protocol, &log.RequestMethod, &log.RequestPath,
			&reqBody, &log.ResponseStatus,
			&respBody, &log.LatencyMs, &log.ToolCallsCount,
			&streaming, &errStr, &scenarioName, &truncated,
		); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		log.Streaming = streaming != 0
		log.Truncated = truncated != 0
		log.Error = errStr.String
		log.RequestBody = reqBody.String
		log.ResponseBody = respBody.String
		log.ScenarioName = scenarioName.String
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// GetByID retrieves a single interaction log by its ID.
func (s *SQLiteStore) GetByID(ctx context.Context, id int64) (*InteractionLog, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, timestamp, tenant_id, agent_name, session_id, protocol,
			request_method, request_path, request_body, response_status,
			response_body, latency_ms, tool_calls_count, streaming, error, scenario_name, truncated
		FROM interaction_logs WHERE id = ?`, id)

	var log InteractionLog
	var streaming, truncated int
	var errStr, reqBody, respBody, scenarioName sql.NullString
	if err := row.Scan(
		&log.ID, &log.Timestamp, &log.TenantID, &log.AgentName, &log.SessionID,
		&log.Protocol, &log.RequestMethod, &log.RequestPath,
		&reqBody, &log.ResponseStatus,
		&respBody, &log.LatencyMs, &log.ToolCallsCount,
		&streaming, &errStr, &scenarioName, &truncated,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting log %d: %w", id, err)
	}
	log.Streaming = streaming != 0
	log.Truncated = truncated != 0
	log.Error = errStr.String
	log.RequestBody = reqBody.String
	log.ResponseBody = respBody.String
	log.ScenarioName = scenarioName.String
	return &log, nil
}

// DeleteAll removes all interaction logs. Returns the number of deleted rows.
func (s *SQLiteStore) DeleteAll(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM interaction_logs")
	if err != nil {
		return 0, fmt.Errorf("deleting logs: %w", err)
	}
	return result.RowsAffected()
}

// DeleteForTenant removes interaction logs owned by tenantID. Returns
// the number of deleted rows.
func (s *SQLiteStore) DeleteForTenant(ctx context.Context, tenantID string) (int64, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM interaction_logs WHERE tenant_id = ?", tenantID)
	if err != nil {
		return 0, fmt.Errorf("deleting logs for tenant: %w", err)
	}
	return result.RowsAffected()
}

// Count returns the total number of interaction logs.
func (s *SQLiteStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM interaction_logs").Scan(&count)
	return count, err
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// DatabaseSize returns approximate database size by querying page_count * page_size.
func (s *SQLiteStore) DatabaseSize(ctx context.Context) (int64, error) {
	var pageCount, pageSize int64
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return 0, err
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, err
	}
	return pageCount * pageSize, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// PruneToMaxRows enforces a retention bound on the interaction-log table by
// deleting the oldest rows beyond the newest `max` (SEC-05). max <= 0 is a no-op
// (unlimited retention). Returns the number of rows deleted.
//
// The subquery finds the id of the (max+1)-th newest row; every row at or below
// it is older than the retained window and is removed. When the table holds <=
// max rows the OFFSET lands past the end, the subquery is NULL, and `id <= NULL`
// matches nothing — so nothing is deleted.
func (s *SQLiteStore) PruneToMaxRows(ctx context.Context, max int) (int64, error) {
	if max <= 0 {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM interaction_logs WHERE id <= (
			SELECT id FROM interaction_logs ORDER BY id DESC LIMIT 1 OFFSET ?
		)`, max)
	if err != nil {
		return 0, fmt.Errorf("pruning interaction logs: %w", err)
	}
	return res.RowsAffected()
}

// TruncateBody returns a truncated version of a body string for summaries.
func TruncateBody(body string, maxLen int) string {
	if len(body) <= maxLen {
		return body
	}
	return body[:maxLen] + "..."
}

// SanitizeBody removes sensitive fields from request bodies before logging.
func SanitizeBody(body string) string {
	// Replace common API key patterns.
	sanitized := body
	for _, pattern := range []string{"sk-", "key-", "Bearer "} {
		if idx := strings.Index(sanitized, pattern); idx >= 0 {
			end := idx + len(pattern)
			for end < len(sanitized) && sanitized[end] != '"' && sanitized[end] != ' ' && sanitized[end] != ',' {
				end++
			}
			sanitized = sanitized[:idx+len(pattern)] + "***" + sanitized[end:]
		}
	}
	return sanitized
}

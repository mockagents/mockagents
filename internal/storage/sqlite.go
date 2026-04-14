package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS interaction_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT    NOT NULL DEFAULT (datetime('now')),
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
    scenario_name   TEXT    DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_logs_agent     ON interaction_logs(agent_name);
CREATE INDEX IF NOT EXISTS idx_logs_session   ON interaction_logs(session_id);
CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON interaction_logs(timestamp);
`

// SQLiteStore manages SQLite-backed interaction log storage.
type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex // protects batch buffer
	buf []InteractionLog
}

// NewSQLiteStore opens or creates a SQLite database at the given path.
// Uses WAL mode for concurrent read/write safety.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dbPath, err)
	}

	// Limit connections for SQLite.
	db.SetMaxOpenConns(1)

	// Apply schema.
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Log inserts a single interaction log record.
func (s *SQLiteStore) Log(ctx context.Context, entry *InteractionLog) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO interaction_logs
			(timestamp, agent_name, session_id, protocol, request_method, request_path,
			 request_body, response_status, response_body, latency_ms,
			 tool_calls_count, streaming, error, scenario_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.Timestamp, entry.AgentName, entry.SessionID, entry.Protocol,
		entry.RequestMethod, entry.RequestPath,
		entry.RequestBody, entry.ResponseStatus, entry.ResponseBody,
		entry.LatencyMs, entry.ToolCallsCount, boolToInt(entry.Streaming),
		entry.Error, entry.ScenarioName,
	)
	return err
}

// Query retrieves interaction logs matching the given filter.
func (s *SQLiteStore) Query(ctx context.Context, filter InteractionFilter) ([]InteractionLog, error) {
	query := `SELECT id, timestamp, agent_name, session_id, protocol,
		request_method, request_path, request_body, response_status,
		response_body, latency_ms, tool_calls_count, streaming, error, scenario_name
		FROM interaction_logs WHERE 1=1`

	var args []any

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

	var logs []InteractionLog
	for rows.Next() {
		var log InteractionLog
		var streaming int
		var errStr, reqBody, respBody, scenarioName sql.NullString
		if err := rows.Scan(
			&log.ID, &log.Timestamp, &log.AgentName, &log.SessionID,
			&log.Protocol, &log.RequestMethod, &log.RequestPath,
			&reqBody, &log.ResponseStatus,
			&respBody, &log.LatencyMs, &log.ToolCallsCount,
			&streaming, &errStr, &scenarioName,
		); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		log.Streaming = streaming != 0
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
		SELECT id, timestamp, agent_name, session_id, protocol,
			request_method, request_path, request_body, response_status,
			response_body, latency_ms, tool_calls_count, streaming, error, scenario_name
		FROM interaction_logs WHERE id = ?`, id)

	var log InteractionLog
	var streaming int
	var errStr, reqBody, respBody, scenarioName sql.NullString
	if err := row.Scan(
		&log.ID, &log.Timestamp, &log.AgentName, &log.SessionID,
		&log.Protocol, &log.RequestMethod, &log.RequestPath,
		&reqBody, &log.ResponseStatus,
		&respBody, &log.LatencyMs, &log.ToolCallsCount,
		&streaming, &errStr, &scenarioName,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting log %d: %w", id, err)
	}
	log.Streaming = streaming != 0
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

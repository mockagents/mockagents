package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func sampleLog(agent, session string) *InteractionLog {
	return &InteractionLog{
		Timestamp:      "2026-04-07T14:00:00Z",
		AgentName:      agent,
		SessionID:      session,
		Protocol:       "openai-chat-completions",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		RequestBody:    `{"model":"gpt-4o"}`,
		ResponseStatus: 200,
		ResponseBody:   `{"content":"hello"}`,
		LatencyMs:      42,
		ToolCallsCount: 0,
		Streaming:      false,
		ScenarioName:   "greeting",
	}
}

func TestSQLiteStore_CreateAndClose(t *testing.T) {
	store := testStore(t)
	assert.NotNil(t, store)
}

func TestSQLiteStore_MigratesTenantColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE interaction_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL DEFAULT (datetime('now')),
			agent_name TEXT NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			protocol TEXT NOT NULL DEFAULT '',
			request_method TEXT NOT NULL DEFAULT 'POST',
			request_path TEXT NOT NULL DEFAULT '',
			request_body TEXT,
			response_status INTEGER NOT NULL DEFAULT 200,
			response_body TEXT,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			tool_calls_count INTEGER DEFAULT 0,
			streaming INTEGER DEFAULT 0,
			error TEXT,
			scenario_name TEXT DEFAULT ''
		)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ok, err := columnExists(store.db, "interaction_logs", "tenant_id")
	require.NoError(t, err)
	assert.True(t, ok)

	// The same legacy table predates the truncated column too; migrate
	// must add it additively so inserts/queries against an existing DB work.
	ok, err = columnExists(store.db, "interaction_logs", "truncated")
	require.NoError(t, err)
	assert.True(t, ok, "truncated column should be added by migrate")

	// A round-trip through the migrated DB confirms the new column reads back.
	require.NoError(t, store.Log(context.Background(), sampleLog("agent-a", "sess-1")))
	logs, err := store.Query(context.Background(), InteractionFilter{})
	require.NoError(t, err)
	require.Len(t, logs, 1)
}

func TestSQLiteStore_TruncatedRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	entry := sampleLog("agent-a", "sess-1")
	entry.Truncated = true
	require.NoError(t, store.Log(ctx, entry))

	// Via Query.
	logs, err := store.Query(ctx, InteractionFilter{})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.True(t, logs[0].Truncated, "Query should read back Truncated=true")

	// Via GetByID.
	got, err := store.GetByID(ctx, logs[0].ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Truncated, "GetByID should read back Truncated=true")

	// Default (unset) round-trips as false.
	require.NoError(t, store.Log(ctx, sampleLog("agent-b", "sess-2")))
	logs, err = store.Query(ctx, InteractionFilter{AgentName: "agent-b"})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.False(t, logs[0].Truncated)
}

func TestSQLiteStore_LogAndQuery(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	err := store.Log(ctx, sampleLog("agent-a", "sess-1"))
	require.NoError(t, err)

	logs, err := store.Query(ctx, InteractionFilter{})
	require.NoError(t, err)
	require.Len(t, logs, 1)

	assert.Equal(t, "agent-a", logs[0].AgentName)
	assert.Equal(t, "sess-1", logs[0].SessionID)
	assert.Equal(t, "openai-chat-completions", logs[0].Protocol)
	assert.Equal(t, 200, logs[0].ResponseStatus)
	assert.Equal(t, int64(42), logs[0].LatencyMs)
	assert.Equal(t, "greeting", logs[0].ScenarioName)
}

func TestSQLiteStore_MultipleLogs(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	}

	logs, err := store.Query(ctx, InteractionFilter{})
	require.NoError(t, err)
	assert.Len(t, logs, 5)
}

func TestSQLiteStore_QueryByAgent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	require.NoError(t, store.Log(ctx, sampleLog("agent-b", "sess-2")))
	require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-3")))

	logs, err := store.Query(ctx, InteractionFilter{AgentName: "agent-a"})
	require.NoError(t, err)
	assert.Len(t, logs, 2)
	for _, log := range logs {
		assert.Equal(t, "agent-a", log.AgentName)
	}
}

func TestSQLiteStore_QueryBySession(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-2")))

	logs, err := store.Query(ctx, InteractionFilter{SessionID: "sess-1"})
	require.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.Equal(t, "sess-1", logs[0].SessionID)
}

func TestSQLiteStore_QueryByTenant(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	a := sampleLog("agent-a", "sess-1")
	a.TenantID = "ten-a"
	b := sampleLog("agent-b", "sess-2")
	b.TenantID = "ten-b"
	require.NoError(t, store.Log(ctx, a))
	require.NoError(t, store.Log(ctx, b))

	logs, err := store.Query(ctx, InteractionFilter{TenantID: "ten-a", FilterTenantID: true})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "ten-a", logs[0].TenantID)
	assert.Equal(t, "agent-a", logs[0].AgentName)
}

func TestSQLiteStore_DeleteForTenant(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	a := sampleLog("agent-a", "sess-1")
	a.TenantID = "ten-a"
	b := sampleLog("agent-b", "sess-2")
	b.TenantID = "ten-b"
	require.NoError(t, store.Log(ctx, a))
	require.NoError(t, store.Log(ctx, b))

	count, err := store.DeleteForTenant(ctx, "ten-a")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	logs, err := store.Query(ctx, InteractionFilter{})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "ten-b", logs[0].TenantID)
}

func TestSQLiteStore_QueryLimit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	}

	logs, err := store.Query(ctx, InteractionFilter{Limit: 3})
	require.NoError(t, err)
	assert.Len(t, logs, 3)
}

func TestSQLiteStore_QueryDefaultLimit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for i := 0; i < 60; i++ {
		require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	}

	logs, err := store.Query(ctx, InteractionFilter{})
	require.NoError(t, err)
	assert.Len(t, logs, DefaultLimit)
}

func TestSQLiteStore_QueryMaxLimit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Even with a very high limit, the query should not error.
	_, err := store.Query(ctx, InteractionFilter{Limit: 5000})
	require.NoError(t, err)
}

func TestSQLiteStore_QueryOffset(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	}

	logs, err := store.Query(ctx, InteractionFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.Len(t, logs, 2)
}

func TestSQLiteStore_QuerySince(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	old := sampleLog("agent-a", "sess-1")
	old.Timestamp = "2020-01-01T00:00:00Z"
	require.NoError(t, store.Log(ctx, old))

	recent := sampleLog("agent-a", "sess-2")
	recent.Timestamp = "2026-04-07T14:00:00Z"
	require.NoError(t, store.Log(ctx, recent))

	logs, err := store.Query(ctx, InteractionFilter{Since: "2026-01-01T00:00:00Z"})
	require.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.Equal(t, "sess-2", logs[0].SessionID)
}

func TestSQLiteStore_GetByID(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))

	logs, _ := store.Query(ctx, InteractionFilter{})
	require.Len(t, logs, 1)

	log, err := store.GetByID(ctx, logs[0].ID)
	require.NoError(t, err)
	require.NotNil(t, log)
	assert.Equal(t, "agent-a", log.AgentName)
	assert.Contains(t, log.RequestBody, "gpt-4o")
}

func TestSQLiteStore_GetByID_NotFound(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	log, err := store.GetByID(ctx, 99999)
	require.NoError(t, err)
	assert.Nil(t, log)
}

func TestSQLiteStore_DeleteAll(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	}

	count, err := store.DeleteAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)

	logs, _ := store.Query(ctx, InteractionFilter{})
	assert.Len(t, logs, 0)
}

func TestSQLiteStore_Count(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	count, err := store.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	require.NoError(t, store.Log(ctx, sampleLog("agent-b", "sess-2")))

	count, err = store.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestSQLiteStore_DatabaseSize(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	size, err := store.DatabaseSize(ctx)
	require.NoError(t, err)
	assert.Greater(t, size, int64(0))
}

func TestSQLiteStore_StreamingLog(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	entry := sampleLog("agent-a", "sess-1")
	entry.Streaming = true
	require.NoError(t, store.Log(ctx, entry))

	logs, _ := store.Query(ctx, InteractionFilter{})
	require.Len(t, logs, 1)
	assert.True(t, logs[0].Streaming)
}

func TestSQLiteStore_ErrorLog(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	entry := sampleLog("agent-a", "sess-1")
	entry.Error = "agent not found"
	entry.ResponseStatus = 404
	require.NoError(t, store.Log(ctx, entry))

	logs, _ := store.Query(ctx, InteractionFilter{})
	require.Len(t, logs, 1)
	assert.Equal(t, "agent not found", logs[0].Error)
	assert.Equal(t, 404, logs[0].ResponseStatus)
}

func TestSQLiteStore_OrderDescending(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, store.Log(ctx, sampleLog("agent-a", "sess-1")))
	}

	logs, _ := store.Query(ctx, InteractionFilter{})
	require.Len(t, logs, 3)
	// Most recent first (descending ID).
	assert.Greater(t, logs[0].ID, logs[1].ID)
	assert.Greater(t, logs[1].ID, logs[2].ID)
}

func TestTruncateBody(t *testing.T) {
	assert.Equal(t, "short", TruncateBody("short", 100))
	assert.Equal(t, "12345...", TruncateBody("1234567890", 5))
}

func TestSanitizeBody(t *testing.T) {
	result := SanitizeBody(`{"key": "sk-abc123def"}`)
	assert.Contains(t, result, "sk-***")
	assert.NotContains(t, result, "abc123def")
}

func TestSanitizeBody_NoSensitiveData(t *testing.T) {
	input := `{"model": "gpt-4o"}`
	assert.Equal(t, input, SanitizeBody(input))
}

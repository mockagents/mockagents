package server

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/stretchr/testify/require"
)

// newServerWithLogStore builds and starts a single-tenant server whose
// interaction-log store (and therefore the SSE broadcaster) is enabled, and
// returns its base URL.
func newServerWithLogStore(t *testing.T) string {
	t.Helper()
	logStore, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "logs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = logStore.Close() })

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewEngine(engine.NewAgentRegistry(), state.NewMemoryStore(5*time.Minute), logger)

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = t.TempDir()
	cfg.LogStore = logStore

	srv := New(eng, cfg, logger)
	require.NoError(t, srv.Listen(), "listen")
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { srv.Shutdown() })
	return "http://" + srv.ListenAddr()
}

// TestNewServer_MountsStreamRoutes is the F-SRV-ORDER-001 regression guard.
// When a log store is configured the live-feed routes must be mounted. Before
// the construction order was fixed, registerRoutes ran while s.logBroadcaster
// was still nil, so the whole `if s.logBroadcaster != nil` block was skipped
// and both /logs/stream and /logs/stream/metrics returned 404.
//
// The assertion targets /logs/stream/metrics: it lives inside the exact same
// guard block as /logs/stream but, unlike the SSE feed, returns immediately
// (plain JSON) so the test never has to interleave a blocking stream handler
// with graceful shutdown. A 200 here proves the guard block now executes (so
// /logs/stream is mounted too) AND that the broadcaster was handed to
// LogHandlers — a nil broadcaster would have yielded 503 "live feed disabled".
func TestNewServer_MountsStreamRoutes(t *testing.T) {
	base := newServerWithLogStore(t)

	resp, err := http.Get(base + "/api/v1/logs/stream/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.NotEqual(t, http.StatusNotFound, resp.StatusCode, "live-feed routes not mounted (F-SRV-ORDER-001)")
	require.Equal(t, http.StatusOK, resp.StatusCode, "stream metrics route mounted but broadcaster not wired")
}

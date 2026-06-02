package server

import (
	"context"
	"io"
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
// returns the server plus its base URL. Shutdown is idempotent, so the
// registered cleanup is safe even when a test calls Shutdown itself.
func newServerWithLogStore(t *testing.T) (*Server, string) {
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
	return srv, "http://" + srv.ListenAddr()
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
	_, base := newServerWithLogStore(t)

	resp, err := http.Get(base + "/api/v1/logs/stream/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.NotEqual(t, http.StatusNotFound, resp.StatusCode, "live-feed routes not mounted (F-SRV-ORDER-001)")
	require.Equal(t, http.StatusOK, resp.StatusCode, "stream metrics route mounted but broadcaster not wired")
}

// TestServerShutdown_UnblocksLiveFeed is the F-SRV-SHUT-002 regression guard.
// With an SSE client still connected to /logs/stream, Shutdown must return
// promptly: closing the broadcaster first unblocks the streaming handler so
// httpServer.Shutdown isn't pinned for the full ShutdownTimeout. The pre-fix
// ordering (close broadcaster AFTER httpServer.Shutdown) blocked ~5s.
func TestServerShutdown_UnblocksLiveFeed(t *testing.T) {
	srv, base := newServerWithLogStore(t)

	// Fire the SSE request in the background and KEEP it connected: drain the
	// body (io.Copy) so the client keeps reading until the test cancels or the
	// server severs the stream. Closing the body early would disconnect the
	// client, making the handler unsubscribe before the poll below sees it.
	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	go func() {
		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/api/v1/logs/stream", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body) // hold the connection open
	}()

	// Wait until the stream handler has subscribed, i.e. it is parked in the
	// select — so Shutdown faces a genuinely in-flight streaming connection.
	require.Eventually(t, func() bool {
		return srv.logBroadcaster.SubscriberCount() > 0
	}, 2*time.Second, 10*time.Millisecond, "stream handler never subscribed")

	done := make(chan struct{})
	go func() { _ = srv.Shutdown(); close(done) }()

	// Comfortably under ShutdownTimeout (5s): the fix returns near-instantly
	// because closing the broadcaster unblocks the handler; the pre-fix
	// ordering would hold httpServer.Shutdown for the full timeout.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown blocked on a connected stream client (F-SRV-SHUT-002)")
	}
}

package server

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/require"
)

func connFaultAgent(name, mode string) *types.AgentDefinition {
	a := testFullAgent(name, "gpt-4o")
	a.Spec.Behavior.Chaos = &types.ChaosConfig{
		Connection: &types.ChaosConnectionConfig{Mode: mode, Rate: 1},
	}
	return a
}

// setupServerWithLogStore builds the server WITH a LogStore so the
// InteractionCapture (captureWriter) middleware is mounted — required to
// exercise the FULL wrapper chain captureWriter -> statusWriter -> net.Conn.
func setupServerWithLogStore(t *testing.T, agents ...*types.AgentDefinition) string {
	base, _ := setupServerAndLogStore(t, agents...)
	return base
}

// setupServerAndLogStore is setupServerWithLogStore plus the store itself, for
// tests that assert on what was written to the interaction log.
func setupServerAndLogStore(t *testing.T, agents ...*types.AgentDefinition) (string, *storage.SQLiteStore) {
	t.Helper()
	registry := engine.NewAgentRegistry()
	for _, a := range agents {
		registry.Register(a)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewEngine(registry, state.NewMemoryStore(5*time.Minute), logger)

	logStore, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "logs.db"))
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = t.TempDir()
	cfg.LogStore = logStore // mounts InteractionCapture / captureWriter

	srv := New(eng, cfg, logger)
	require.NoError(t, srv.Listen())
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() {
		_ = srv.Shutdown()
		_ = logStore.Close()
	})
	return "http://" + srv.ListenAddr(), logStore
}

// TestConnectionFault_FullChainHijacks is the regression guard for FB-03 slice
// 5: a connection fault must travel through the FULL middleware chain —
// InteractionCapture's captureWriter AND StructuredLogger's statusWriter, BOTH
// of which must implement Unwrap — and actually fault the TCP connection. If any
// wrapper breaks the Unwrap chain, http.NewResponseController(w).Hijack() fails
// and the adapter writes a 502 "could not be delivered" fallback instead, so
// http.Post would return a normal response rather than a transport error. The
// server is built WITH a LogStore so captureWriter (the layer the live-smoke bug
// was in) is genuinely present.
func TestConnectionFault_FullChainHijacks(t *testing.T) {
	for _, mode := range []string{"empty", "reset"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			base := setupServerWithLogStore(t, connFaultAgent("cf-"+mode, mode))
			body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
			resp, err := http.Post(base+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err == nil {
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				t.Fatalf("mode %q: expected a transport-level error from the connection fault, got status %d body=%q — Hijack fell back to a 502, meaning a middleware wrapper (captureWriter or statusWriter) broke the Unwrap chain",
					mode, resp.StatusCode, b)
			}
		})
	}
}

// waitForFirstLog polls the store until one entry lands and returns it. The
// interaction log is written by an async worker, so the request returning says
// nothing about the row existing yet.
func waitForFirstLog(t *testing.T, store *storage.SQLiteStore) storage.InteractionLog {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		logs, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
		require.NoError(t, err)
		if len(logs) > 0 {
			return logs[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no interaction log was written within 5s")
	return storage.InteractionLog{}
}

// TestConnectionFault_NotLoggedAs200 is the guard for #41: the adapter hijacks
// the connection and never calls WriteHeader, so captureWriter's statusCode
// stays at the 200 it is initialised to and the log claimed the request
// succeeded while the client got a TCP reset.
func TestConnectionFault_NotLoggedAs200(t *testing.T) {
	for _, mode := range []string{"empty", "reset"} {
		t.Run(mode, func(t *testing.T) {
			base, store := setupServerAndLogStore(t, connFaultAgent("cf-log-"+mode, mode))
			body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
			resp, err := http.Post(base+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err == nil {
				resp.Body.Close()
				t.Fatalf("mode %q: expected a transport error, got status %d", mode, resp.StatusCode)
			}

			entry := waitForFirstLog(t, store)

			require.NotEqual(t, 200, entry.ResponseStatus,
				"a hijacked connection must not be logged as a success")
			require.Equal(t, 0, entry.ResponseStatus,
				"no HTTP status was sent, so the log should say so — matching what the metrics report as status=none")
			require.Equal(t, "/v1/chat/completions", entry.RequestPath)
		})
	}
}

// A normal request must keep reporting its real status: the hijack sentinel
// applies only to a connection that was actually taken over.
func TestNormalRequest_StillLogsItsStatus(t *testing.T) {
	base, store := setupServerAndLogStore(t, testFullAgent("ok-agent", "gpt-4o"))
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	resp, err := http.Post(base+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	entry := waitForFirstLog(t, store)

	require.Equal(t, 200, entry.ResponseStatus)
}

// An error response is not a hijack either — 404 must survive as 404.
func TestUnknownModel_StillLogsItsStatus(t *testing.T) {
	base, store := setupServerAndLogStore(t, testFullAgent("ok-agent-2", "gpt-4o"))
	body := `{"model":"no-such-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := http.Post(base+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()

	entry := waitForFirstLog(t, store)

	require.Equal(t, resp.StatusCode, entry.ResponseStatus)
	require.NotEqual(t, 0, entry.ResponseStatus, "only a hijack should log status 0")
}

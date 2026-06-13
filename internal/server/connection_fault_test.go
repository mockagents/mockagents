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
	return "http://" + srv.ListenAddr()
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

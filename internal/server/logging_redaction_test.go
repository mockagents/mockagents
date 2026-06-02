package server

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
)

// TestStructuredLogger_DoesNotLogSecrets is the F-MW-007 guard: the request
// logger must never echo the Authorization bearer token (or an api_key query
// param) into the log stream. Driving the full middleware chain via
// ServeHTTP keeps the assertion deterministic — the StructuredLogger writes
// its line synchronously before ServeHTTP returns, so there's no log/read
// race.
func TestStructuredLogger_DoesNotLogSecrets(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	eng := engine.NewEngine(engine.NewAgentRegistry(), state.NewMemoryStore(time.Minute), logger)

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = t.TempDir()
	srv := New(eng, cfg, logger)
	handler := srv.httpServer.Handler

	const secret = "sk-supersecret-DEADBEEF"
	req := httptest.NewRequest("GET", "/api/v1/health?api_key="+secret, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	out := buf.String()
	// Sanity: the request WAS logged (otherwise the assertion below is vacuous).
	if !strings.Contains(out, "/api/v1/health") {
		t.Fatalf("expected a request log line; got:\n%s", out)
	}
	if strings.Contains(out, secret) {
		t.Errorf("log output leaked a secret credential:\n%s", out)
	}
}

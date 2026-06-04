package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
)

// TestNormalizeLogBodyMode covers the SEC-05 mode parsing: known modes pass
// through, everything else (empty/garbage) defaults to full so an unset value
// never silently drops data.
func TestNormalizeLogBodyMode(t *testing.T) {
	cases := map[string]LogBodyMode{
		"":          LogBodyFull,
		"full":      LogBodyFull,
		"sanitized": LogBodySanitized,
		"none":      LogBodyNone,
		"garbage":   LogBodyFull,
		"FULL":      LogBodyFull, // case-sensitive → unknown → default full
	}
	for in, want := range cases {
		if got := NormalizeLogBodyMode(in); got != want {
			t.Errorf("NormalizeLogBodyMode(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestApplyLogBodyMode covers the per-mode body transform (SEC-05).
func TestApplyLogBodyMode(t *testing.T) {
	body := `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":5},"api_key":"sk-secret123"}`

	if got := applyLogBodyMode(LogBodyFull, body); got != body {
		t.Errorf("full mode altered the body")
	}
	if got := applyLogBodyMode(LogBodyNone, body); got != "" {
		t.Errorf("none mode = %q, want empty", got)
	}
	san := applyLogBodyMode(LogBodySanitized, body)
	if strings.Contains(san, "sk-secret123") {
		t.Errorf("sanitized mode left the secret intact: %q", san)
	}
	if !strings.Contains(san, "usage") {
		t.Errorf("sanitized mode dropped the usage block (cost annotation would break): %q", san)
	}
}

// TestInteractionCapture_BodyModeNone is the SEC-05 integration guard: in `none`
// mode the persisted ResponseBody is empty, yet the model probe (which runs on
// the raw body before the mode is applied) still resolves the agent so by_agent
// grouping is preserved.
func TestInteractionCapture_BodyModeNone(t *testing.T) {
	store := newTestStore(t)
	worker := NewLogWorker(store, nil, LogWorkerConfig{Workers: 1, QueueSize: 16})
	t.Cleanup(func() { worker.Shutdown(2 * time.Second) })

	handler := InteractionCapture(worker, LogBodyNone)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"gpt-4o","choices":[{"message":{"content":"sensitive reply"}}],"usage":{"prompt_tokens":3}}`))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", nil))

	if !waitForLog(t, store, 1, 2*time.Second) {
		t.Fatal("log not persisted")
	}
	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].ResponseBody != "" {
		t.Errorf("none mode persisted a body: %q", rows[0].ResponseBody)
	}
	if rows[0].AgentName != "gpt-4o" {
		t.Errorf("agent grouping lost in none mode: %q (probe should use the raw body)", rows[0].AgentName)
	}
}

// TestInteractionCapture_BodyModeSanitized verifies a secret in the response is
// redacted before persistence while the usage block survives (SEC-05).
func TestInteractionCapture_BodyModeSanitized(t *testing.T) {
	store := newTestStore(t)
	worker := NewLogWorker(store, nil, LogWorkerConfig{Workers: 1, QueueSize: 16})
	t.Cleanup(func() { worker.Shutdown(2 * time.Second) })

	handler := InteractionCapture(worker, LogBodySanitized)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"gpt-4o","leaked":"sk-supersecret","usage":{"prompt_tokens":3}}`))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", nil))

	if !waitForLog(t, store, 1, 2*time.Second) {
		t.Fatal("log not persisted")
	}
	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	body := rows[0].ResponseBody
	if strings.Contains(body, "sk-supersecret") {
		t.Errorf("sanitized mode persisted the secret: %q", body)
	}
	if !strings.Contains(body, "usage") {
		t.Errorf("sanitized mode dropped the usage block: %q", body)
	}
}

// TestLogPruner_TrimsToMaxRows covers the retention pruner lifecycle (SEC-05):
// it prunes once at start and Stop() cleanly joins the goroutine. A long tick
// interval means only the boot-time prune runs, making the assertion
// deterministic.
func TestLogPruner_TrimsToMaxRows(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 10; i++ {
		err := store.Log(t.Context(), &storage.InteractionLog{
			Timestamp:      "2026-06-04T00:00:00Z",
			AgentName:      "a",
			RequestMethod:  "POST",
			RequestPath:    "/v1/chat/completions",
			ResponseStatus: 200,
		})
		if err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	p := newLogPruner(store, 4, time.Hour, nil)
	p.start()
	p.Stop() // waits for the goroutine; the boot-time prune has completed

	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("after prune rows = %d, want 4", len(rows))
	}
}

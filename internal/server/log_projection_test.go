package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/storage"
)

// §9.1: `?fields=meta` is a privacy boundary, not a display one. Without it
// every captured body in the window crosses the network before anyone asks to
// see a single one, and a UI that hides them can only honestly claim they are
// not SHOWN — not that they were never sent.
func TestListLogs_MetadataProjection(t *testing.T) {
	store, err := storage.NewSQLiteStore(t.TempDir() + "/logs.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	if err := store.Log(ctx, &storage.InteractionLog{
		AgentName:    "support-bot",
		SessionID:    "sess-1",
		RequestBody:  `{"messages":[{"role":"user","content":"my credit card is 4111"}]}`,
		ResponseBody: `{"usage":{"prompt_tokens":11,"completion_tokens":7},"model":"gpt-4o"}`,
	}); err != nil {
		t.Fatal(err)
	}

	h := &LogHandlers{Store: store, Prices: pricing.NewDefaultTable()}
	list := func(query string) []LogWithCost {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/logs"+query, nil)
		rec := httptest.NewRecorder()
		h.ListLogs(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var rows []LogWithCost
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}

	t.Run("the default listing still carries bodies", func(t *testing.T) {
		rows := list("")
		if len(rows) != 1 || rows[0].RequestBody == "" || rows[0].ResponseBody == "" {
			t.Fatalf("bodies should be present by default, got %+v", rows)
		}
	})

	t.Run("fields=meta withholds them", func(t *testing.T) {
		rows := list("?fields=meta")
		if len(rows) != 1 {
			t.Fatalf("got %d rows", len(rows))
		}
		if rows[0].RequestBody != "" || rows[0].ResponseBody != "" {
			t.Error("a metadata listing must not send bodies")
		}
		// Metadata is the point — the row is still useful.
		if rows[0].AgentName != "support-bot" || rows[0].SessionID != "sess-1" {
			t.Errorf("metadata was dropped along with the bodies: %+v", rows[0])
		}
	})

	// Cost is derived FROM the response body, so the projection has to happen
	// after annotation. Get the order wrong and a metadata listing reports
	// every request as free, which is worse than sending the body.
	t.Run("cost survives the projection", func(t *testing.T) {
		full := list("")
		meta := list("?fields=meta")
		if meta[0].PromptTokens != full[0].PromptTokens || meta[0].PromptTokens == 0 {
			t.Errorf("token counts = %d, want the full listing's %d",
				meta[0].PromptTokens, full[0].PromptTokens)
		}
		if meta[0].CostUSD != full[0].CostUSD {
			t.Errorf("cost = %v, want %v", meta[0].CostUSD, full[0].CostUSD)
		}
	})

	// An unknown projection must not silently drop data a caller relies on.
	t.Run("an unrecognised fields value returns the full row", func(t *testing.T) {
		rows := list("?fields=everything")
		if rows[0].RequestBody == "" {
			t.Error("an unknown projection should be ignored, not applied")
		}
	})
}

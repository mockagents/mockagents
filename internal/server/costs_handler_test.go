package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// seedCostRow inserts an interaction-log row whose response body carries an
// OpenAI-style usage block so pricing.ExtractUsage can read it back.
func seedCostRow(t *testing.T, s *storage.SQLiteStore, tenantID, agent, model string, prompt, completion int) {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"usage":{"prompt_tokens":%d,"completion_tokens":%d}}`, model, prompt, completion)
	err := s.Log(context.Background(), &storage.InteractionLog{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		TenantID:       tenantID,
		AgentName:      agent,
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		ResponseStatus: 200,
		ResponseBody:   body,
	})
	if err != nil {
		t.Fatalf("seed cost row: %v", err)
	}
}

func findGroup(groups []CostGroup, key string) (CostGroup, bool) {
	for _, g := range groups {
		if g.Key == key {
			return g, true
		}
	}
	return CostGroup{}, false
}

// TestCostsHandler_NilStore503 covers the F-CO-001 nil-store guard.
func TestCostsHandler_NilStore503(t *testing.T) {
	h := &CostsHandlers{Store: nil}
	rec := httptest.NewRecorder()
	h.ListCosts(rec, httptest.NewRequest("GET", "/api/v1/costs", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil-store status = %d, want 503", rec.Code)
	}
}

// TestCostsHandler_AggregatesAndUnknownKeys covers the aggregation math and
// the "(unknown)" fallback keys (F-CO-001).
func TestCostsHandler_AggregatesAndUnknownKeys(t *testing.T) {
	store := newTestStore(t)
	seedCostRow(t, store, "", "weather", "gpt-4o", 10, 5)
	seedCostRow(t, store, "", "weather", "gpt-4o", 20, 10)
	seedCostRow(t, store, "", "", "", 1, 1) // empty agent + model -> "(unknown)"

	h := &CostsHandlers{Store: store}
	rec := httptest.NewRecorder()
	h.ListCosts(rec, httptest.NewRequest("GET", "/api/v1/costs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp CostsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Requests != 3 {
		t.Errorf("total requests = %d, want 3", resp.Requests)
	}
	if resp.PromptTokens != 31 || resp.CompletionTokens != 16 {
		t.Errorf("token totals = (%d,%d), want (31,16)", resp.PromptTokens, resp.CompletionTokens)
	}
	if g, ok := findGroup(resp.ByModel, "gpt-4o"); !ok || g.Requests != 2 || g.PromptTokens != 30 {
		t.Errorf("by_model gpt-4o = %+v (ok=%v), want requests=2 prompt=30", g, ok)
	}
	if _, ok := findGroup(resp.ByModel, "(unknown)"); !ok {
		t.Error("by_model missing (unknown) bucket")
	}
	if g, ok := findGroup(resp.ByAgent, "weather"); !ok || g.Requests != 2 {
		t.Errorf("by_agent weather = %+v (ok=%v), want requests=2", g, ok)
	}
	if _, ok := findGroup(resp.ByAgent, "(unknown)"); !ok {
		t.Error("by_agent missing (unknown) bucket")
	}
}

// TestCostsHandler_LimitOutOfRange400 covers the shared limit clamp (F-CO-001).
func TestCostsHandler_LimitOutOfRange400(t *testing.T) {
	h := &CostsHandlers{Store: newTestStore(t)}
	rec := httptest.NewRecorder()
	h.ListCosts(rec, httptest.NewRequest("GET", "/api/v1/costs?limit=0", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=0 status = %d, want 400", rec.Code)
	}
}

// TestCostsHandler_TenantFilter covers per-tenant scoping (F-CO-001): a
// caller only sees their own tenant's costs.
func TestCostsHandler_TenantFilter(t *testing.T) {
	store := newTestStore(t)
	seedCostRow(t, store, "ten_a", "a", "gpt-4o", 10, 5)
	seedCostRow(t, store, "ten_b", "b", "gpt-4o", 99, 99)

	h := &CostsHandlers{Store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/costs", h.ListCosts)
	srv := httptest.NewServer(servePrincipal(&tenancy.Principal{TenantID: "ten_a", Role: tenancy.RoleViewer}, mux))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/costs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var cr CostsResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		t.Fatal(err)
	}
	if cr.Requests != 1 || cr.PromptTokens != 10 {
		t.Errorf("tenant-A view = (%d reqs, %d prompt), want (1, 10) — cross-tenant leak", cr.Requests, cr.PromptTokens)
	}
}

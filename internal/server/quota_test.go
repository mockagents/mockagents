package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// llmReq builds a request to an LLM endpoint with the given tenant on the
// engine context (the way WithPrincipalTenantScope would have set it).
func llmReq(tenantID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	if tenantID != "" {
		r = r.WithContext(engine.WithTenantID(r.Context(), tenantID))
	}
	return r
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestQuotaEnforce_RateLimit(t *testing.T) {
	enf := quota.NewEnforcer(quota.Config{RatePerSec: 1, RateBurst: 1})
	h := QuotaEnforce(enf)(okHandler())

	// First request for ten_a passes; second is rate-limited with Retry-After.
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, llmReq("ten_a"))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first = %d, want 200", rec1.Code)
	}
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, llmReq("ten_a"))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second = %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Error("429 should carry a Retry-After header")
	}
}

func TestQuotaEnforce_SpendCap(t *testing.T) {
	enf := quota.NewEnforcer(quota.Config{MonthlySpendUSD: 1.0})
	h := QuotaEnforce(enf)(okHandler())

	// Under cap → allowed.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, llmReq("ten_a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("under cap = %d, want 200", rec.Code)
	}
	// Accrue past the cap → next request is 402.
	enf.AddSpend("ten_a", 1.5)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, llmReq("ten_a"))
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("over cap = %d, want 402", rec.Code)
	}
}

func TestQuotaEnforce_AnonymousAndNonQuotaPathBypass(t *testing.T) {
	enf := quota.NewEnforcer(quota.Config{RatePerSec: 1, RateBurst: 1, MonthlySpendUSD: 0.001})
	h := QuotaEnforce(enf)(okHandler())

	// Anonymous traffic (no tenant) is never limited.
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, llmReq(""))
		if rec.Code != http.StatusOK {
			t.Fatalf("anonymous req %d = %d, want 200", i, rec.Code)
		}
	}
	// A non-LLM path is never quota-limited even for a capped tenant.
	enf.AddSpend("ten_a", 1) // way over the 0.001 cap
	r := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	r = r.WithContext(engine.WithTenantID(r.Context(), "ten_a"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("non-LLM path = %d, want 200 (not quota-gated)", rec.Code)
	}
}

func TestQuotaEnforce_NilEnforcerPassthrough(t *testing.T) {
	h := QuotaEnforce(nil)(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, llmReq("ten_a"))
	if rec.Code != http.StatusOK {
		t.Errorf("nil enforcer should pass through, got %d", rec.Code)
	}
}

func TestQuotaHandlers_GetReflectsOverride(t *testing.T) {
	enf := quota.NewEnforcer(quota.Config{})
	enf.SetOverride("ten_a", quota.Config{RatePerSec: 5, MonthlySpendUSD: 42})
	enf.AddSpend("ten_a", 7)
	h := &QuotaHandlers{Enforcer: enf}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/quota", nil)
	r = r.WithContext(tenancy.WithPrincipal(r.Context(), &tenancy.Principal{TenantID: "ten_a", Role: tenancy.RoleViewer}))
	rec := httptest.NewRecorder()
	h.GetQuota(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GetQuota = %d, want 200", rec.Code)
	}
	var resp struct {
		TenantID string       `json:"tenant_id"`
		Limits   quota.Config `json:"limits"`
		Usage    quota.Usage  `json:"usage"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.TenantID != "ten_a" || resp.Limits.MonthlySpendUSD != 42 || resp.Usage.SpendUSD != 7 {
		t.Errorf("GetQuota body = %+v", resp)
	}
}

func TestQuotaHandlers_SetTenantQuota(t *testing.T) {
	enf := quota.NewEnforcer(quota.Config{})
	h := &QuotaHandlers{Enforcer: enf}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/tenants/{id}/quota", h.SetTenantQuota)

	body := `{"rate_per_sec":10,"rate_burst":20,"monthly_spend_usd":100}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/ten_a/quota", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SetTenantQuota = %d, want 200", rec.Code)
	}
	if got := enf.Effective("ten_a"); got.RatePerSec != 10 || got.RateBurst != 20 || got.MonthlySpendUSD != 100 {
		t.Errorf("override not applied: %+v", got)
	}

	// Negative values are rejected.
	bad := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/ten_a/quota", strings.NewReader(`{"rate_per_sec":-1}`))
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Errorf("negative quota = %d, want 400", badRec.Code)
	}
}

// TestQuotaHandlers_SetTenantQuota_Persists verifies that with a Store wired in,
// PUT persists the override (so it survives a restart) and an unknown tenant is
// a 404.
func TestQuotaHandlers_SetTenantQuota_Persists(t *testing.T) {
	store, err := tenancy.NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ten, err := store.CreateTenant(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	enf := quota.NewEnforcer(quota.Config{})
	h := &QuotaHandlers{Enforcer: enf, Store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/tenants/{id}/quota", h.SetTenantQuota)

	body := `{"rate_per_sec":10,"rate_burst":20,"monthly_spend_usd":100}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/"+ten.ID+"/quota", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Applied to the live enforcer AND persisted in the store.
	if got := enf.Effective(ten.ID); got.MonthlySpendUSD != 100 {
		t.Errorf("enforcer override = %+v", got)
	}
	q, err := store.GetTenantQuota(context.Background(), ten.ID)
	if err != nil || q == nil || q.RatePerSec != 10 || q.RateBurst != 20 || q.MonthlySpendUSD != 100 {
		t.Errorf("persisted = %+v err=%v", q, err)
	}

	// An unknown tenant → 404 (and nothing persisted).
	missing := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/ten_missing/quota", strings.NewReader(body))
	missingRec := httptest.NewRecorder()
	mux.ServeHTTP(missingRec, missing)
	if missingRec.Code != http.StatusNotFound {
		t.Errorf("unknown tenant = %d, want 404", missingRec.Code)
	}
}

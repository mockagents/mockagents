package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEngineEndpoint_OptIn is the audit M-07 guard: the generic engine
// endpoint (unauthenticated, unmetered, addresses any agent by name) is not
// mounted unless a deployment opts in, and only then is it auth-exempt.
func TestEngineEndpoint_OptIn(t *testing.T) {
	body := `{"agent_name":"echo","messages":[{"role":"user","content":"hi"}]}`
	for _, enabled := range []bool{false, true} {
		cfg := DefaultConfig()
		cfg.EnableEngineEndpoint = enabled
		eng := newTestEngineFromReg()
		eng.Registry.Register(testFullAgent("echo", "gpt-4o"))
		srv := New(eng, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

		rec := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/engines/process", strings.NewReader(body)))
		open := srv.skipAuth(httptest.NewRequest(http.MethodPost, "/v1/engines/process", nil))
		if enabled {
			if rec.Code == http.StatusNotFound {
				t.Fatal("enabled: endpoint not mounted")
			}
			if !open {
				t.Fatal("enabled: endpoint should be auth-exempt like the provider surfaces")
			}
		} else {
			if rec.Code != http.StatusNotFound {
				t.Fatalf("disabled: status = %d, want 404", rec.Code)
			}
			if open {
				t.Fatal("disabled: endpoint must not be in the auth-exempt set")
			}
		}
	}
}

// TestCORS_ExposesConditionalWriteHeaders: a browser client must be able to
// send If-Match and read the revision ETag family (audit M-06).
func TestCORS_ExposesConditionalWriteHeaders(t *testing.T) {
	h := CORS(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/agents/x", nil)
	req.Header.Set("Origin", "https://console.example")
	h.ServeHTTP(rec, req)
	for _, want := range []string{"If-Match", "If-None-Match"} {
		if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), want) {
			t.Errorf("Allow-Headers lacks %s: %q", want, rec.Header().Get("Access-Control-Allow-Headers"))
		}
	}
	for _, want := range []string{"ETag", "X-Mockagents-Revision-Effective", "X-Request-Id"} {
		if !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), want) {
			t.Errorf("Expose-Headers lacks %s: %q", want, rec.Header().Get("Access-Control-Expose-Headers"))
		}
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Error("Allow-Methods lacks PATCH (used by /api/v1/keys/{id})")
	}
}

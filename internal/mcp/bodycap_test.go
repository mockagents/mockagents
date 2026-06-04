package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHTTPHandler_RejectsOversizedBody is the SEC-01 guard: the JSON-RPC HTTP
// transport must cap the request body so a hostile client can't drive an
// unbounded io.ReadAll allocation. An over-cap POST returns 413, not an OOM.
func TestHTTPHandler_RejectsOversizedBody(t *testing.T) {
	srv := NewServer(v02Def())
	big := strings.Repeat("a", maxMCPBodyBytes+1024)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(big))
	rec := httptest.NewRecorder()

	NewHTTPHandler(srv).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for an over-cap body", rec.Code)
	}
}

// TestNotifyHandler_RejectsOversizedBody covers the same cap on the admin
// notify endpoint (SEC-01).
func TestNotifyHandler_RejectsOversizedBody(t *testing.T) {
	srv := NewServer(v02Def())
	big := `{"method":"x","params":"` + strings.Repeat("a", maxMCPBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/notify", strings.NewReader(big))
	rec := httptest.NewRecorder()

	NewNotifyHandler(srv).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for an over-cap body", rec.Code)
	}
}

// TestHTTPHandler_AcceptsNormalBody confirms the cap doesn't break a normal
// request (a small JSON-RPC ping still gets 200).
func TestHTTPHandler_AcceptsNormalBody(t *testing.T) {
	srv := NewServer(v02Def())
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	rec := httptest.NewRecorder()

	NewHTTPHandler(srv).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a normal body", rec.Code)
	}
}

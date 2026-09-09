package mcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

func surfaceServer() *Server {
	return NewServer(&types.MCPServerDefinition{Metadata: types.Metadata{Name: "surface"}})
}

// TestPendingQueueIsBounded is the audit M-25 guard: the legacy-transport
// notification queue and the bidirectional outbound queue drop the oldest
// entries past the cap instead of growing without limit.
func TestPendingQueueIsBounded(t *testing.T) {
	s := surfaceServer()
	for i := 0; i < maxPendingNotifications+50; i++ {
		s.EmitNotification("notifications/test", map[string]any{"i": i})
	}
	if n := s.PendingNotificationCount(); n != maxPendingNotifications {
		t.Fatalf("pending = %d, want cap %d", n, maxPendingNotifications)
	}
	s.bi.mu.Lock()
	outbound := len(s.bi.outbound)
	first := s.bi.outbound[0]
	s.bi.mu.Unlock()
	if outbound != maxPendingNotifications {
		t.Fatalf("outbound = %d, want cap %d", outbound, maxPendingNotifications)
	}
	if first.Notification == nil || first.Notification.Params["i"] != 50 {
		t.Fatalf("oldest retained outbound = %+v, want i=50 (drop-oldest)", first)
	}
}

// TestAdminRoutes_BodyCapAndTimeoutClamp is the audit M-24 guard.
func TestAdminRoutes_BodyCapAndTimeoutClamp(t *testing.T) {
	s := surfaceServer()
	big := strings.Repeat(" ", maxMCPBodyBytes+1) + "{}"

	rec := httptest.NewRecorder()
	NewResponseHandler(s).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/response", strings.NewReader(big)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("/mcp/response oversized: %d, want 413", rec.Code)
	}

	h := NewSendRequestHandler(s, "roots/list")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/roots", strings.NewReader(big)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("/mcp/roots oversized: %d, want 413", rec.Code)
	}

	// An absurd client timeout is clamped: with no client attached the
	// request must time out within maxAdminRequestTimeout, not the
	// requested ~11 days. Use a tiny clamp-relative budget by checking the
	// handler returns 504 well before the header's value could.
	h.DefaultTimeout = 50 * time.Millisecond
	req := httptest.NewRequest(http.MethodPost, "/mcp/roots", nil)
	req.Header.Set("X-MCP-Timeout-Ms", fmt.Sprint(int64(maxAdminRequestTimeout/time.Millisecond)*1000))
	rec = httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(rec, req); close(done) }()
	select {
	case <-done:
	case <-time.After(maxAdminRequestTimeout + 5*time.Second):
		t.Fatal("handler honoured an unbounded client timeout")
	}
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
}

// TestLegacyNotify_OriginGuard: the legacy /mcp/notify route applies the
// same DNS-rebinding guard as the streamable one (audit M-25).
func TestLegacyNotify_OriginGuard(t *testing.T) {
	s := surfaceServer()
	h := NewNotifyHandler(s)
	body := `{"method":"notifications/tools/list_changed"}`
	cases := []struct {
		origin string
		want   int
	}{
		{"", http.StatusAccepted},
		{"http://localhost:3000", http.StatusAccepted},
		{"https://evil.example", http.StatusForbidden},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp/notify", strings.NewReader(body))
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("origin %q: %d, want %d", c.origin, rec.Code, c.want)
		}
	}
	h.AllowedOrigins = []string{"https://evil.example"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/notify", strings.NewReader(body))
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("listed origin: %d, want 202", rec.Code)
	}
}

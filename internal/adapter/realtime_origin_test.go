package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func realtimeServerWithOrigins(t *testing.T, allowed []string) string {
	t.Helper()
	h := &RealtimeHandler{Engine: testEngine(testOpenAIAgent()), AllowedOrigins: allowed}
	mux := http.NewServeMux()
	for _, rt := range h.Routes() {
		mux.HandleFunc(rt.Pattern, rt.Handler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/realtime?model=gpt-4o"
}

func dialWithOrigin(t *testing.T, wsURL, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	return websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{"realtime"}, HTTPHeader: hdr})
}

// TestRealtimeHandshake_OriginAllowlist: with an allowlist configured the
// handshake refuses a foreign Origin before upgrading (audit H-04); without
// one the mock keeps accepting any origin, and non-browser clients (no
// Origin) are never affected.
func TestRealtimeHandshake_OriginAllowlist(t *testing.T) {
	restricted := realtimeServerWithOrigins(t, []string{"https://console.example:8443"})

	if c, resp, err := dialWithOrigin(t, restricted, "https://evil.example"); err == nil {
		c.Close(websocket.StatusNormalClosure, "")
		t.Fatal("foreign origin was accepted despite the allowlist")
	} else if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: expected 403 handshake, got resp=%v err=%v", resp, err)
	}

	c, _, err := dialWithOrigin(t, restricted, "https://console.example:8443")
	if err != nil {
		t.Fatalf("allow-listed origin rejected: %v", err)
	}
	c.Close(websocket.StatusNormalClosure, "")

	c, _, err = dialWithOrigin(t, restricted, "")
	if err != nil {
		t.Fatalf("non-browser client (no Origin) rejected: %v", err)
	}
	c.Close(websocket.StatusNormalClosure, "")

	open := realtimeServerWithOrigins(t, nil)
	c, _, err = dialWithOrigin(t, open, "https://evil.example")
	if err != nil {
		t.Fatalf("permissive default rejected an origin: %v", err)
	}
	c.Close(websocket.StatusNormalClosure, "")
}

func TestOriginPatterns(t *testing.T) {
	if got := originPatterns(nil); got != nil {
		t.Fatalf("nil list -> %v, want nil", got)
	}
	if got := originPatterns([]string{"https://a.example", "*"}); got != nil {
		t.Fatalf("wildcard -> %v, want nil", got)
	}
	got := originPatterns([]string{"https://a.example:8443", " http://b.example ", "c.example"})
	want := []string{"a.example:8443", "b.example", "c.example"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("patterns = %v, want %v", got, want)
	}
}

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Round-7 R7-20: the browser WebSocket credential (Sec-WebSocket-Protocol
// "openai-insecure-api-key.<key>") is lifted into Authorization on
// /v1/realtime so tenancy's best-effort resolution can scope the socket.
func TestRealtimeBrowserAuthLift(t *testing.T) {
	var got string
	h := RealtimeBrowserAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
	}))

	run := func(path, auth string, protocols ...string) string {
		r := httptest.NewRequest("GET", path, nil)
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		for _, p := range protocols {
			r.Header.Add("Sec-WebSocket-Protocol", p)
		}
		got = ""
		h.ServeHTTP(httptest.NewRecorder(), r)
		return got
	}

	if lifted := run("/v1/realtime", "", "realtime, openai-insecure-api-key.mak_secret123"); lifted != "Bearer mak_secret123" {
		t.Errorf("lift = %q, want the subprotocol key as a bearer", lifted)
	}
	// An existing Authorization header wins.
	if kept := run("/v1/realtime", "Bearer existing", "openai-insecure-api-key.mak_other"); kept != "Bearer existing" {
		t.Errorf("existing header overwritten: %q", kept)
	}
	// Ephemeral ek_ tokens are not tenant credentials — never lifted.
	if lifted := run("/v1/realtime", "", "openai-insecure-api-key.ek_mint123"); lifted != "" {
		t.Errorf("ek_ token lifted: %q", lifted)
	}
	// Other paths are untouched.
	if lifted := run("/v1/chat/completions", "", "openai-insecure-api-key.mak_secret123"); lifted != "" {
		t.Errorf("non-realtime path lifted: %q", lifted)
	}
}

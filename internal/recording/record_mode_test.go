package recording

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// countingUpstream returns an httptest upstream that increments *hits per
// request and replies with the given status + body.
func countingUpstream(t *testing.T, hits *int32, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestReplay_RecordOnMiss_NewEpisodes mirrors how runReplay wires new_episodes:
// a miss forwards to upstream + records; an identical second request replays
// from the cassette without hitting upstream again.
func TestReplay_RecordOnMiss_NewEpisodes(t *testing.T) {
	var hits int32
	upstream := countingUpstream(t, &hits, 200, `{"id":"up","choices":[{"message":{"content":"hello"}}]}`)

	path := filepath.Join(t.TempDir(), "c.jsonl")
	cass, err := Load(path) // missing file → empty cassette
	if err != nil {
		t.Fatal(err)
	}
	rp := NewReplay(cass)
	proxy, err := NewProxy(upstream.URL, cass)
	if err != nil {
		t.Fatal(err)
	}
	proxy.SkipRecordOnError = true
	rp.Fallback = proxy
	srv := httptest.NewServer(rp)
	defer srv.Close()

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`

	// Request 1: miss → record-on-miss.
	resp1, b1 := post(t, srv.URL, body)
	if resp1.StatusCode != 200 || !strings.Contains(b1, "hello") {
		t.Fatalf("request 1: status=%d body=%s", resp1.StatusCode, b1)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("upstream hits after miss = %d, want 1", got)
	}
	if cass.Len() != 1 {
		t.Fatalf("cassette len = %d, want 1 (recorded)", cass.Len())
	}

	// Request 2: identical → replay from cassette, upstream NOT hit again.
	resp2, _ := post(t, srv.URL, body)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("upstream hit again on replay: hits = %d, want 1", got)
	}
	if resp2.Header.Get("X-Mockagents-Replay") != "hit" {
		t.Errorf("expected X-Mockagents-Replay: hit, got %q", resp2.Header.Get("X-Mockagents-Replay"))
	}
}

// TestProxy_AllMode_AlwaysRecords mirrors `all`: the proxy is the route handler,
// so every request forwards + records even when identical.
func TestProxy_AllMode_AlwaysRecords(t *testing.T) {
	var hits int32
	upstream := countingUpstream(t, &hits, 200, `{"ok":true}`)
	cass := New("")
	proxy, err := NewProxy(upstream.URL, cass)
	if err != nil {
		t.Fatal(err)
	}
	proxy.SkipRecordOnError = true
	srv := httptest.NewServer(proxy)
	defer srv.Close()

	body := `{"model":"x"}`
	post(t, srv.URL, body)
	post(t, srv.URL, body) // identical

	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("all mode must forward every request: hits = %d, want 2", got)
	}
	if cass.Len() != 2 {
		t.Errorf("all mode must record every request: len = %d, want 2", cass.Len())
	}
}

func TestProxy_SkipRecordOnError(t *testing.T) {
	t.Run("skip=true drops a 500", func(t *testing.T) {
		var hits int32
		upstream := countingUpstream(t, &hits, 500, `{"error":"boom"}`)
		cass := New("")
		proxy, _ := NewProxy(upstream.URL, cass)
		proxy.SkipRecordOnError = true
		srv := httptest.NewServer(proxy)
		defer srv.Close()

		resp, body := post(t, srv.URL, `{"model":"x"}`)
		if resp.StatusCode != 500 {
			t.Errorf("client should still see 500, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "boom") {
			t.Errorf("client should see upstream error body, got %s", body)
		}
		if cass.Len() != 0 {
			t.Errorf("a 500 must NOT be recorded with SkipRecordOnError, len = %d", cass.Len())
		}
	})

	t.Run("default records a 500", func(t *testing.T) {
		var hits int32
		upstream := countingUpstream(t, &hits, 500, `{"error":"boom"}`)
		cass := New("")
		proxy, _ := NewProxy(upstream.URL, cass) // SkipRecordOnError defaults false
		srv := httptest.NewServer(proxy)
		defer srv.Close()

		post(t, srv.URL, `{"model":"x"}`)
		if cass.Len() != 1 {
			t.Errorf("default proxy must record the 500 (record command behavior), len = %d", cass.Len())
		}
	})

	t.Run("skip=true still records a 200", func(t *testing.T) {
		var hits int32
		upstream := countingUpstream(t, &hits, 200, `{"ok":true}`)
		cass := New("")
		proxy, _ := NewProxy(upstream.URL, cass)
		proxy.SkipRecordOnError = true
		srv := httptest.NewServer(proxy)
		defer srv.Close()

		post(t, srv.URL, `{"model":"x"}`)
		if cass.Len() != 1 {
			t.Errorf("a 200 must still be recorded, len = %d", cass.Len())
		}
	})
}

// TestReplay_MatchIndexRebuildsOnGrow exercises the R-01 fix: when --match-ignore
// is active and the cassette grows (record-on-miss), the match index rebuilds so
// the newly-recorded interaction is matchable.
func TestReplay_MatchIndexRebuildsOnGrow(t *testing.T) {
	cass := New("")
	rp := NewReplay(cass)
	rp.Matcher = NewMatcher([]string{"temperature"})
	srv := httptest.NewServer(rp)
	defer srv.Close()

	body := `{"model":"gpt-4o","temperature":0.1}`

	// Empty cassette: first request misses and builds the index at len 0.
	resp1, _ := post(t, srv.URL, body)
	if resp1.StatusCode != http.StatusNotFound {
		t.Fatalf("expected initial miss, got %d", resp1.StatusCode)
	}

	// Append a new interaction (as record-on-miss would), temperature differing.
	if err := cass.Append(&Interaction{
		Method: "POST", Path: "/v1/chat/completions", ResponseStatus: 200,
		RequestBody:  json.RawMessage(`{"model":"gpt-4o","temperature":0.9}`),
		ResponseBody: json.RawMessage(`{"ok":true}`),
	}); err != nil {
		t.Fatal(err)
	}

	// Second request (temperature ignored) must now HIT via the rebuilt index.
	resp2, b2 := post(t, srv.URL, body)
	if resp2.StatusCode != 200 || !strings.Contains(b2, "ok") {
		t.Fatalf("match index did not rebuild after growth: status=%d body=%s", resp2.StatusCode, b2)
	}
}

// TestProxy_StreamMidFailureNotCachedOnMiss covers the R-01 fix: a 200 SSE that
// breaks mid-stream must NOT be recorded on the record-on-miss path (it would
// otherwise replay a truncated stream forever). With SkipRecordOnError=false the
// partial IS recorded (faithful `all`/`record` capture).
func TestProxy_StreamMidFailureNotCachedOnMiss(t *testing.T) {
	// Upstream sends a 200 event-stream, flushes one chunk, then aborts the
	// connection mid-stream via a panic (closes the conn without a clean EOF).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("data: {\"chunk\":1}\n\n"))
			f.Flush()
		}
		panic(http.ErrAbortHandler) // drop the connection mid-stream
	}))
	defer upstream.Close()

	t.Run("record-on-miss skips the broken stream", func(t *testing.T) {
		cass := New("")
		proxy, _ := NewProxy(upstream.URL, cass)
		proxy.SkipRecordOnError = true
		srv := httptest.NewServer(proxy)
		defer srv.Close()
		post(t, srv.URL, `{"model":"x","stream":true}`)
		if cass.Len() != 0 {
			t.Errorf("a mid-stream upstream failure must not be cached on the miss path, len=%d", cass.Len())
		}
	})

	t.Run("faithful capture records the partial", func(t *testing.T) {
		cass := New("")
		proxy, _ := NewProxy(upstream.URL, cass) // SkipRecordOnError=false (all/record)
		srv := httptest.NewServer(proxy)
		defer srv.Close()
		post(t, srv.URL, `{"model":"x","stream":true}`)
		if cass.Len() != 1 {
			t.Errorf("faithful capture should record the partial stream, len=%d", cass.Len())
		}
	})
}

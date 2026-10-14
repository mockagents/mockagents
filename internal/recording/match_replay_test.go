package recording

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// matchReplayServer builds an httptest server over a Replay whose cassette holds
// the given interactions, optionally with a match-ignore matcher.
func matchReplayServer(t *testing.T, ignore []string, its ...*Interaction) *httptest.Server {
	t.Helper()
	c := New("")
	for _, it := range its {
		if err := c.Append(it); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	rp := NewReplay(c)
	if len(ignore) > 0 {
		rp.Matcher = NewMatcher(ignore)
	}
	srv := httptest.NewServer(rp)
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func TestReplayMatchIgnore_HitsWhenIgnoredFieldDiffers(t *testing.T) {
	srv := matchReplayServer(t, []string{"temperature"}, &Interaction{
		Method: "POST", Path: "/v1/chat/completions", ResponseStatus: 200,
		RequestBody:  json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"temperature":0.2}`),
		ResponseBody: json.RawMessage(`{"ok":true}`),
	})
	// temperature differs (0.9 vs recorded 0.2) but is ignored → hit.
	resp, body := post(t, srv.URL, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"temperature":0.9}`)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 hit; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"ok":true`) {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestReplayMatchIgnore_MissWhenNonIgnoredFieldDiffers(t *testing.T) {
	srv := matchReplayServer(t, []string{"temperature"}, &Interaction{
		Method: "POST", Path: "/v1/chat/completions", ResponseStatus: 200,
		RequestBody:  json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"temperature":0.2}`),
		ResponseBody: json.RawMessage(`{"ok":true}`),
	})
	// model differs and is NOT ignored → miss even with matcher.
	resp, _ := post(t, srv.URL, `{"model":"gpt-4.1","messages":[{"role":"user","content":"hi"}],"temperature":0.2}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestReplayMatchIgnore_SequencePreserved(t *testing.T) {
	var its []*Interaction
	for i := 1; i <= 3; i++ {
		its = append(its, &Interaction{
			Method: "POST", Path: "/v1/chat/completions", ResponseStatus: 200,
			RequestBody:  json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"loop"}],"temperature":0.2}`),
			ResponseBody: json.RawMessage(fmt.Sprintf(`{"turn":%d}`, i)),
		})
	}
	srv := matchReplayServer(t, []string{"temperature"}, its...)
	for i := 1; i <= 3; i++ {
		// Each turn varies the ignored field; sequence must still advance.
		_, body := post(t, srv.URL, fmt.Sprintf(`{"model":"gpt-4o","messages":[{"role":"user","content":"loop"}],"temperature":%d}`, i))
		if !strings.Contains(body, fmt.Sprintf(`"turn":%d`, i)) {
			t.Errorf("turn %d got %s", i, body)
		}
	}
}

func TestReplayMatchIgnore_ConcurrentSafe(t *testing.T) {
	const n = 5
	var its []*Interaction
	for i := 0; i < n; i++ {
		its = append(its, &Interaction{
			Method: "POST", Path: "/v1/chat/completions", ResponseStatus: 200,
			RequestBody:  json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"c"}],"temperature":0.2}`),
			ResponseBody: json.RawMessage(fmt.Sprintf(`{"seq":%d}`, i)),
		})
	}
	srv := matchReplayServer(t, []string{"temperature"}, its...)

	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, body := post(t, srv.URL, fmt.Sprintf(`{"model":"gpt-4o","messages":[{"role":"user","content":"c"}],"temperature":%d}`, i))
			mu.Lock()
			seen[strings.TrimSpace(body)]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("expected %d distinct responses, got %d: %v", n, len(seen), seen)
	}
	for body, count := range seen {
		if count != 1 {
			t.Errorf("response %q served %d times, want 1", body, count)
		}
	}
}

// redactedInteraction mirrors the proxy's record path under --redact: the hash
// covers the ORIGINAL request body, then the stored copy is masked.
func redactedInteraction(t *testing.T, reqBody, respBody string) *Interaction {
	t.Helper()
	it := &Interaction{
		Method: "POST", Path: "/v1/chat/completions", ResponseStatus: 200,
		RequestBody: json.RawMessage(reqBody), ResponseBody: json.RawMessage(respBody),
	}
	it.Hash = HashRequest(it.Method, it.Path, []byte(reqBody))
	rd, err := NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rd.Apply(it); err != nil {
		t.Fatal(err)
	}
	if string(it.RequestBody) == reqBody {
		t.Fatal("fixture request was not redacted; the test would prove nothing")
	}
	return it
}

// The match index is keyed on the STORED (redacted) body while the incoming
// request carries the original, so --match-ignore never hit a redacted
// cassette — even for a byte-identical request plain replay serves. A match
// index miss must fall back to the exact stored hash, keeping the R-04
// sequence cursor.
func TestReplayMatchIgnore_FallsBackToExactHashOnRedactedCassette(t *testing.T) {
	req := `{"model":"gpt-4o","messages":[{"role":"user","content":"use sk-proj-abcdefghijklmnopqrstuvwxyz0123456789"}],"temperature":0.2}`
	srv := matchReplayServer(t, []string{"temperature"},
		redactedInteraction(t, req, `{"turn":1}`),
		redactedInteraction(t, req, `{"turn":2}`))

	for i, want := range []string{`"turn":1`, `"turn":2`, `"turn":2`} { // then repeat-last
		resp, body := post(t, srv.URL, req)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Mockagents-Replay") != "hit" {
			t.Fatalf("request %d: status=%d replay=%q, want a hit; body=%s",
				i+1, resp.StatusCode, resp.Header.Get("X-Mockagents-Replay"), body)
		}
		if !strings.Contains(body, want) {
			t.Errorf("request %d: body=%s, want %s", i+1, body, want)
		}
	}
}

func TestReplayMiss_DiagnosticsBody(t *testing.T) {
	srv := matchReplayServer(t, nil, &Interaction{
		Method: "POST", Path: "/v1/chat/completions", ResponseStatus: 200, Hash: "abc123def456aaaa",
		RequestBody:  json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"say hello"}]}`),
		ResponseBody: json.RawMessage(`{"ok":true}`),
	})
	resp, body := post(t, srv.URL, `{"model":"gpt-4o","messages":[{"role":"user","content":"say goodbye"}]}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	var mr missResponse
	if err := json.Unmarshal([]byte(body), &mr); err != nil {
		t.Fatalf("404 body is not valid JSON: %v\n%s", err, body)
	}
	if mr.Error != "no cassette match" {
		t.Errorf("error = %q", mr.Error)
	}
	if mr.Nearest == nil {
		t.Fatal("expected a nearest block")
	}
	if mr.Nearest.Hash != "abc123def456" {
		t.Errorf("nearest hash = %q, want 12-char prefix", mr.Nearest.Hash)
	}
	by := diffByField(mr.Nearest.Diff)
	if e, ok := by["messages"]; !ok || e.Kind != "changed" {
		t.Errorf("expected messages 'changed' in diff, got %+v", mr.Nearest.Diff)
	}
}

func TestReplayMiss_FallbackStillDelegates(t *testing.T) {
	called := false
	fb := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/v1/chat/completions", ResponseStatus: 200,
		RequestBody: json.RawMessage(`{"model":"gpt-4o"}`), ResponseBody: json.RawMessage(`{"ok":true}`)})
	rp := NewReplay(c)
	rp.Fallback = fb
	srv := httptest.NewServer(rp)
	defer srv.Close()

	resp, _ := post(t, srv.URL, `{"model":"different"}`)
	if !called {
		t.Error("fallback should be invoked on a miss")
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want fallback's 418 (no diagnostics when Fallback set)", resp.StatusCode)
	}
}

func TestReplayMissReturns404_JSONBody(t *testing.T) {
	// Empty cassette: still a structured JSON 404 with nearest omitted.
	srv := matchReplayServer(t, nil)
	resp, body := post(t, srv.URL, `{"model":"x"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var mr missResponse
	if err := json.Unmarshal([]byte(body), &mr); err != nil {
		t.Fatalf("404 body not JSON: %v\n%s", err, body)
	}
	if mr.Error != "no cassette match" || mr.Nearest != nil {
		t.Errorf("empty-cassette miss should have no nearest, got %+v", mr)
	}
}

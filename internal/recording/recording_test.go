package recording

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestHashRequestStableAcrossKeyOrder(t *testing.T) {
	a := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	b := []byte(`{"messages":[{"content":"hi","role":"user"}],"model":"gpt-4o"}`)
	if HashRequest("POST", "/v1/chat/completions", a) != HashRequest("POST", "/v1/chat/completions", b) {
		t.Error("hash should be invariant under JSON key reordering")
	}
}

func TestHashRequestDiffersOnBodyChange(t *testing.T) {
	a := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	b := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"bye"}]}`)
	if HashRequest("POST", "/v1/chat/completions", a) == HashRequest("POST", "/v1/chat/completions", b) {
		t.Error("hash should differ on body content change")
	}
}

func TestCassetteLoadMissingFileIsEmpty(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Len() != 0 {
		t.Errorf("expected empty cassette, got %d", c.Len())
	}
}

func TestCassetteAppendAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)
	body := json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	err := c.Append(&Interaction{
		Method:         "POST",
		Path:           "/v1/chat/completions",
		RequestBody:    body,
		ResponseStatus: 200,
		ResponseBody:   json.RawMessage(`{"choices":[{"message":{"content":"hello"}}]}`),
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Len() != 1 {
		t.Fatalf("expected 1 interaction after reload, got %d", reloaded.Len())
	}
	hash := HashRequest("POST", "/v1/chat/completions", body)
	if reloaded.Lookup(hash) == nil {
		t.Errorf("lookup by hash %s missed", hash)
	}
}

func TestProxyCapturesInteraction(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(bodyBytes, &req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"content":"from upstream"}}]}`))
		_ = req
	}))
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "cass.jsonl")
	cass := New(path)
	proxy, err := NewProxy(upstream.URL, cass)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	front := httptest.NewServer(proxy)
	defer front.Close()

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("from upstream")) {
		t.Errorf("proxy did not relay upstream body: %s", body)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if cass.Len() != 1 {
		t.Fatalf("cassette size = %d, want 1", cass.Len())
	}
	// File-on-disk should also be populated.
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat cassette file: %v", err)
	}
	if stat.Size() == 0 {
		t.Error("cassette file is empty on disk")
	}
}

func TestReplayServesCassetteHit(t *testing.T) {
	cass := New("")
	reqBody := json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	err := cass.Append(&Interaction{
		Method:          "POST",
		Path:            "/v1/chat/completions",
		RequestBody:     reqBody,
		ResponseStatus:  200,
		ResponseHeaders: map[string]string{"Content-Type": "application/json"},
		ResponseBody:    json.RawMessage(`{"choices":[{"message":{"content":"replayed"}}]}`),
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	rp := NewReplay(cass)
	srv := httptest.NewServer(rp)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(string(reqBody)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("X-Mockagents-Replay") != "hit" {
		t.Errorf("expected X-Mockagents-Replay: hit, got %q", resp.Header.Get("X-Mockagents-Replay"))
	}
	if !bytes.Contains(body, []byte("replayed")) {
		t.Errorf("replay did not return recorded body: %s", body)
	}
}

func TestReplayMissReturns404(t *testing.T) {
	rp := NewReplay(New(""))
	srv := httptest.NewServer(rp)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"x"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRecordThenReplayEndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-e2e","choices":[{"message":{"content":"end-to-end"}}]}`))
	}))
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "e2e.jsonl")
	cass := New(path)
	proxy, _ := NewProxy(upstream.URL, cass)
	recordSrv := httptest.NewServer(proxy)
	defer recordSrv.Close()

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"round-trip"}]}`
	_, err := http.Post(recordSrv.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("record POST: %v", err)
	}

	// Replay from disk.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rp := NewReplay(reloaded)
	replaySrv := httptest.NewServer(rp)
	defer replaySrv.Close()

	resp, err := http.Post(replaySrv.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("replay POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("end-to-end")) {
		t.Errorf("replay did not return upstream body: %s", body)
	}
	if resp.Header.Get("X-Mockagents-Replay") != "hit" {
		t.Error("replay hit header missing")
	}
}

// fakeSSEUpstream writes a canned 4-event SSE stream with tiny delays
// between events. Returns the exact bytes it emitted so tests can do
// byte-level comparisons against the replayed output.
func fakeSSEUpstream(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	chunks := [][]byte{
		[]byte("event: message\ndata: {\"id\":\"1\",\"delta\":\"Hello\"}\n\n"),
		[]byte("event: message\ndata: {\"id\":\"1\",\"delta\":\" \"}\n\n"),
		[]byte("event: message\ndata: {\"id\":\"1\",\"delta\":\"world\"}\n\n"),
		[]byte("event: done\ndata: [DONE]\n\n"),
	}
	var combined []byte
	for _, c := range chunks {
		combined = append(combined, c...)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write(c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	return srv, combined
}

func TestProxyCapturesStreamingResponse(t *testing.T) {
	upstream, expected := fakeSSEUpstream(t)
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "stream.jsonl")
	cass := New(path)
	proxy, err := NewProxy(upstream.URL, cass)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	front := httptest.NewServer(proxy)
	defer front.Close()

	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"go"}]}`
	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, expected) {
		t.Errorf("client bytes did not match upstream\nwant:\n%s\ngot:\n%s", expected, body)
	}

	if cass.Len() != 1 {
		t.Fatalf("cassette size = %d, want 1", cass.Len())
	}
	it := cass.All()[0]
	if !it.Streaming {
		t.Error("interaction not marked Streaming")
	}
	if len(it.StreamEvents) == 0 {
		t.Fatal("no stream events captured")
	}
	// Concatenating the captured events must yield exactly the upstream bytes.
	var rebuilt []byte
	for _, ev := range it.StreamEvents {
		rebuilt = append(rebuilt, []byte(ev.Data)...)
	}
	if !bytes.Equal(rebuilt, expected) {
		t.Errorf("cassette bytes did not match upstream\nwant:\n%s\ngot:\n%s", expected, rebuilt)
	}
	if len(it.ResponseBody) != 0 {
		t.Errorf("ResponseBody should be empty for streaming capture, got %d bytes", len(it.ResponseBody))
	}
}

func TestReplayServesCapturedStream(t *testing.T) {
	upstream, expected := fakeSSEUpstream(t)
	defer upstream.Close()

	// Record once.
	cass := New(filepath.Join(t.TempDir(), "rep.jsonl"))
	proxy, _ := NewProxy(upstream.URL, cass)
	recordSrv := httptest.NewServer(proxy)
	defer recordSrv.Close()

	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"go"}]}`
	resp, err := http.Post(recordSrv.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("record POST: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Replay (delays off to keep the test deterministic).
	rp := NewReplay(cass)
	replaySrv := httptest.NewServer(rp)
	defer replaySrv.Close()

	resp2, err := http.Post(replaySrv.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("replay POST: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get("X-Mockagents-Replay") != "hit-streaming" {
		t.Errorf("replay header = %q, want hit-streaming", resp2.Header.Get("X-Mockagents-Replay"))
	}
	if ct := resp2.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("replay Content-Type = %q, want text/event-stream", ct)
	}
	got, _ := io.ReadAll(resp2.Body)
	if !bytes.Equal(got, expected) {
		t.Errorf("replay bytes did not match original\nwant:\n%s\ngot:\n%s", expected, got)
	}
}

// TestStreamingRoundTripsThroughDisk verifies that a captured stream
// reloaded from disk still replays exactly the same bytes. Catches
// any JSON-escape regressions in the StreamEvent.Data field.
func TestStreamingRoundTripsThroughDisk(t *testing.T) {
	upstream, expected := fakeSSEUpstream(t)
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "disk.jsonl")
	cass := New(path)
	proxy, _ := NewProxy(upstream.URL, cass)
	recordSrv := httptest.NewServer(proxy)
	defer recordSrv.Close()

	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"go"}]}`
	resp, err := http.Post(recordSrv.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("record POST: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Len() != 1 {
		t.Fatalf("reloaded cassette size = %d, want 1", reloaded.Len())
	}

	rp := NewReplay(reloaded)
	replaySrv := httptest.NewServer(rp)
	defer replaySrv.Close()

	resp2, err := http.Post(replaySrv.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("replay POST: %v", err)
	}
	defer resp2.Body.Close()
	got, _ := io.ReadAll(resp2.Body)
	if !bytes.Equal(got, expected) {
		t.Errorf("round-trip bytes mismatch\nwant:\n%s\ngot:\n%s", expected, got)
	}
}

// newInteraction builds a distinct interaction for append tests.
func newInteraction(content string) *Interaction {
	return &Interaction{
		Method:         "POST",
		Path:           "/v1/chat/completions",
		RequestBody:    json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"` + content + `"}]}`),
		ResponseStatus: 200,
		ResponseBody:   json.RawMessage(`{"choices":[{"message":{"content":"ok"}}]}`),
	}
}

// TestCassetteAppendDoesNotRewriteTheFile pins the O(1) write: a full rewrite
// lands via os.Rename, which replaces the file, so the identity of the file on
// disk is what distinguishes appending from rewriting. (Sizes cannot: a rewrite
// produces byte-identical content.)
func TestCassetteAppendDoesNotRewriteTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)

	if err := c.Append(newInteraction("one")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	for i := range 5 {
		if err := c.Append(newInteraction(fmt.Sprintf("more-%d", i))); err != nil {
			t.Fatalf("Append: %v", err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if !os.SameFile(before, after) {
			t.Fatalf("append %d replaced the cassette file; it should be written in place", i)
		}
	}
}

// TestCassetteAppendWritesLinearBytes is the other half of #37: each append
// should grow the file by exactly its own line, not by the whole cassette.
func TestCassetteAppendWritesLinearBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)

	var sizes []int64
	for i := range 6 {
		if err := c.Append(newInteraction(fmt.Sprintf("turn-%d", i))); err != nil {
			t.Fatalf("Append: %v", err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		sizes = append(sizes, fi.Size())
	}

	first := sizes[0]
	for i := 1; i < len(sizes); i++ {
		delta := sizes[i] - sizes[i-1]
		// Lines differ only by the turn number, so every delta should be within
		// a couple of bytes of the first line.
		if delta < first-4 || delta > first+4 {
			t.Errorf("append %d grew the file by %d bytes, expected ~%d", i, delta, first)
		}
	}
}

func TestCassetteAppendRoundTripsManyInteractions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)
	for i := range 20 {
		if err := c.Append(newInteraction(fmt.Sprintf("turn-%d", i))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Len() != 20 {
		t.Fatalf("expected 20 interactions, got %d", reloaded.Len())
	}
	// Insertion order must survive the round trip: sequenced replay depends on it.
	for i, it := range reloaded.All() {
		want := fmt.Sprintf(`"turn-%d"`, i)
		if !strings.Contains(string(it.RequestBody), want) {
			t.Fatalf("interaction %d is %s, expected to contain %s", i, it.RequestBody, want)
		}
	}
}

func TestCassetteAppendAfterAppendAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)
	if err := c.AppendAll([]*Interaction{newInteraction("bulk-0"), newInteraction("bulk-1")}); err != nil {
		t.Fatalf("AppendAll: %v", err)
	}
	if err := c.Append(newInteraction("live")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Len() != 3 {
		t.Fatalf("expected 3 interactions, got %d", reloaded.Len())
	}
}

// TestCassetteAppendStartsANewLineAfterATornWrite guards the one hazard of
// appending instead of rewriting: a file left ending mid-line by an interrupted
// recorder must not have the next record glued onto the fragment.
func TestCassetteAppendStartsANewLineAfterATornWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)
	if err := c.Append(newInteraction("one")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(`{"method":"POST","pa`); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()

	if err := New(path).Append(newInteraction("two")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.Count(line, `"recorded_at"`) > 1 {
			t.Fatalf("two records share a line, the fragment was not terminated: %s", line)
		}
	}
}

func TestCassetteAppendIsSafeUnderConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)

	var wg sync.WaitGroup
	for i := range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Append(newInteraction(fmt.Sprintf("turn-%d", i))); err != nil {
				t.Errorf("Append: %v", err)
			}
		}()
	}
	wg.Wait()

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Len() != 25 {
		t.Fatalf("expected 25 interactions on disk, got %d", reloaded.Len())
	}
	if c.Len() != 25 {
		t.Fatalf("expected 25 interactions in memory, got %d", c.Len())
	}
}

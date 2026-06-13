package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// newStreamableTestServer wires the streamable handler plus the streamable
// notify admin endpoint onto an httptest server, mirroring the cmd wiring.
func newStreamableTestServer(t *testing.T) (*httptest.Server, *StreamableHTTPHandler) {
	t.Helper()
	h := NewStreamableHTTPHandler(NewServer(testDef()))
	h.HeartbeatInterval = 50 * time.Millisecond
	mux := http.NewServeMux()
	mux.Handle("/mcp", h)
	mux.Handle("/mcp/notify", NewStreamableNotifyHandler(h))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

// postJSON posts a JSON-RPC body and returns the response. Caller closes body.
func postJSON(t *testing.T, url, sessionID, accept, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if sessionID != "" {
		req.Header.Set(headerSessionID, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// initSession runs an initialize POST and returns the minted session id.
func initSession(t *testing.T, url string) string {
	t.Helper()
	resp := postJSON(t, url, "", "application/json", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, want 200", resp.StatusCode)
	}
	sid := resp.Header.Get(headerSessionID)
	if sid == "" {
		t.Fatalf("initialize did not return %s header", headerSessionID)
	}
	return sid
}

func TestStreamable_InitializeMintsSession(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")
	if len(sid) != 32 {
		t.Errorf("session id = %q, want 32 hex chars", sid)
	}
	// The negotiated protocol version is the bumped default.
	resp := postJSON(t, srv.URL+"/mcp", "", "application/json", `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(types.DefaultMCPProtocolVersion)) {
		t.Errorf("initialize body missing protocol version %q: %s", types.DefaultMCPProtocolVersion, body)
	}
}

func TestStreamable_MissingSessionIs400_UnknownIs404(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	// tools/list with NO session header is a client bug → 400.
	resp := postJSON(t, srv.URL+"/mcp", "", "application/json", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing-session status = %d, want 400", resp.StatusCode)
	}
	// A present-but-unknown session id → 404 so the client reinitializes.
	resp2 := postJSON(t, srv.URL+"/mcp", "deadbeef", "application/json", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown-session status = %d, want 404", resp2.StatusCode)
	}
}

func TestStreamable_JSONResponseRoundTrip(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")
	resp := postJSON(t, srv.URL+"/mcp", sid, "application/json",
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_forecast","arguments":{"city":"tokyo"}}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	body, _ := json.Marshal(out.Result)
	if !bytes.Contains(body, []byte("sunny, 22C")) {
		t.Errorf("unexpected tool result: %s", body)
	}
}

func TestStreamable_NotificationGets202(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")
	resp := postJSON(t, srv.URL+"/mcp", sid, "application/json",
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if len(b) != 0 {
		t.Errorf("notification response should have empty body, got %q", b)
	}
}

func TestStreamable_PostSSEDeliversResponse(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")

	// A POST that advertises text/event-stream gets its JSON-RPC response back
	// as a single SSE `message` event, then the stream closes.
	resp := postJSON(t, srv.URL+"/mcp", sid, "application/json, text/event-stream",
		`{"jsonrpc":"2.0","id":5,"method":"ping"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	events := readSSEEvents(t, resp.Body, 1)
	if len(events) != 1 {
		t.Fatalf("want exactly 1 SSE event, got %d: %+v", len(events), events)
	}
	if events[0].event != "message" {
		t.Errorf("event name = %q, want message", events[0].event)
	}
	if !strings.Contains(events[0].data, `"id":5`) {
		t.Errorf("event is not the response: %q", events[0].data)
	}
	// POST-stream events are deliberately id-less (non-resumable).
	if events[0].id != "" {
		t.Errorf("POST-stream event should have no id, got %q", events[0].id)
	}
}

func TestStreamable_GetStreamBroadcastAndResume(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")

	// Open the GET stream.
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/mcp", nil)
	getReq.Header.Set("Accept", "text/event-stream")
	getReq.Header.Set(headerSessionID, sid)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}

	// Drive two notifications through the admin endpoint.
	for i := 0; i < 2; i++ {
		nr := postJSON(t, srv.URL+"/mcp/notify", "", "",
			fmt.Sprintf(`{"method":"notifications/message","params":{"n":%d}}`, i))
		nr.Body.Close()
		if nr.StatusCode != http.StatusAccepted {
			t.Fatalf("notify status = %d, want 202", nr.StatusCode)
		}
	}

	events := readSSEEvents(t, getResp.Body, 2)
	if len(events) < 2 {
		t.Fatalf("want >=2 broadcast events, got %d", len(events))
	}
	lastID := events[1].id
	if lastID == "" {
		t.Fatalf("broadcast event missing id")
	}

	// Reconnect with Last-Event-Id == first event's id; only the second event
	// should be replayed.
	resumeReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/mcp", nil)
	resumeReq.Header.Set("Accept", "text/event-stream")
	resumeReq.Header.Set(headerSessionID, sid)
	resumeReq.Header.Set(headerLastEventID, events[0].id)
	resumeResp, err := http.DefaultClient.Do(resumeReq)
	if err != nil {
		t.Fatalf("resume GET: %v", err)
	}
	defer resumeResp.Body.Close()
	replayed := readSSEEvents(t, resumeResp.Body, 1)
	if len(replayed) < 1 {
		t.Fatalf("want >=1 replayed event")
	}
	if replayed[0].id != events[1].id {
		t.Errorf("replay started at id %q, want %q (events after Last-Event-Id only)", replayed[0].id, events[1].id)
	}
}

func TestStreamable_DeleteTerminatesSession(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")

	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/mcp", nil)
	delReq.Header.Set(headerSessionID, sid)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", delResp.StatusCode)
	}

	// A request on the now-dead session is 404.
	resp := postJSON(t, srv.URL+"/mcp", sid, "application/json", `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("post-delete status = %d, want 404", resp.StatusCode)
	}

	// Deleting an unknown session is 404.
	delReq2, _ := http.NewRequest(http.MethodDelete, srv.URL+"/mcp", nil)
	delReq2.Header.Set(headerSessionID, "nope")
	delResp2, _ := http.DefaultClient.Do(delReq2)
	delResp2.Body.Close()
	if delResp2.StatusCode != http.StatusNotFound {
		t.Fatalf("delete-unknown status = %d, want 404", delResp2.StatusCode)
	}
}

func TestStreamable_OriginRejected(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}

	// A loopback origin is allowed.
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req2.Header.Set("Origin", "http://localhost:3000")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("loopback origin status = %d, want 200", resp2.StatusCode)
	}
}

func TestStreamable_UnsupportedProtocolVersionRejected(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerSessionID, sid)
	req.Header.Set(headerProtocolVersion, "1999-01-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	// A supported version is accepted.
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"ping"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set(headerSessionID, sid)
	req2.Header.Set(headerProtocolVersion, "2025-06-18")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("supported version status = %d, want 200", resp2.StatusCode)
	}
}

func TestStreamable_GetRequiresEventStreamAccept(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mcp", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set(headerSessionID, sid)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want 406", resp.StatusCode)
	}
}

func TestStreamable_UnsupportedMethod(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/mcp", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") || !strings.Contains(allow, "DELETE") {
		t.Errorf("Allow header = %q, want GET/POST/DELETE", allow)
	}
}

func TestStreamable_SessionEvictionFIFO(t *testing.T) {
	m := newSessionManager(2)
	s1 := m.create()
	s2 := m.create()
	s3 := m.create() // evicts s1
	if _, ok := m.get(s1.id); ok {
		t.Errorf("s1 should have been evicted")
	}
	if _, ok := m.get(s2.id); !ok {
		t.Errorf("s2 should still be live")
	}
	if _, ok := m.get(s3.id); !ok {
		t.Errorf("s3 should be live")
	}
	if !s1.closed {
		t.Errorf("evicted session should be closed")
	}
}

func TestStreamable_LogBounded(t *testing.T) {
	s := newStreamSession("x")
	for i := 0; i < maxSessionLogEvents+50; i++ {
		s.broadcastNotification(&Notification{Method: "m"})
	}
	s.mu.Lock()
	got := len(s.log)
	s.mu.Unlock()
	if got > maxSessionLogEvents {
		t.Errorf("log len = %d, want <= %d", got, maxSessionLogEvents)
	}
}

func TestStreamable_SubscriberCap(t *testing.T) {
	s := newStreamSession("cap")
	cancels := make([]func(), 0, maxSubscribersPerSession)
	for i := 0; i < maxSubscribersPerSession; i++ {
		_, _, cancel, atCap := s.subscribe(0)
		if atCap {
			t.Fatalf("subscriber %d unexpectedly at cap", i)
		}
		cancels = append(cancels, cancel)
	}
	// One past the cap is rejected.
	_, ch, _, atCap := s.subscribe(0)
	if !atCap {
		t.Fatalf("subscribe past cap should report atCap")
	}
	// The returned channel must be closed (no registered subscriber).
	if _, ok := <-ch; ok {
		t.Fatalf("at-cap subscribe should return a closed channel")
	}
	// Releasing one slot lets a new subscriber in.
	cancels[0]()
	_, _, cancel, atCap := s.subscribe(0)
	if atCap {
		t.Fatalf("subscribe after release should succeed")
	}
	cancel()
	for _, c := range cancels[1:] {
		c()
	}
}

func TestStreamable_ConcurrentBroadcastNoRace(t *testing.T) {
	s := newStreamSession("c")
	_, ch, cancel, _ := s.subscribe(0)
	defer cancel()
	// Drain the subscriber so it never blocks the senders.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
		}
	}()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.broadcastNotification(&Notification{Method: "m"})
			}
		}()
	}
	wg.Wait()
	s.close()
	<-done
}

func TestIsLoopbackOrigin(t *testing.T) {
	cases := map[string]bool{
		"http://localhost":         true,
		"http://localhost:8080":    true,
		"http://127.0.0.1:3000":    true,
		"http://[::1]:9000":        true,
		"https://example.com":      false,
		"http://evil.localhost.io": false,
		"::not a url::":            false,
	}
	for origin, want := range cases {
		if got := isLoopbackOrigin(origin); got != want {
			t.Errorf("isLoopbackOrigin(%q) = %v, want %v", origin, got, want)
		}
	}
}

// --- SSE parsing helper ----------------------------------------------------

type sseEvent struct {
	id    string
	event string
	data  string
}

// readSSEEvents reads up to want complete SSE events from r, returning early if
// the stream closes. A short read deadline is enforced via a goroutine so a
// hung stream fails the test rather than blocking forever.
func readSSEEvents(t *testing.T, r io.Reader, want int) []sseEvent {
	t.Helper()
	type result struct {
		events []sseEvent
	}
	resCh := make(chan result, 1)
	go func() {
		var events []sseEvent
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var cur sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if cur.data != "" || cur.id != "" {
					events = append(events, cur)
					cur = sseEvent{}
					if len(events) >= want {
						resCh <- result{events}
						return
					}
				}
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, ":"):
				// heartbeat/comment — ignore
			}
		}
		resCh <- result{events}
	}()
	select {
	case res := <-resCh:
		return res.events
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %d SSE events", want)
		return nil
	}
}

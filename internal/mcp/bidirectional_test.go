package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

func newTestMCPServer() *Server {
	return NewServer(&types.MCPServerDefinition{
		Metadata: types.Metadata{Name: "test-mcp"},
	})
}

// --- unit tests for the bidirectional primitives ---

func TestSendRequestRoundTrip(t *testing.T) {
	s := newTestMCPServer()
	ch, cancel := s.bi.Subscribe(4)
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()

	// Launch SendRequest in a goroutine so we can drain the outbound
	// side and deliver a response synchronously from the test.
	type result struct {
		resp *Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := s.SendRequest(ctx, "sampling/createMessage", map[string]any{"prompt": "hi"})
		done <- result{resp, err}
	}()

	// The request should land on the subscriber channel almost
	// immediately.
	var msg *OutboundMessage
	select {
	case msg = <-ch:
	case <-time.After(time.Second):
		t.Fatal("outbound request never arrived")
	}
	if msg.Kind != OutboundRequest || msg.Request == nil {
		t.Fatalf("unexpected outbound %+v", msg)
	}
	if msg.Request.Method != "sampling/createMessage" {
		t.Errorf("method = %q", msg.Request.Method)
	}

	// Reply with the matching id.
	reply := &Response{
		JSONRPC: "2.0",
		ID:      msg.Request.ID,
		Result:  map[string]any{"text": "pong"},
	}
	if err := s.DeliverResponse(reply); err != nil {
		t.Fatalf("DeliverResponse: %v", err)
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("SendRequest err: %v", got.err)
		}
		if got.resp == nil || got.resp.Result == nil {
			t.Fatalf("resp = %+v", got.resp)
		}
	case <-time.After(time.Second):
		t.Fatal("SendRequest never returned")
	}
}

func TestSendRequestTimeout(t *testing.T) {
	s := newTestMCPServer()
	_, cancel := s.bi.Subscribe(4)
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelCtx()

	_, err := s.SendRequest(ctx, "roots/list", nil)
	if err == nil {
		t.Fatal("expected context error")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	// The pending entry must not leak: reply late and DeliverResponse
	// should return "no pending request" instead of hanging.
	// Build a bogus id 1 (matches the first outbound request).
	err = s.DeliverResponse(&Response{ID: json.RawMessage("1"), Result: "late"})
	if err == nil || !strings.Contains(err.Error(), "no pending") {
		t.Errorf("late delivery err = %v", err)
	}
}

func TestDeliverResponseUnknownID(t *testing.T) {
	s := newTestMCPServer()
	err := s.DeliverResponse(&Response{ID: json.RawMessage("9999"), Result: "x"})
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestSubscribeReplaysBufferedMessages(t *testing.T) {
	s := newTestMCPServer()
	// Buffer two notifications before anyone subscribes.
	s.EmitNotification("notifications/tools/list_changed", nil)
	s.EmitNotification("notifications/resources/list_changed", nil)

	ch, cancel := s.bi.Subscribe(8)
	defer cancel()

	var kinds []string
	timeout := time.After(500 * time.Millisecond)
	for i := 0; i < 2; i++ {
		select {
		case msg := <-ch:
			if msg.Kind != OutboundNotification {
				t.Errorf("kind = %q", msg.Kind)
			}
			kinds = append(kinds, msg.Notification.Method)
		case <-timeout:
			t.Fatal("replay did not deliver both buffered messages")
		}
	}
	want := []string{"notifications/tools/list_changed", "notifications/resources/list_changed"}
	for i, w := range want {
		if kinds[i] != w {
			t.Errorf("kinds[%d] = %q, want %q", i, kinds[i], w)
		}
	}
}

func TestSubscribeStealsPreviousSubscriber(t *testing.T) {
	s := newTestMCPServer()
	first, _ := s.bi.Subscribe(4)
	second, cancel := s.bi.Subscribe(4)
	defer cancel()

	// The first subscriber's channel must be closed.
	select {
	case _, ok := <-first:
		if ok {
			t.Error("first subscriber should receive zero-value on steal")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first subscriber was not closed on steal")
	}
	// The second must still receive messages.
	s.EmitNotification("notifications/ping", nil)
	select {
	case msg := <-second:
		if msg.Notification.Method != "notifications/ping" {
			t.Errorf("method = %q", msg.Notification.Method)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second subscriber never received")
	}
}

// --- HTTP-level tests ---

// newSSEPair wires up an httptest.Server that mounts the three v0.3
// endpoints and returns the Server plus the base URL.
func newSSEPair(t *testing.T) (*Server, string, func()) {
	t.Helper()
	s := newTestMCPServer()
	mux := http.NewServeMux()
	evt := NewEventStreamHandler(s)
	evt.HeartbeatInterval = 100 * time.Millisecond
	mux.Handle("/mcp/events", evt)
	mux.Handle("/mcp/response", NewResponseHandler(s))
	mux.Handle("/mcp/sample", NewSendRequestHandler(s, "sampling/createMessage"))
	mux.Handle("/mcp/roots", NewSendRequestHandler(s, "roots/list"))
	srv := httptest.NewServer(mux)
	return s, srv.URL, srv.Close
}

func readFrame(t *testing.T, r *bufio.Reader) (event, data string) {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			// blank line = frame terminator
			if data != "" || event != "" {
				return event, data
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			// heartbeat/comment — keep reading
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(line[len("event:"):])
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(line[len("data:"):])
			continue
		}
	}
}

func TestHTTPBidirectionalSampleRoundTrip(t *testing.T) {
	_, base, close := newSSEPair(t)
	defer close()

	// Start the SSE subscriber first so the outbound request lands on
	// the channel.
	req, err := http.NewRequest(http.MethodGet, base+"/mcp/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /mcp/events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)

	// Fire the admin-level /mcp/sample POST in a goroutine — it blocks
	// until the client POSTs a response, so we drain the SSE stream
	// and reply first.
	done := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		// Use a generous server-side timeout (not the 5s default): under a
		// loaded full-suite run the read-frame -> reply-POST sequence below can
		// take several seconds, and a fired SendRequest timeout would delete the
		// pending waiter and make the reply POST 404 (the historical flake).
		postReq, _ := http.NewRequest(http.MethodPost, base+"/mcp/sample", strings.NewReader(`{"prompt":"ping"}`))
		postReq.Header.Set("Content-Type", "application/json")
		postReq.Header.Set("X-MCP-Timeout-Ms", "60000")
		r, err := http.DefaultClient.Do(postReq)
		if err != nil {
			errCh <- err
			return
		}
		done <- r
	}()

	// Read the outbound SSE frame.
	event, data := readFrame(t, reader)
	if event != "request" {
		t.Fatalf("event = %q, want request", event)
	}
	var outReq Request
	if err := json.Unmarshal([]byte(data), &outReq); err != nil {
		t.Fatalf("decode outbound request: %v (data=%s)", err, data)
	}
	if outReq.Method != "sampling/createMessage" {
		t.Errorf("method = %q", outReq.Method)
	}
	if len(outReq.ID) == 0 {
		t.Error("request has no id")
	}

	// POST a matching response.
	replyBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(outReq.ID),
		"result":  map[string]any{"text": "pong"},
	}
	rb, _ := json.Marshal(replyBody)
	postResp, err := http.Post(base+"/mcp/response", "application/json", strings.NewReader(string(rb)))
	if err != nil {
		t.Fatalf("POST /mcp/response: %v", err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(postResp.Body)
		t.Fatalf("POST /mcp/response status = %d: %s", postResp.StatusCode, body)
	}

	// The /mcp/sample call should now return with the decoded response.
	select {
	case sampleResp := <-done:
		defer sampleResp.Body.Close()
		if sampleResp.StatusCode != 200 {
			body, _ := io.ReadAll(sampleResp.Body)
			t.Fatalf("/mcp/sample status = %d: %s", sampleResp.StatusCode, body)
		}
		var got Response
		if err := json.NewDecoder(sampleResp.Body).Decode(&got); err != nil {
			t.Fatalf("decode sample response: %v", err)
		}
		// Result round-trips as a map[string]any with "text" = "pong".
		result, _ := got.Result.(map[string]any)
		if result == nil || result["text"] != "pong" {
			t.Errorf("result = %+v", got.Result)
		}
	case err := <-errCh:
		t.Fatalf("sample POST failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("/mcp/sample never returned")
	}
}

func TestHTTPSendRequestTimeout(t *testing.T) {
	_, base, close := newSSEPair(t)
	defer close()

	// Subscribe so the request actually makes it to the outbound
	// channel (otherwise it just sits in the buffer forever, which is
	// also a valid timeout path but a slower test).
	req, _ := http.NewRequest(http.MethodGet, base+"/mcp/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	postReq, _ := http.NewRequest(http.MethodPost, base+"/mcp/sample", strings.NewReader(`{}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("X-MCP-Timeout-Ms", "100")
	r, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusGatewayTimeout {
		body, _ := io.ReadAll(r.Body)
		t.Errorf("status = %d: %s", r.StatusCode, body)
	}
}

func TestHTTPResponseHandlerUnknownID(t *testing.T) {
	_, base, close := newSSEPair(t)
	defer close()
	body := `{"jsonrpc":"2.0","id":123,"result":{}}`
	r, err := http.Post(base+"/mcp/response", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", r.StatusCode)
	}
}

func TestEventStreamCancelsOnClientDisconnect(t *testing.T) {
	s, base, closeSrv := newSSEPair(t)
	defer closeSrv()

	// Open a stream and cancel the context immediately. The server
	// should clean up the subscription so the next Subscribe call
	// gets its own messages without the previous one interfering.
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/mcp/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = resp.Body.Close()

	// Give the server a moment to process the disconnect.
	var subbed bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			s.bi.mu.Lock()
			nilSub := s.bi.sub == nil
			s.bi.mu.Unlock()
			if nilSub {
				subbed = true
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	wg.Wait()
	if !subbed {
		t.Error("subscription never cleared after client disconnect")
	}
}

package mockagents

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSSEFrame(t *testing.T) {
	cases := []struct {
		name     string
		frame    string
		wantData string
		wantNil  bool
	}{
		{"basic", "event: delta\ndata: hello", "hello", false},
		{"no_leading_space", "data:raw", "raw", false},
		{"multi_data", "data: line1\ndata: line2", "line1\nline2", false},
		{"skip_comment", ":heartbeat\ndata: ok", "ok", false},
		{"no_data", "event: noop\n", "", true},
		{"crlf", "event: x\r\ndata: y\r\n", "y", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSSEFrame(tc.frame)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want nil frame, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("unexpected nil frame")
			}
			if got.Data != tc.wantData {
				t.Errorf("data = %q, want %q", got.Data, tc.wantData)
			}
		})
	}
}

func TestNormalizeOpenAIStreamDropsPadding(t *testing.T) {
	var st normalizeState
	got := normalizeOpenAIStream(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}}},
	}, &st)
	if len(got) != 0 {
		t.Fatalf("padding chunk should drop, got %+v", got)
	}
}

func TestNormalizeOpenAIStreamTextAndToolAndFinish(t *testing.T) {
	var st normalizeState
	text := normalizeOpenAIStream(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{"content": "hi"},
		}},
	}, &st)
	if len(text) != 1 || text[0].Text != "hi" || text[0].Finished {
		t.Errorf("text chunk wrong: %+v", text)
	}

	tool := normalizeOpenAIStream(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    float64(2),
					"function": map[string]any{"name": "lookup", "arguments": `{"q":`},
				}},
			},
		}},
	}, &st)
	if len(tool) != 1 || tool[0].ToolCallDelta == nil {
		t.Fatalf("want tool delta, got %+v", tool)
	}
	if tool[0].ToolCallDelta.Index != 2 || tool[0].ToolCallDelta.Name != "lookup" || tool[0].ToolCallDelta.Fragment != `{"q":` {
		t.Errorf("tool delta = %+v", tool[0].ToolCallDelta)
	}

	done := normalizeOpenAIStream(map[string]any{
		"choices": []any{map[string]any{
			"delta":         map[string]any{},
			"finish_reason": "stop",
		}},
	}, &st)
	if len(done) != 1 || !done[0].Finished || done[0].FinishReason != "stop" {
		t.Errorf("finish chunk = %+v", done)
	}
}

func TestNormalizeAnthropicStreamTextAndToolAccumulation(t *testing.T) {
	var st normalizeState

	// Open a tool_use block.
	start := normalizeAnthropicStream(map[string]any{
		"type":  "content_block_start",
		"index": float64(0),
		"content_block": map[string]any{
			"type": "tool_use",
			"name": "search",
		},
	}, &st)
	if len(start) != 1 || start[0].ToolCallDelta == nil || start[0].ToolCallDelta.Name != "search" {
		t.Fatalf("start = %+v", start)
	}

	// input_json_delta should reuse the remembered index+name.
	frag := normalizeAnthropicStream(map[string]any{
		"type": "content_block_delta",
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": `{"q":"cats"}`,
		},
	}, &st)
	if len(frag) != 1 || frag[0].ToolCallDelta == nil ||
		frag[0].ToolCallDelta.Index != 0 || frag[0].ToolCallDelta.Name != "search" ||
		frag[0].ToolCallDelta.Fragment != `{"q":"cats"}` {
		t.Errorf("frag = %+v", frag[0].ToolCallDelta)
	}

	// Text delta.
	text := normalizeAnthropicStream(map[string]any{
		"type": "content_block_delta",
		"delta": map[string]any{
			"type": "text_delta",
			"text": "hello",
		},
	}, &st)
	if len(text) != 1 || text[0].Text != "hello" {
		t.Errorf("text = %+v", text)
	}

	// message_delta carries stop reason.
	_ = normalizeAnthropicStream(map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "tool_use"},
	}, &st)

	// message_stop terminal.
	stop := normalizeAnthropicStream(map[string]any{"type": "message_stop"}, &st)
	if len(stop) != 1 || !stop[0].Finished || stop[0].FinishReason != "tool_use" {
		t.Errorf("stop = %+v", stop)
	}
}

func TestNormalizeAnthropicStreamDefaultFinishReason(t *testing.T) {
	var st normalizeState
	stop := normalizeAnthropicStream(map[string]any{"type": "message_stop"}, &st)
	if len(stop) != 1 || stop[0].FinishReason != "end_turn" {
		t.Errorf("stop = %+v", stop)
	}
}

// --- End-to-end streaming against a fake SSE server ---

// sseWrite flushes the SSE frame to the response writer.
func sseWrite(w http.ResponseWriter, payload string) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func newSSEServer(t *testing.T, path string, frames []string, terminator string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, frame := range frames {
			sseWrite(w, frame)
		}
		if terminator != "" {
			sseWrite(w, terminator)
		}
	})
	return httptest.NewServer(mux)
}

func TestChatStreamEndToEnd(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"content":"hel"}}]}`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}
	srv := newSSEServer(t, "/v1/chat/completions", frames, "[DONE]")
	defer srv.Close()

	c := NewClient(ClientOptions{BaseURL: srv.URL})
	stream, err := c.ChatStream(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, ChatOptions{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer stream.Close()

	var events []map[string]any
	for stream.Next() {
		events = append(events, stream.Value())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream err: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %+v", len(events), events)
	}
}

func TestIterStreamOpenAIAssemblesText(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"content":"hel"}}]}`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}
	srv := newSSEServer(t, "/v1/chat/completions", frames, "[DONE]")
	defer srv.Close()

	c := NewClient(ClientOptions{BaseURL: srv.URL})
	stream, err := c.IterStream(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, IterStreamOptions{})
	if err != nil {
		t.Fatalf("IterStream: %v", err)
	}
	defer stream.Close()

	var text strings.Builder
	var finished bool
	for stream.Next() {
		ch := stream.Value()
		text.WriteString(ch.Text)
		if ch.Finished {
			finished = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("err: %v", err)
	}
	if text.String() != "hello" {
		t.Errorf("text = %q", text.String())
	}
	if !finished {
		t.Error("never saw finished chunk")
	}
}

func TestMessageStreamStopsOnMessageStop(t *testing.T) {
	frames := []string{
		`{"type":"message_start"}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`{"type":"message_stop"}`,
		// This event should never be surfaced — stream stops after message_stop.
		`{"type":"ghost"}`,
	}
	srv := newSSEServer(t, "/v1/messages", frames, "")
	defer srv.Close()

	c := NewClient(ClientOptions{BaseURL: srv.URL})
	stream, err := c.MessageStream(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, MessageOptions{})
	if err != nil {
		t.Fatalf("MessageStream: %v", err)
	}
	defer stream.Close()

	var kinds []string
	for stream.Next() {
		ev := stream.Value()
		t, _ := ev["type"].(string)
		kinds = append(kinds, t)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"message_start", "content_block_delta", "message_delta", "message_stop"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i, k := range want {
		if kinds[i] != k {
			t.Errorf("kinds[%d] = %q, want %q", i, kinds[i], k)
		}
	}
}

func TestIterStreamAnthropicEndToEnd(t *testing.T) {
	frames := []string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"hel"}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`{"type":"message_stop"}`,
	}
	srv := newSSEServer(t, "/v1/messages", frames, "")
	defer srv.Close()

	c := NewClient(ClientOptions{BaseURL: srv.URL})
	stream, err := c.IterStream(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, IterStreamOptions{Protocol: "anthropic"})
	if err != nil {
		t.Fatalf("IterStream: %v", err)
	}
	defer stream.Close()

	var text strings.Builder
	var finishReason string
	for stream.Next() {
		ch := stream.Value()
		text.WriteString(ch.Text)
		if ch.Finished {
			finishReason = ch.FinishReason
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("err: %v", err)
	}
	if text.String() != "hello" {
		t.Errorf("text = %q", text.String())
	}
	if finishReason != "end_turn" {
		t.Errorf("finish = %q", finishReason)
	}
}

func TestIterStreamUnknownProtocol(t *testing.T) {
	c := NewClient(ClientOptions{BaseURL: "http://nowhere.invalid"})
	_, err := c.IterStream(context.Background(), nil, IterStreamOptions{Protocol: "cohere"})
	if err == nil {
		t.Fatal("expected error for unknown protocol")
	}
	if !strings.Contains(err.Error(), "cohere") {
		t.Errorf("error = %v", err)
	}
}

func TestChatStreamHTTPErrorSurfacesAsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()
	c := NewClient(ClientOptions{BaseURL: srv.URL})
	_, err := c.ChatStream(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, ChatOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if herr, ok := err.(*HTTPError); !ok || herr.Status != 400 {
		t.Errorf("err = %v", err)
	}
}

// TestSplitSSEFramesMixedLineEndings is the regression test for a
// FuzzSSEStream finding: a CRLF-terminated frame followed by an
// LF-terminated one was merged into a single frame when both were already
// buffered (the "\n\n" search ran first and matched the later frame's
// terminator), so both events were silently dropped as invalid JSON. The
// same bytes arriving in smaller chunks parsed correctly.
func TestSplitSSEFramesMixedLineEndings(t *testing.T) {
	body := "data: {\"a\":1}\r\n\r\ndata: {\"b\":2}\n\n"
	r := strings.NewReader(body)
	stream := &RawEventStream{body: io.NopCloser(r), scanner: newSSEScanner(r)}
	defer stream.Close()
	var got []map[string]any
	for stream.Next() {
		got = append(got, stream.Value())
	}
	if len(got) != 2 || got[0]["a"] != float64(1) || got[1]["b"] != float64(2) {
		t.Fatalf("events = %v, want [{a:1} {b:2}]", got)
	}

	sc := newSSEScanner(strings.NewReader("\r\n\r\n\n"))
	var tokens []string
	for sc.Scan() {
		tokens = append(tokens, sc.Text())
	}
	// One empty frame for the CRLF terminator, then the drained LF tail.
	if got, want := fmt.Sprintf("%q", tokens), `["" ""]`; got != want {
		t.Fatalf("tokens = %s, want %s", got, want)
	}
}

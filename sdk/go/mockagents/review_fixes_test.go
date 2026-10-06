package mockagents

// Regression tests for the 2026-10 SDK review findings (K-xx) in the Go SDK.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"
)

// exitImmediatelyEnv makes the test binary act as a server that dies before
// becoming healthy, so Start's early-exit path can be exercised without a
// real mockagents binary.
const exitImmediatelyEnv = "MOCKAGENTS_SDK_TEST_EXIT_IMMEDIATELY"

func TestMain(m *testing.M) {
	if os.Getenv(exitImmediatelyEnv) == "1" {
		fmt.Fprintln(os.Stderr, "fake server: agents dir is invalid")
		os.Exit(3)
	}
	os.Exit(m.Run())
}

// --- K-01: the API key reaches every request path ---

type seenRequest struct {
	path          string
	authorization string
	xAPIKey       string
	model         string
}

func newRecordingServer(t *testing.T) (*httptest.Server, func() []seenRequest) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []seenRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seen = append(seen, seenRequest{
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			xAPIKey:       r.Header.Get("X-Api-Key"),
			model:         body.Model,
		})
		mu.Unlock()
		stream := r.Header.Get("Accept") == "text/event-stream"
		switch {
		case r.URL.Path == "/v1/chat/completions" && stream:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case r.URL.Path == "/v1/messages" && stream:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
		case r.URL.Path == "/v1/chat/completions":
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
		case r.URL.Path == "/v1/messages":
			_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
		case r.URL.Path == "/api/v1/agents":
			_, _ = io.WriteString(w, `[]`)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenRequest(nil), seen...)
	}
}

func drainRaw(t *testing.T, s *RawEventStream, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for s.Next() {
	}
	_ = s.Close()
}

func drainChunks(t *testing.T, s *ChunkStream, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for s.Next() {
	}
	_ = s.Close()
}

func TestAPIKeyIsSentOnEveryRequestPath(t *testing.T) {
	srv, seen := newRecordingServer(t)
	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "tenant-key"})
	ctx := context.Background()
	msgs := []ChatMessage{{Role: "user", Content: "hi"}}

	if _, err := c.Chat(ctx, msgs, ChatOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Message(ctx, msgs, MessageOptions{}); err != nil {
		t.Fatal(err)
	}
	s1, err := c.ChatStream(ctx, msgs, ChatOptions{})
	drainRaw(t, s1, err)
	s2, err := c.MessageStream(ctx, msgs, MessageOptions{})
	drainRaw(t, s2, err)
	s3, err := c.IterStream(ctx, msgs, IterStreamOptions{})
	drainChunks(t, s3, err)
	s4, err := c.IterStream(ctx, msgs, IterStreamOptions{Protocol: "anthropic"})
	drainChunks(t, s4, err)
	if _, err := c.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListAgents(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetAgent(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReloadAgent(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RotateMyAPIKey(ctx); err != nil {
		t.Fatal(err)
	}

	got := seen()
	if len(got) != 11 {
		t.Fatalf("saw %d requests, want 11", len(got))
	}
	for _, r := range got {
		if r.authorization != "Bearer tenant-key" {
			t.Errorf("%s: Authorization = %q", r.path, r.authorization)
		}
		if r.path == "/v1/messages" && r.xAPIKey != "tenant-key" {
			t.Errorf("anthropic call sent X-Api-Key %q, want the configured key", r.xAPIKey)
		}
	}
}

func TestNoAPIKeyKeepsAnthropicPlaceholder(t *testing.T) {
	srv, seen := newRecordingServer(t)
	c := NewClient(ClientOptions{BaseURL: srv.URL})
	s, err := c.MessageStream(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, MessageOptions{})
	drainRaw(t, s, err)
	r := seen()[0]
	if r.xAPIKey != "mock-api-key" || r.authorization != "" {
		t.Fatalf("X-Api-Key=%q Authorization=%q", r.xAPIKey, r.authorization)
	}
}

// --- K-24: one default Anthropic model across SDKs ---

func TestDefaultAnthropicModelMatchesOtherSDKs(t *testing.T) {
	if DefaultAnthropicModel != "claude-sonnet-4-20250514" {
		t.Fatalf("DefaultAnthropicModel = %q", DefaultAnthropicModel)
	}
	srv, seen := newRecordingServer(t)
	c := NewClient(ClientOptions{BaseURL: srv.URL})
	ctx := context.Background()
	msgs := []ChatMessage{{Role: "user", Content: "x"}}
	if _, err := c.Message(ctx, msgs, MessageOptions{}); err != nil {
		t.Fatal(err)
	}
	s, err := c.MessageStream(ctx, msgs, MessageOptions{})
	drainRaw(t, s, err)
	for _, r := range seen() {
		if r.model != DefaultAnthropicModel {
			t.Errorf("%s sent model %q", r.path, r.model)
		}
	}
}

// --- K-03: raw / malformed tool arguments stay observable ---

func TestDecodeArgsKeepsRawAndValidity(t *testing.T) {
	cases := []struct {
		name      string
		wire      string
		wantRaw   string
		wantValid bool
		wantArgs  map[string]any
	}{
		{"openai string object", `"{\"limit\":5}"`, `{"limit":5}`, true, map[string]any{"limit": float64(5)}},
		{"anthropic object", `{"q":null}`, `{"q":null}`, true, map[string]any{"q": nil}},
		{"malformed json string", `"{\"limit\":"`, `{"limit":`, false, nil},
		{"array in string", `"[1]"`, `[1]`, false, nil},
		{"bare array", `[1]`, `[1]`, false, nil},
		{"empty string", `""`, ``, false, nil},
		{"json null", `null`, ``, false, nil},
		{"absent", ``, ``, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, raw, valid := decodeArgs(json.RawMessage(tc.wire))
			if raw != tc.wantRaw || valid != tc.wantValid {
				t.Fatalf("raw=%q valid=%v, want raw=%q valid=%v", raw, valid, tc.wantRaw, tc.wantValid)
			}
			if fmt.Sprintf("%#v", args) != fmt.Sprintf("%#v", tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tc.wantArgs)
			}
		})
	}
}

func TestParsedToolCallsCarryRawArguments(t *testing.T) {
	resp, err := parseOpenAIResponse(json.RawMessage(`{"choices":[{"message":{"tool_calls":[
		{"id":"c1","function":{"name":"f","arguments":"{\"a\":1}"}},
		{"id":"c2","function":{"name":"g","arguments":"{not json"}}]}}]}`), 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	ok, bad := resp.ToolCalls[0], resp.ToolCalls[1]
	if !ok.ArgumentsValid || ok.RawArguments != `{"a":1}` {
		t.Errorf("valid call = %+v", ok)
	}
	if bad.ArgumentsValid || bad.RawArguments != "{not json" || bad.Arguments != nil {
		t.Errorf("malformed call = %+v", bad)
	}
}

// --- K-25: assistant tool_calls round trip ---

func TestAssistantMessageReplaysToolCalls(t *testing.T) {
	resp := &ChatResponse{ToolCalls: []ToolCall{
		{ID: "call_1", Name: "lookup", RawArguments: `{"id": 1}`, Arguments: map[string]any{"id": float64(1)}, ArgumentsValid: true},
		{ID: "call_2", Name: "noargs"},
	}}
	msgs := []ChatMessage{
		AssistantMessage(resp),
		{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true}`},
	}
	buf, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"assistant","content":"","tool_calls":[` +
		`{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"id\": 1}"}},` +
		`{"id":"call_2","type":"function","function":{"name":"noargs","arguments":"{}"}}]},` +
		`{"role":"tool","content":"{\"ok\":true}","tool_call_id":"call_1"}]`
	if string(buf) != want {
		t.Fatalf("wire =\n%s\nwant\n%s", buf, want)
	}
}

// --- K-06: numeric argument comparison ---

// formattingT records fully formatted Errorf messages.
type formattingT struct {
	testing.TB
	errors []string
}

func (f *formattingT) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}
func (f *formattingT) Helper() {}

func TestToHaveToolCallComparesNumbersAsJSON(t *testing.T) {
	// Arguments decoded from the wire hold float64, as encoding/json yields.
	resp := &ChatResponse{ToolCalls: []ToolCall{{Name: "search", Arguments: map[string]any{
		"limit": float64(5), "filter": nil, "ids": []any{float64(1), float64(2)},
	}}}}
	for _, want := range []any{5, int64(5), 5.0, float32(5), json.Number("5"), uint8(5)} {
		ft := &formattingT{}
		Expect(ft, resp).ToHaveToolCall("search", map[string]any{"limit": want})
		if len(ft.errors) != 0 {
			t.Errorf("%T(%v) did not match: %v", want, want, ft.errors)
		}
	}
	ft := &formattingT{}
	Expect(ft, resp).ToHaveToolCall("search", map[string]any{"ids": []int{1, 2}, "filter": nil})
	if len(ft.errors) != 0 {
		t.Errorf("typed slice / explicit nil did not match: %v", ft.errors)
	}
}

func TestToHaveToolCallMissingKeyNeverMatchesNil(t *testing.T) {
	resp := &ChatResponse{ToolCalls: []ToolCall{{Name: "search", Arguments: map[string]any{"limit": float64(5)}}}}
	ft := &formattingT{}
	Expect(ft, resp).ToHaveToolCall("search", map[string]any{"filter": nil})
	if len(ft.errors) != 1 {
		t.Fatalf("missing key matched an expected nil")
	}
	ft = &formattingT{}
	Expect(ft, resp).ToHaveToolCall("search", map[string]any{"limit": 6})
	if len(ft.errors) != 1 || !strings.Contains(ft.errors[0], `map[string]interface {}{"limit":6}`) ||
		!strings.Contains(ft.errors[0], `"limit":5`) {
		t.Fatalf("failure message should print both sides with %%#v: %v", ft.errors)
	}
}

func TestToHaveToolCallCountByName(t *testing.T) {
	resp := &ChatResponse{ToolCalls: []ToolCall{{Name: "a"}, {Name: "b"}, {Name: "a"}}}
	ft := &formattingT{}
	Expect(ft, resp).ToHaveToolCallCountByName("a", 2).ToHaveToolCallCountByName("c", 0)
	if len(ft.errors) != 0 {
		t.Fatalf("unexpected failures: %v", ft.errors)
	}
	Expect(ft, resp).ToHaveToolCallCountByName("b", 2)
	if len(ft.errors) != 1 {
		t.Fatalf("expected one failure, got %v", ft.errors)
	}
}

// --- K-12: every SSE line-ending style frames identically ---

// chunkReader returns the given chunks one Read at a time, so a test can put a
// CRLF or a frame boundary exactly across two reads.
type chunkReader struct{ chunks []string }

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	c.chunks[0] = c.chunks[0][n:]
	if c.chunks[0] == "" {
		c.chunks = c.chunks[1:]
	}
	return n, nil
}

func splitAll(t *testing.T, r io.Reader) []string {
	t.Helper()
	sc := bufio.NewScanner(&newlineNormalizer{r: r})
	sc.Split(splitSSEFrames)
	var frames []string
	for sc.Scan() {
		frames = append(frames, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return frames
}

func TestSSEFramingAcrossLineEndings(t *testing.T) {
	cases := map[string]string{
		"LF":                       "data: a\n\ndata: b\n\n",
		"CRLF":                     "data: a\r\n\r\ndata: b\r\n\r\n",
		"CR":                       "data: a\r\rdata: b\r\r",
		"CRLF then LF":             "data: a\r\n\r\ndata: b\n\n",
		"LF then CRLF (\\n\\r\\n)": "data: a\n\r\ndata: b\n\n",
		"LF frame before CRLF":     "data: a\n\ndata: b\r\n\r\n",
		"CRLF + CR":                "data: a\r\n\rdata: b\r\n\n",
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) {
			for _, r := range []io.Reader{strings.NewReader(wire), iotest.OneByteReader(strings.NewReader(wire))} {
				got := splitAll(t, r)
				if strings.Join(got, "|") != "data: a|data: b" {
					t.Fatalf("frames = %q", got)
				}
			}
		})
	}
}

func TestSSEFramingSplitCRLFIsOneLineEnding(t *testing.T) {
	// "\r" ends one read and "\n" starts the next: ONE line break, so the two
	// data lines stay in a single frame.
	got := splitAll(t, &chunkReader{chunks: []string{"data: a\r", "\ndata: b\r\n\r\n"}})
	if len(got) != 1 || got[0] != "data: a\ndata: b" {
		t.Fatalf("frames = %q", got)
	}
	// A boundary split across reads is still found.
	got = splitAll(t, &chunkReader{chunks: []string{"data: a\r\n", "\r\ndata: b", "\n\n"}})
	if strings.Join(got, "|") != "data: a|data: b" {
		t.Fatalf("frames = %q", got)
	}
	// A read holding only the swallowed LF must not end the stream early.
	got = splitAll(t, &chunkReader{chunks: []string{"data: a\r", "\n", "\r\ndata: b"}})
	if strings.Join(got, "|") != "data: a|data: b" {
		t.Fatalf("frames = %q", got)
	}
}

// --- K-14: truncation and malformed frames are observable ---

func rawSSEServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamFaultsAreObservable(t *testing.T) {
	ctx := context.Background()
	msgs := []ChatMessage{{Role: "user", Content: "x"}}

	t.Run("clean openai stream", func(t *testing.T) {
		srv := rawSSEServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: [DONE]\n\n")
		s, err := NewClient(ClientOptions{BaseURL: srv.URL}).ChatStream(ctx, msgs, ChatOptions{})
		drainRaw(t, s, err)
		if !s.Completed() || s.Truncated() || s.MalformedFrames() != 0 {
			t.Fatalf("completed=%v truncated=%v malformed=%d", s.Completed(), s.Truncated(), s.MalformedFrames())
		}
	})

	t.Run("truncated openai stream with malformed frames", func(t *testing.T) {
		srv := rawSSEServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"+
			"data: {\"choices\":[{\"del\n\ndata: [1]\n\n")
		s, err := NewClient(ClientOptions{BaseURL: srv.URL}).IterStream(ctx, msgs, IterStreamOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var text string
		for s.Next() {
			text += s.Value().Text
		}
		_ = s.Close()
		if text != "Hel" || s.Err() != nil {
			t.Fatalf("text=%q err=%v", text, s.Err())
		}
		if s.Completed() || !s.Truncated() || s.MalformedFrames() != 2 {
			t.Fatalf("completed=%v truncated=%v malformed=%d", s.Completed(), s.Truncated(), s.MalformedFrames())
		}
	})

	t.Run("anthropic stream without message_stop", func(t *testing.T) {
		srv := rawSSEServer(t, "data: {\"type\":\"message_start\"}\n\n")
		s, err := NewClient(ClientOptions{BaseURL: srv.URL}).MessageStream(ctx, msgs, MessageOptions{})
		drainRaw(t, s, err)
		if s.Completed() || !s.Truncated() {
			t.Fatalf("completed=%v truncated=%v", s.Completed(), s.Truncated())
		}
	})

	t.Run("chunk stream stopped at its finished chunk counts as completed", func(t *testing.T) {
		srv := rawSSEServer(t, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		s, err := NewClient(ClientOptions{BaseURL: srv.URL}).IterStream(ctx, msgs, IterStreamOptions{})
		drainChunks(t, s, err)
		if !s.Completed() || s.Truncated() {
			t.Fatalf("completed=%v truncated=%v", s.Completed(), s.Truncated())
		}
	})
}

// --- K-13: the in-process server's documented route subset ---

func TestInProcessClientManagementRoutesAreAbsent(t *testing.T) {
	client, err := NewInProcessClient(InProcessOptions{AgentsDir: writeTempAgent(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.ListAgents(context.Background())
	var herr *HTTPError
	if !errors.As(err, &herr) || herr.Status != http.StatusNotFound {
		t.Fatalf("ListAgents err = %v, want a documented 404", err)
	}
}

// --- K-19: IPv4 loopback ---

func TestServerURLUsesIPv4Loopback(t *testing.T) {
	s := &Server{Port: 12345}
	if s.URL() != "http://127.0.0.1:12345" || s.Client().BaseURL() != "http://127.0.0.1:12345" {
		t.Fatalf("URL = %q", s.URL())
	}
}

// --- K-22: binary discovery ---

func TestFindBinaryEnvMatrix(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "fake-a")
	b := filepath.Join(dir, "fake-b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bare := "mockagents"
	if runtime.GOOS == "windows" {
		bare = "mockagents.exe"
	}
	cases := []struct {
		name, binary, bin, want string
	}{
		{"MOCKAGENTS_BINARY only", a, "", a},
		{"MOCKAGENTS_BIN only", "", b, b},
		{"both: MOCKAGENTS_BINARY wins", a, b, a},
		{"MOCKAGENTS_BINARY missing: MOCKAGENTS_BIN used", filepath.Join(dir, "nope"), b, b},
		{"a directory is not a binary", dir, "", bare},
		{"neither", "", "", bare},
	}
	t.Chdir(t.TempDir()) // no ./mockagents in the working directory
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MOCKAGENTS_BINARY", tc.binary)
			t.Setenv("MOCKAGENTS_BIN", tc.bin)
			if got := FindBinary(); got != tc.want {
				t.Fatalf("FindBinary() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindBinaryIgnoresParentDirectories(t *testing.T) {
	t.Setenv("MOCKAGENTS_BINARY", "")
	t.Setenv("MOCKAGENTS_BIN", "")
	name := "mockagents"
	if runtime.GOOS == "windows" {
		name = "mockagents.exe"
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "sdk", "go")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if got := FindBinary(); got != name {
		t.Fatalf("FindBinary() = %q, want the bare name (no ancestor search)", got)
	}
	t.Chdir(root)
	if got := FindBinary(); got != filepath.Join(root, name) {
		t.Fatalf("FindBinary() = %q, want ./%s in the working directory", got, name)
	}
}

// --- K-23: an early child exit fails Start fast ---

func TestServerStartFailsFastWhenChildExits(t *testing.T) {
	t.Setenv(exitImmediatelyEnv, "1")
	srv, err := NewServer(ServerOptions{BinaryPath: os.Args[0], AgentsDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = srv.Start(context.Background(), 20*time.Second)
	if err == nil {
		_ = srv.Stop(time.Second)
		t.Fatal("Start succeeded against a child that exits immediately")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Start took %s; it should fail as soon as the child exits", elapsed)
	}
	if !strings.Contains(err.Error(), "exited before becoming ready") ||
		!strings.Contains(err.Error(), "agents dir is invalid") {
		t.Fatalf("err = %v", err)
	}
	if srv.IsRunning() {
		t.Fatal("server reports running after a failed start")
	}
}

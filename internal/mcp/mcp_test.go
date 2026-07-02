package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// testDef builds a fully populated MCPServer definition used across the
// dispatch and transport tests. Kept compact on purpose so each test can
// focus on a single method.
func testDef() *types.MCPServerDefinition {
	return &types.MCPServerDefinition{
		APIVersion: types.AgentAPIVersion,
		Kind:       types.MCPServerKind,
		Metadata:   types.Metadata{Name: "weather-mcp"},
		Spec: types.MCPServerSpec{
			Tools: []types.MCPTool{{
				Name:        "get_forecast",
				Description: "Mock weather forecast",
				InputSchema: types.JSONSchemaObject{"type": "object"},
				Responses: []types.MCPToolResponse{
					{
						Match: map[string]any{"city": "tokyo"},
						Content: []types.MCPContentBlock{
							{Type: "text", Text: "sunny, 22C"},
						},
					},
					{
						Default: true,
						Content: []types.MCPContentBlock{
							{Type: "text", Text: "overcast, 15C"},
						},
					},
				},
			}},
			Resources: []types.MCPResource{{
				URI:      "mock://weather/today",
				Name:     "Today's Forecast",
				MimeType: "text/plain",
				Text:     "clear skies",
			}},
			Prompts: []types.MCPPrompt{{
				Name:        "greet",
				Description: "say hello",
				Arguments: []types.MCPPromptArg{
					{Name: "name", Required: true},
				},
				Messages: []types.MCPPromptMessage{{
					Role:    "user",
					Content: types.MCPContentBlock{Type: "text", Text: "hello {{name}}"},
				}},
			}},
		},
	}
}

func mustHandle(t *testing.T, srv *Server, payload string) *Response {
	t.Helper()
	var req Request
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return srv.Handle(&req)
}

func TestInitializeAdvertisesCapabilities(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	if !bytes.Contains(body, []byte("\"tools\"")) ||
		!bytes.Contains(body, []byte("\"resources\"")) ||
		!bytes.Contains(body, []byte("\"prompts\"")) {
		t.Errorf("initialize did not advertise expected capabilities: %s", body)
	}
	if !bytes.Contains(body, []byte(types.DefaultMCPProtocolVersion)) {
		t.Errorf("protocolVersion missing: %s", body)
	}
}

func TestPingReturnsEmptyResult(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	if resp.Error != nil {
		t.Fatalf("ping error: %v", resp.Error)
	}
}

func TestToolsListReturnsEntries(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	body, _ := json.Marshal(resp.Result)
	if !bytes.Contains(body, []byte("get_forecast")) {
		t.Errorf("tools/list missing tool: %s", body)
	}
}

func TestToolsCallMatchesSpecificResponse(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_forecast","arguments":{"city":"tokyo"}}}`)
	if resp.Error != nil {
		t.Fatalf("tools/call error: %v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	if !bytes.Contains(body, []byte("sunny, 22C")) {
		t.Errorf("match-based response not returned: %s", body)
	}
}

func TestToolsCallFallsBackToDefault(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_forecast","arguments":{"city":"paris"}}}`)
	body, _ := json.Marshal(resp.Result)
	if !bytes.Contains(body, []byte("overcast, 15C")) {
		t.Errorf("default response not returned: %s", body)
	}
}

// An unknown tool NAME is Invalid params per the spec — the METHOD
// tools/call exists (round-10 R10-10 overturned the old -32601 here).
func TestToolsCallUnknownToolReturnsInvalidParams(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`)
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrInvalidParams)
	}
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":6,"method":"completely/unknown"}`)
	if resp.Error == nil || resp.Error.Code != ErrMethodNotFound {
		t.Fatalf("expected MethodNotFound, got %+v", resp.Error)
	}
}

func TestResourcesReadHitAndMiss(t *testing.T) {
	srv := NewServer(testDef())
	hit := mustHandle(t, srv, `{"jsonrpc":"2.0","id":8,"method":"resources/read","params":{"uri":"mock://weather/today"}}`)
	if hit.Error != nil {
		t.Fatalf("hit error: %v", hit.Error)
	}
	body, _ := json.Marshal(hit.Result)
	if !bytes.Contains(body, []byte("clear skies")) {
		t.Errorf("resource text missing: %s", body)
	}
	miss := mustHandle(t, srv, `{"jsonrpc":"2.0","id":9,"method":"resources/read","params":{"uri":"mock://nope"}}`)
	if miss.Error == nil {
		t.Fatal("expected error for unknown resource")
	}
}

func TestPromptsGetExpandsArguments(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":10,"method":"prompts/get","params":{"name":"greet","arguments":{"name":"Ada"}}}`)
	body, _ := json.Marshal(resp.Result)
	if !bytes.Contains(body, []byte("hello Ada")) {
		t.Errorf("arg expansion failed: %s", body)
	}
}

func TestNotificationReturnsNoResponse(t *testing.T) {
	srv := NewServer(testDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if resp != nil {
		t.Errorf("notification should return nil, got %+v", resp)
	}
}

func TestHandleBytesInvalidJSONReturnsParseError(t *testing.T) {
	srv := NewServer(testDef())
	out, err := srv.HandleBytes([]byte("not json"))
	if err != nil {
		t.Fatalf("HandleBytes: %v", err)
	}
	if !bytes.Contains(out, []byte("-32700")) {
		t.Errorf("expected parse error code in %s", out)
	}
}

// --- HTTP transport ---

func TestHTTPHandlerRoundTrip(t *testing.T) {
	srv := NewServer(testDef())
	ts := httptest.NewServer(NewHTTPHandler(srv))
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("get_forecast")) {
		t.Errorf("missing tool: %s", body)
	}
}

func TestHTTPHandlerRejectsGET(t *testing.T) {
	srv := NewServer(testDef())
	ts := httptest.NewServer(NewHTTPHandler(srv))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestHTTPHandlerNotificationReturns204(t *testing.T) {
	srv := NewServer(testDef())
	ts := httptest.NewServer(NewHTTPHandler(srv))
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

// --- stdio transport ---

func TestServeStdioRoundTrip(t *testing.T) {
	srv := NewServer(testDef())

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_forecast","arguments":{"city":"tokyo"}}}` + "\n",
	)
	var out bytes.Buffer
	if err := ServeStdio(srv, in, &out); err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}

	// Expect exactly two responses (initialize + tools/call); the
	// notification must not produce output.
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d: %s", len(lines), out.String())
	}
	if !bytes.Contains(lines[0], []byte("protocolVersion")) {
		t.Errorf("first response missing protocolVersion: %s", lines[0])
	}
	if !bytes.Contains(lines[1], []byte("sunny, 22C")) {
		t.Errorf("second response missing tool result: %s", lines[1])
	}
}

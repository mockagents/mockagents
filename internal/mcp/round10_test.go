// Round-10 fidelity regression tests (2026-07-02 eval — official MCP SDK in
// the loop): version negotiation, SDK-breaking nil/omitempty shapes, the
// 202-for-responses rule, and capability advertisement.
package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

func round10Server() *Server {
	return NewServer(&types.MCPServerDefinition{
		Kind:     types.MCPServerKind,
		Metadata: types.Metadata{Name: "r10"},
		Spec: types.MCPServerSpec{
			Tools: []types.MCPTool{{Name: "schemaless", Description: "no schema"}},
			Resources: []types.MCPResource{
				{URI: "mock://unnamed", Text: "body"},
			},
			Prompts: []types.MCPPrompt{{Name: "greet"}},
		},
	})
}

func handleJSON(t *testing.T, s *Server, body string) map[string]any {
	t.Helper()
	out, err := s.HandleBytes([]byte(body))
	if err != nil {
		t.Fatalf("HandleBytes: %v", err)
	}
	if out == nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("response not JSON: %v (%s)", err, out)
	}
	return m
}

// R10-2 (S2): the server MUST echo a supported requested protocolVersion —
// always answering the newest broke every pre-2025-11-25 official SDK.
func TestInitializeEchoesSupportedVersion(t *testing.T) {
	s := round10Server()
	resp := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize",
		"params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the client's supported 2025-06-18 echoed", result["protocolVersion"])
	}

	// An unsupported version falls back to the server's default.
	resp = handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"initialize",
		"params":{"protocolVersion":"1999-01-01"}}`)
	if v := resp["result"].(map[string]any)["protocolVersion"]; v != types.DefaultMCPProtocolVersion {
		t.Errorf("unsupported version → %v, want the default %s", v, types.DefaultMCPProtocolVersion)
	}
}

// R10-3/R10-13 (S2): completion never emits values:null, ref/resource
// matches by uri, bogus ref types are rejected, and the completions +
// listChanged capabilities are advertised.
func TestCompletionShapesAndCapabilities(t *testing.T) {
	s := round10Server()

	// Catalog miss (the out-of-box case) → an EMPTY ARRAY, never null.
	resp := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"completion/complete",
		"params":{"ref":{"type":"ref/prompt","name":"greet"},"argument":{"name":"who","value":""}}}`)
	raw, _ := json.Marshal(resp["result"])
	if strings.Contains(string(raw), `"values":null`) || !strings.Contains(string(raw), `"values":[]`) {
		t.Errorf("completion miss = %s, want \"values\":[]", raw)
	}

	// Bogus ref type → invalid params.
	resp = handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"completion/complete",
		"params":{"ref":{"type":"ref/banana","name":"x"},"argument":{"name":"a"}}}`)
	if code := resp["error"].(map[string]any)["code"]; code != float64(ErrInvalidParams) {
		t.Errorf("bogus ref.type code = %v, want -32602", code)
	}

	// Capabilities: completions always declared; listChanged on the features.
	resp = handleJSON(t, s, `{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	caps := resp["result"].(map[string]any)["capabilities"].(map[string]any)
	if _, ok := caps["completions"]; !ok {
		t.Error("completions capability not advertised despite completion/complete being implemented")
	}
	if tools, ok := caps["tools"].(map[string]any); !ok || tools["listChanged"] != true {
		t.Errorf("tools capability = %v, want listChanged:true", caps["tools"])
	}
}

// R10-3 (S2): a spec-shaped ref/resource carries `uri`; catalog entries keyed
// by RefName must match it.
func TestCompletionResourceRefByURI(t *testing.T) {
	s := NewServer(&types.MCPServerDefinition{
		Kind:     types.MCPServerKind,
		Metadata: types.Metadata{Name: "r10c"},
		Spec: types.MCPServerSpec{
			Completions: []types.MCPCompletion{{
				RefType: "ref/resource", RefName: "mock://data", ArgName: "path",
				Values: []string{"a.txt", "b.txt"},
			}},
		},
	})
	resp := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"completion/complete",
		"params":{"ref":{"type":"ref/resource","uri":"mock://data"},"argument":{"name":"path","value":""}}}`)
	raw, _ := json.Marshal(resp["result"])
	if !strings.Contains(string(raw), "a.txt") {
		t.Errorf("uri-addressed resource completion missed the catalog: %s", raw)
	}
}

// R10-5/R10-6 (S2): required-by-spec members are always present — a
// schema-less tool carries inputSchema {"type":"object"} and a nameless
// resource carries name (defaulted to the URI). One missing member
// previously poisoned the ENTIRE list for strict SDK clients.
func TestRequiredListShapes(t *testing.T) {
	s := round10Server()

	resp := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, _ := json.Marshal(resp["result"])
	if !strings.Contains(string(raw), `"inputSchema":{"type":"object"}`) {
		t.Errorf("schema-less tool missing the defaulted inputSchema: %s", raw)
	}

	resp = handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"resources/list"}`)
	raw, _ = json.Marshal(resp["result"])
	if !strings.Contains(string(raw), `"name":"mock://unnamed"`) {
		t.Errorf("nameless resource missing the defaulted name: %s", raw)
	}
}

// R10-7 (S2): a client RESPONSE body (no method) is acked silently (the
// transports answer 202) instead of being dispatched as a bogus request.
func TestClientResponseBodiesNotDispatched(t *testing.T) {
	s := round10Server()
	out, err := s.HandleBytes([]byte(`{"jsonrpc":"2.0","id":99,"result":{"ok":true}}`))
	if err != nil {
		t.Fatalf("HandleBytes: %v", err)
	}
	if out != nil {
		t.Errorf("response-shaped body earned a reply: %s (want none → transport 202)", out)
	}
	out, _ = s.HandleBytes([]byte(`{"jsonrpc":"2.0","id":100,"error":{"code":-1,"message":"client failed"}}`))
	if out != nil {
		t.Errorf("error-response body earned a reply: %s", out)
	}
}

// R10-10/11 (S2): spec error codes — unknown tool name is -32602 (the
// METHOD exists), unknown resource URI is MCP's -32002 with data.uri.
func TestSpecErrorCodes(t *testing.T) {
	s := round10Server()

	resp := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
		"params":{"name":"no-such-tool"}}`)
	if code := resp["error"].(map[string]any)["code"]; code != float64(ErrInvalidParams) {
		t.Errorf("unknown tool code = %v, want -32602", code)
	}

	resp = handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"resources/read",
		"params":{"uri":"mock://nope"}}`)
	errObj := resp["error"].(map[string]any)
	if errObj["code"] != float64(ErrResourceNotFound) {
		t.Errorf("unknown resource code = %v, want -32002", errObj["code"])
	}
	if data, _ := errObj["data"].(map[string]any); data["uri"] != "mock://nope" {
		t.Errorf("unknown resource data = %v, want uri:mock://nope", errObj["data"])
	}
}

// R10-12 (S2): a batch (JSON array) is well-formed JSON that MCP 2025-06-18
// removed — Invalid Request (-32600), not a parse error (-32700).
func TestBatchBodiesAreInvalidRequest(t *testing.T) {
	s := round10Server()
	out, err := s.HandleBytes([]byte(` [{"jsonrpc":"2.0","id":1,"method":"ping"}]`))
	if err != nil {
		t.Fatalf("HandleBytes: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("bad response: %v", err)
	}
	if code := resp["error"].(map[string]any)["code"]; code != float64(ErrInvalidRequest) {
		t.Errorf("batch body code = %v, want -32600", code)
	}
	if !strings.Contains(string(out), `"id":null`) {
		t.Errorf("batch rejection missing id:null: %s", out)
	}
}

// R10-14 (S2): prompts/get without a REQUIRED argument is -32602 —
// previously the placeholder was silently left unexpanded.
func TestPromptsGetMissingRequiredArgument(t *testing.T) {
	s := NewServer(&types.MCPServerDefinition{
		Kind:     types.MCPServerKind,
		Metadata: types.Metadata{Name: "r10p"},
		Spec: types.MCPServerSpec{
			Prompts: []types.MCPPrompt{{
				Name:      "greet",
				Arguments: []types.MCPPromptArg{{Name: "who", Required: true}},
				Messages: []types.MCPPromptMessage{{
					Role:    "user",
					Content: types.MCPContentBlock{Type: "text", Text: "hi {{who}}"},
				}},
			}},
		},
	})
	resp := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"prompts/get",
		"params":{"name":"greet"}}`)
	if code := resp["error"].(map[string]any)["code"]; code != float64(ErrInvalidParams) {
		t.Errorf("missing required arg code = %v, want -32602", code)
	}

	// Supplying it still renders.
	resp = handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"prompts/get",
		"params":{"name":"greet","arguments":{"who":"ada"}}}`)
	raw, _ := json.Marshal(resp["result"])
	if !strings.Contains(string(raw), "hi ada") {
		t.Errorf("prompt did not render: %s", raw)
	}
}

// R10-16 (S3): the mock never issues a nextCursor, so any non-empty cursor
// on a list method is unknown → -32602 (silently re-sending page 1 traps a
// paginating client in an infinite loop).
func TestUnknownListCursorRejected(t *testing.T) {
	s := round10Server()
	for id, method := range map[int]string{1: "tools/list", 2: "resources/list", 3: "prompts/list"} {
		resp := handleJSON(t, s, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"method":"%s","params":{"cursor":"opaque123"}}`, id, method))
		errObj, ok := resp["error"].(map[string]any)
		if !ok || errObj["code"] != float64(ErrInvalidParams) {
			t.Errorf("%s with bogus cursor = %v, want -32602", method, resp)
		}
	}
}

// R10-8 (S3): ANY id-less request is a notification and gets no reply —
// previously only unknown methods were guarded, so an id-less ping or
// tools/list earned an orphaned response.
func TestKnownMethodNotificationsGetNoReply(t *testing.T) {
	s := round10Server()
	for _, body := range []string{
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
	} {
		out, err := s.HandleBytes([]byte(body))
		if err != nil {
			t.Fatalf("HandleBytes(%s): %v", body, err)
		}
		if out != nil {
			t.Errorf("notification %s earned a reply: %s", body, out)
		}
	}
}

// R10-20 (S2): the spec allows AT MOST ONE server→client GET stream per
// session; a second concurrent GET MUST be 409 Conflict (previously up to
// 64 streams each received a copy of every event, which the spec forbids).
func TestStreamableSecondGetStreamIs409(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	sid := initSession(t, srv.URL+"/mcp")

	open := func() *http.Response {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mcp", nil)
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set(headerSessionID, sid)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET stream: %v", err)
		}
		return resp
	}

	first := open()
	defer first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first GET stream status = %d, want 200", first.StatusCode)
	}

	second := open()
	defer second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Errorf("second concurrent GET status = %d, want 409", second.StatusCode)
	}

	// Releasing the first stream frees the slot (resumability still works);
	// the server notices the disconnect asynchronously, hence the retry.
	first.Body.Close()
	for i := 0; ; i++ {
		third := open()
		code := third.StatusCode
		third.Body.Close()
		if code == http.StatusOK {
			break
		}
		if i > 100 {
			t.Fatalf("GET after release still %d after retries, want 200", code)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// R10-12 on the streamable transport: a batch array POST earns -32600
// before session validation, not a -32700 parse error.
func TestStreamableBatchPostIsInvalidRequest(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	resp := postJSON(t, srv.URL+"/mcp", "", "application/json",
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)
	defer resp.Body.Close()
	var body bytes.Buffer
	_, _ = body.ReadFrom(resp.Body)
	if !strings.Contains(body.String(), `-32600`) {
		t.Errorf("batch POST body = %s, want -32600", body.String())
	}
}

// R10-22 (S2): an over-long stdio frame earns a -32700 and the loop keeps
// serving — previously bufio.Scanner hit ErrTooLong and the process exited,
// killing the session.
func TestStdioOverlongFrameRecovers(t *testing.T) {
	s := round10Server()
	var in bytes.Buffer
	in.WriteString(strings.Repeat("x", maxStdioFrameBytes+2) + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":7,"method":"ping"}` + "\n")

	var out bytes.Buffer
	if err := ServeStdio(s, &in, &out); err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d output lines, want 2 (parse error + ping reply): %q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `-32700`) || !strings.Contains(lines[0], `"id":null`) {
		t.Errorf("over-long frame reply = %s, want -32700 with id:null", lines[0])
	}
	if !strings.Contains(lines[1], `"id":7`) {
		t.Errorf("follow-up ping reply = %s, want id:7 (loop must continue)", lines[1])
	}
}

// R10-9 (S3): when the request id is undeterminable (parse error), the
// error response must carry "id": null — the member is required and was
// previously omitted entirely.
func TestParseErrorCarriesNullID(t *testing.T) {
	s := round10Server()
	out, err := s.HandleBytes([]byte(`{not json`))
	if err != nil {
		t.Fatalf("HandleBytes: %v", err)
	}
	if !strings.Contains(string(out), `"id":null`) {
		t.Errorf("parse-error response = %s, want \"id\":null present", out)
	}
}

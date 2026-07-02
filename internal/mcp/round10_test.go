// Round-10 fidelity regression tests (2026-07-02 eval — official MCP SDK in
// the loop): version negotiation, SDK-breaking nil/omitempty shapes, the
// 202-for-responses rule, and capability advertisement.
package mcp

import (
	"encoding/json"
	"strings"
	"testing"

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

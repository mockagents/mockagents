// Round-11 strict-tools regression tests: MCP tools/call argument
// validation vs inputSchema (closes R10-19). Ground truth: the spec says
// servers MUST validate tool inputs; per the 2025-11-25 revision (and the
// measured official FastMCP server) invalid argument VALUES yield an
// isError:true execution result — never -32602 — and extra arguments on a
// permissive schema stay accepted.
package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func round11Server(strictArgs *bool) *Server {
	return NewServer(&types.MCPServerDefinition{
		Kind:     types.MCPServerKind,
		Metadata: types.Metadata{Name: "r11"},
		Spec: types.MCPServerSpec{
			StrictArgs: strictArgs,
			Tools: []types.MCPTool{
				{
					Name: "get_forecast",
					InputSchema: types.JSONSchemaObject{
						"type":     "object",
						"required": []any{"city"},
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
							"days": map[string]any{"type": "integer"},
						},
					},
					Responses: []types.MCPToolResponse{{
						Default: true,
						Content: []types.MCPContentBlock{{Type: "text", Text: "overcast, 15C"}},
					}},
				},
				{Name: "schemaless"},
			},
		},
	})
}

func callTool(t *testing.T, s *Server, argsJSON string) (result map[string]any, rpcErr map[string]any) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_forecast","arguments":` + argsJSON + `}}`
	out, err := s.HandleBytes([]byte(body))
	if err != nil {
		t.Fatalf("HandleBytes: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("bad response: %v", err)
	}
	result, _ = resp["result"].(map[string]any)
	rpcErr, _ = resp["error"].(map[string]any)
	return result, rpcErr
}

// Invalid argument VALUES → isError:true execution result with actionable,
// path-qualified feedback; NEVER a -32602 protocol error.
func TestMCPToolCallArgsValidatedByDefault(t *testing.T) {
	s := round11Server(nil) // default = strict

	cases := []struct {
		name, args, wantSubstr string
	}{
		{"missing required", `{}`, `missing required parameter "city"`},
		{"wrong type", `{"city":123}`, "expected string"},
		{"wrong nested type", `{"city":"tokyo","days":"three"}`, "expected integer"},
	}
	for _, tc := range cases {
		result, rpcErr := callTool(t, s, tc.args)
		if rpcErr != nil {
			t.Errorf("%s: got protocol error %v, want isError result (2025-11-25: values are execution errors)", tc.name, rpcErr)
			continue
		}
		if result["isError"] != true {
			t.Errorf("%s: isError = %v, want true (result: %v)", tc.name, result["isError"], result)
			continue
		}
		blocks, _ := result["content"].([]any)
		if len(blocks) == 0 {
			t.Errorf("%s: no content blocks", tc.name)
			continue
		}
		text, _ := blocks[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, tc.wantSubstr) {
			t.Errorf("%s: content %q missing actionable text %q", tc.name, text, tc.wantSubstr)
		}
	}
}

// Extra arguments on a schema WITHOUT additionalProperties:false stay
// accepted — the official SDK tolerates them; strictness is schema-driven.
func TestMCPToolCallExtraArgsStayLenient(t *testing.T) {
	s := round11Server(nil)
	result, rpcErr := callTool(t, s, `{"city":"tokyo","bogus":"extra"}`)
	if rpcErr != nil || result["isError"] == true {
		t.Errorf("extra arg rejected (result=%v err=%v), want accepted", result, rpcErr)
	}

	// Valid args still resolve the canned response.
	result, _ = callTool(t, s, `{"city":"paris"}`)
	raw, _ := json.Marshal(result)
	if !strings.Contains(string(raw), "overcast, 15C") {
		t.Errorf("valid call did not resolve: %s", raw)
	}

	// A schema-less tool accepts anything.
	out, _ := s.HandleBytes([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"schemaless","arguments":{"anything":1}}}`))
	if strings.Contains(string(out), `"isError":true`) {
		t.Errorf("schema-less tool rejected args: %s", out)
	}
}

// spec.strictArgs: false restores the old accept-anything behavior.
func TestMCPToolCallStrictArgsOptOut(t *testing.T) {
	off := false
	s := round11Server(&off)
	result, rpcErr := callTool(t, s, `{"city":123}`)
	if rpcErr != nil || result["isError"] == true {
		t.Errorf("strictArgs:false still rejected (result=%v err=%v)", result, rpcErr)
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// JSON-RPC envelope rules from the 2026-10-06 review (P-12): a null id and a
// missing method are Invalid Request (-32600), not a dropped notification or
// method-not-found.
func TestEnvelope_NullIDAndMissingMethod(t *testing.T) {
	s := newTestMCPServer()
	for name, body := range map[string]string{
		"null id":        `{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		"missing method": `{"jsonrpc":"2.0","id":7}`,
	} {
		out, err := s.HandleBytes([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `"code":-32600`) {
			t.Errorf("%s: response %s, want -32600", name, out)
		}
	}
	if out, _ := s.HandleBytes([]byte(`{"jsonrpc":"2.0","method":"ping"}`)); len(out) != 0 {
		t.Errorf("a notification must not be answered, got %s", out)
	}
}

// Streamable HTTP answers a malformed body with HTTP 400 plus the JSON-RPC
// error, as the official SDK servers do.
func TestEnvelope_StreamableParseErrorIs400(t *testing.T) {
	srv, _ := newStreamableTestServer(t)
	for _, body := range []string{`{not json`, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %q: status %d, want 400", body, resp.StatusCode)
		}
	}
}

// Well-formed JSON that is not a Request object (a bare number, string, or
// bool, or a field of the wrong type) is Invalid Request (-32600); Parse
// error (-32700) is reserved for bodies that are not JSON at all (JSON-RPC
// 2.0 §5.1). Both the HandleBytes path and Streamable HTTP agree.
func TestEnvelope_NonObjectJSONIsInvalidRequest(t *testing.T) {
	s := newTestMCPServer()
	srv, _ := newStreamableTestServer(t)
	for body, want := range map[string]string{
		`123`:                                 `"code":-32600`,
		`"x"`:                                 `"code":-32600`,
		`true`:                                `"code":-32600`,
		`{"jsonrpc":"2.0","id":1,"method":7}`: `"code":-32600`,
		`{not json`:                           `"code":-32700`,
		`12 34`:                               `"code":-32700`,
	} {
		out, err := s.HandleBytes([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), want) || !strings.Contains(string(out), `"id":null`) {
			t.Errorf("HandleBytes(%s) = %s, want %s with id:null", body, out, want)
		}

		resp := postJSON(t, srv.URL+"/mcp", "", "application/json, text/event-stream", body)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), want) {
			t.Errorf("streamable POST %s: status %d body %s, want 400 with %s", body, resp.StatusCode, raw, want)
		}
	}
}

// resources/subscribe rejects an undeclared URI with -32002 (review P-13).
func TestResourcesSubscribe_UnknownURI(t *testing.T) {
	s := NewServer(&types.MCPServerDefinition{
		Metadata: types.Metadata{Name: "r"},
		Spec: types.MCPServerSpec{
			Capabilities: types.MCPCapabilities{Resources: true},
			Resources:    []types.MCPResource{{URI: "file:///known", Name: "known"}},
		},
	})
	call := func(uri string) string {
		params, _ := json.Marshal(map[string]string{"uri": uri})
		out, _ := s.HandleBytes([]byte(`{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":` + string(params) + `}`))
		return string(out)
	}
	if out := call("file:///unknown"); !strings.Contains(out, `"code":-32002`) {
		t.Errorf("unknown uri: %s", out)
	}
	if out := call("file:///known"); strings.Contains(out, `"error"`) {
		t.Errorf("known uri: %s", out)
	}
}

// A panicking programmatic tool handler becomes an internal error instead of
// unwinding through the transport (audit L-38).
func TestToolHandlerPanicIsRecovered(t *testing.T) {
	s := newTestMCPServer()
	s.RegisterTool(types.MCPTool{Name: "boom"}, func(context.Context, map[string]any) (ToolResult, error) {
		panic("kaboom")
	})
	out, err := s.HandleBytes([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom","arguments":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "panicked") || !strings.Contains(string(out), `"code":-32603`) {
		t.Fatalf("response %s, want an internal error naming the panic", out)
	}
}

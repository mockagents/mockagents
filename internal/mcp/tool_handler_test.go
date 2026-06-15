package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// echoTool is a trivial programmatic tool used to exercise the handler hook.
func echoSpec(name string) types.MCPTool {
	return types.MCPTool{
		Name:        name,
		Description: "echoes its input",
		InputSchema: types.JSONSchemaObject{"type": "object"},
	}
}

func TestRegisterTool_ListedAndDispatched(t *testing.T) {
	srv := NewServer(testDef())
	srv.RegisterTool(echoSpec("echo"), func(_ context.Context, args map[string]any) (ToolResult, error) {
		return ToolResult{Content: []types.MCPContentBlock{{Type: "text", Text: "echo:" + args["msg"].(string)}}}, nil
	})

	// tools/list includes both the declarative and the programmatic tool.
	list := mustHandle(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	body, _ := json.Marshal(list.Result)
	if !bytes.Contains(body, []byte("get_forecast")) || !bytes.Contains(body, []byte("echo")) {
		t.Fatalf("tools/list should contain both tools: %s", body)
	}

	// tools/call dispatches to the Go handler.
	call := mustHandle(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hi"}}}`)
	if call.Error != nil {
		t.Fatalf("unexpected error: %v", call.Error)
	}
	cb, _ := json.Marshal(call.Result)
	if !bytes.Contains(cb, []byte("echo:hi")) {
		t.Errorf("handler result not returned: %s", cb)
	}
}

func TestRegisterTool_DomainErrorIsResultNotRPCError(t *testing.T) {
	srv := NewServer(testDef())
	srv.RegisterTool(echoSpec("boom"), func(_ context.Context, _ map[string]any) (ToolResult, error) {
		return ToolResult{Content: []types.MCPContentBlock{{Type: "text", Text: "nope"}}, IsError: true}, nil
	})
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"boom","arguments":{}}}`)
	// A domain failure is a successful JSON-RPC response with isError:true,
	// NOT a protocol error — so the client/LLM sees it in-band.
	if resp.Error != nil {
		t.Fatalf("domain error must not be a JSON-RPC error: %v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	if !bytes.Contains(body, []byte("\"isError\":true")) {
		t.Errorf("result should carry isError:true: %s", body)
	}
}

func TestRegisterTool_GoErrorMapsToRPCError(t *testing.T) {
	srv := NewServer(testDef())
	srv.RegisterTool(echoSpec("fault"), func(_ context.Context, _ map[string]any) (ToolResult, error) {
		return ToolResult{}, errors.New("internal boom")
	})
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fault","arguments":{}}}`)
	if resp.Error == nil {
		t.Fatal("an unexpected Go error must map to a JSON-RPC error")
	}
	if resp.Error.Code != ErrInternal {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrInternal)
	}
}

func TestRegisterTool_OverridesDeclarativeOfSameName(t *testing.T) {
	srv := NewServer(testDef())
	// Register a handler with the same name as the declarative get_forecast.
	srv.RegisterTool(echoSpec("get_forecast"), func(_ context.Context, _ map[string]any) (ToolResult, error) {
		return ToolResult{Content: []types.MCPContentBlock{{Type: "text", Text: "from-handler"}}}, nil
	})

	// tools/list must not show get_forecast twice.
	list := mustHandle(t, srv, `{"jsonrpc":"2.0","id":5,"method":"tools/list"}`)
	var listRes struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	lb, _ := json.Marshal(list.Result)
	if err := json.Unmarshal(lb, &listRes); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	count := 0
	for _, tl := range listRes.Tools {
		if tl.Name == "get_forecast" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("get_forecast listed %d times, want 1: %s", count, lb)
	}

	// tools/call routes to the handler, not the declarative response.
	call := mustHandle(t, srv, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_forecast","arguments":{"city":"tokyo"}}}`)
	cb, _ := json.Marshal(call.Result)
	if !bytes.Contains(cb, []byte("from-handler")) || bytes.Contains(cb, []byte("sunny, 22C")) {
		t.Errorf("handler should override declarative tool: %s", cb)
	}
}

func TestRegisterTool_InitializeAdvertisesToolsCapabilityWithoutDeclarativeTools(t *testing.T) {
	// A def with no declarative tools, but a registered handler, must still
	// advertise the tools capability so a client knows to call tools/list.
	def := &types.MCPServerDefinition{
		Kind:     types.MCPServerKind,
		Metadata: types.Metadata{Name: "admin"},
		Spec:     types.MCPServerSpec{},
	}
	srv := NewServer(def)
	srv.RegisterTool(echoSpec("only_tool"), func(_ context.Context, _ map[string]any) (ToolResult, error) {
		return ToolResult{}, nil
	})
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":7,"method":"initialize","params":{}}`)
	body, _ := json.Marshal(resp.Result)
	if !bytes.Contains(body, []byte("\"tools\"")) {
		t.Errorf("initialize should advertise tools capability when a handler is registered: %s", body)
	}
}

func TestRegisterTool_IgnoresBlankNameOrNilHandler(t *testing.T) {
	srv := NewServer(testDef())
	srv.RegisterTool(echoSpec(""), func(_ context.Context, _ map[string]any) (ToolResult, error) { return ToolResult{}, nil })
	srv.RegisterTool(echoSpec("x"), nil)
	if srv.hasToolHandlers() {
		t.Error("blank-name and nil-handler registrations must be ignored")
	}
}

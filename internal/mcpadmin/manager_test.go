package mcpadmin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/mcp"
	"github.com/mockagents/mockagents/internal/types"
)

// minimalDef is a bare MCPServer definition with no declarative tools — the
// management tools are added programmatically by the Manager.
func minimalDef() *types.MCPServerDefinition {
	return &types.MCPServerDefinition{
		Kind:     types.MCPServerKind,
		Metadata: types.Metadata{Name: "mockagents-admin"},
	}
}

// rig wires a Manager onto a live MCP server over a temp agents dir.
type rig struct {
	srv      *mcp.Server
	registry *engine.AgentRegistry
	dir      string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	reg := engine.NewAgentRegistry()
	srv := mcp.NewServer(minimalDef())
	NewManager(reg, dir, "").Register(srv)
	return &rig{srv: srv, registry: reg, dir: dir}
}

// call invokes a tool through the server's JSON-RPC dispatch and returns the
// concatenated text content plus the isError flag.
func (r *rig) call(t *testing.T, name string, args map[string]any) (string, bool) {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	resp := r.srv.Handle(&mcp.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: params})
	if resp.Error != nil {
		t.Fatalf("tool %q returned JSON-RPC error: %v", name, resp.Error)
	}
	rb, _ := json.Marshal(resp.Result)
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		t.Fatalf("decode tool result: %v (%s)", err, rb)
	}
	var sb strings.Builder
	for _, c := range out.Content {
		sb.WriteString(c.Text)
	}
	return sb.String(), out.IsError
}

func agentYAML(name, model string) string {
	return strings.Join([]string{
		"apiVersion: mockagents/v1",
		"kind: Agent",
		"metadata:",
		"  name: " + name,
		"spec:",
		"  protocol: openai-chat-completions",
		"  model: " + model,
		"  behavior:",
		"    scenarios:",
		"      - name: default",
		"        response:",
		"          content: \"hi from " + name + "\"",
		"",
	}, "\n")
}

func TestCreate_ServesImmediatelyAndPersists(t *testing.T) {
	r := newRig(t)
	text, isErr := r.call(t, "create_agent", map[string]any{"definition": agentYAML("alpha", "gpt-4o")})
	if isErr {
		t.Fatalf("create_agent failed: %s", text)
	}
	if !strings.Contains(text, "\"status\": \"created\"") {
		t.Errorf("unexpected create response: %s", text)
	}
	// Serves immediately from the registry.
	if r.registry.GetForTenant("alpha", "") == nil {
		t.Error("agent not registered after create")
	}
	// Persisted to disk.
	if _, err := os.Stat(filepath.Join(r.dir, "alpha.yaml")); err != nil {
		t.Errorf("agent file not persisted: %v", err)
	}
}

func TestCreate_ConflictIsErrorResult(t *testing.T) {
	r := newRig(t)
	if _, isErr := r.call(t, "create_agent", map[string]any{"definition": agentYAML("dup", "gpt-4o")}); isErr {
		t.Fatal("first create should succeed")
	}
	text, isErr := r.call(t, "create_agent", map[string]any{"definition": agentYAML("dup", "gpt-4o")})
	if !isErr {
		t.Fatalf("second create should be a conflict error result, got: %s", text)
	}
	if !strings.Contains(text, "already exists") {
		t.Errorf("conflict message missing: %s", text)
	}
}

func TestPut_CreateThenUpdate(t *testing.T) {
	r := newRig(t)
	text, isErr := r.call(t, "put_agent", map[string]any{"definition": agentYAML("beta", "gpt-4o")})
	if isErr || !strings.Contains(text, "\"status\": \"created\"") {
		t.Fatalf("first put should create: %s (isErr=%v)", text, isErr)
	}
	text, isErr = r.call(t, "put_agent", map[string]any{"definition": agentYAML("beta", "gpt-4o-mini")})
	if isErr || !strings.Contains(text, "\"status\": \"updated\"") {
		t.Fatalf("second put should update: %s (isErr=%v)", text, isErr)
	}
	if got := r.registry.GetForTenant("beta", "").Spec.Model; got != "gpt-4o-mini" {
		t.Errorf("model after update = %q, want gpt-4o-mini", got)
	}
}

func TestGet_ReturnsCanonicalYAML(t *testing.T) {
	r := newRig(t)
	r.call(t, "create_agent", map[string]any{"definition": agentYAML("gamma", "gpt-4o")})
	text, isErr := r.call(t, "get_agent", map[string]any{"name": "gamma"})
	if isErr {
		t.Fatalf("get_agent failed: %s", text)
	}
	if !strings.Contains(text, "name: gamma") || !strings.Contains(text, "model: gpt-4o") {
		t.Errorf("get_agent did not return the definition: %s", text)
	}
}

func TestGet_NotFound(t *testing.T) {
	r := newRig(t)
	text, isErr := r.call(t, "get_agent", map[string]any{"name": "ghost"})
	if !isErr || !strings.Contains(text, "not found") {
		t.Errorf("expected not-found error result, got: %s (isErr=%v)", text, isErr)
	}
}

func TestList_ReflectsRegistry(t *testing.T) {
	r := newRig(t)
	r.call(t, "create_agent", map[string]any{"definition": agentYAML("one", "gpt-4o")})
	r.call(t, "create_agent", map[string]any{"definition": agentYAML("two", "gpt-4o")})
	text, isErr := r.call(t, "list_agents", map[string]any{})
	if isErr {
		t.Fatalf("list_agents failed: %s", text)
	}
	if !strings.Contains(text, "one") || !strings.Contains(text, "two") || !strings.Contains(text, "\"count\": 2") {
		t.Errorf("list_agents missing entries: %s", text)
	}
}

func TestDelete_RemovesAndUnserves(t *testing.T) {
	r := newRig(t)
	r.call(t, "create_agent", map[string]any{"definition": agentYAML("doomed", "gpt-4o")})
	file := filepath.Join(r.dir, "doomed.yaml")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("precondition: file should exist: %v", err)
	}
	text, isErr := r.call(t, "delete_agent", map[string]any{"name": "doomed"})
	if isErr || !strings.Contains(text, "\"status\": \"deleted\"") {
		t.Fatalf("delete failed: %s (isErr=%v)", text, isErr)
	}
	if r.registry.GetForTenant("doomed", "") != nil {
		t.Error("agent still served after delete")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("agent file not removed: %v", err)
	}
	// Deleting again is a not-found error result.
	if _, isErr := r.call(t, "delete_agent", map[string]any{"name": "doomed"}); !isErr {
		t.Error("second delete should be a not-found error result")
	}
}

func TestValidate_GoodAndBad(t *testing.T) {
	r := newRig(t)
	// Valid: ok:true and NOT persisted/registered.
	text, isErr := r.call(t, "validate_agent", map[string]any{"definition": agentYAML("checkme", "gpt-4o")})
	if isErr || !strings.Contains(text, "\"ok\": true") {
		t.Fatalf("valid agent should report ok:true: %s (isErr=%v)", text, isErr)
	}
	if r.registry.GetForTenant("checkme", "") != nil {
		t.Error("validate_agent must not register the agent")
	}
	// Invalid: missing required spec fields.
	bad := "apiVersion: mockagents/v1\nkind: Agent\nmetadata:\n  name: broken\nspec: {}\n"
	text, isErr = r.call(t, "validate_agent", map[string]any{"definition": bad})
	if !isErr || !strings.Contains(text, "\"ok\": false") {
		t.Errorf("invalid agent should report an error result: %s (isErr=%v)", text, isErr)
	}
}

func TestCreate_InvalidDefinitionIsErrorResult(t *testing.T) {
	r := newRig(t)
	// metadata.name violating the kebab-case rule must be rejected, not persisted.
	bad := strings.Replace(agentYAML("ok", "gpt-4o"), "name: ok", "name: Bad_Name", 1)
	text, isErr := r.call(t, "create_agent", map[string]any{"definition": bad})
	if !isErr {
		t.Fatalf("invalid name should be rejected, got: %s", text)
	}
	if r.registry.GetForTenant("Bad_Name", "") != nil {
		t.Error("invalid agent must not be registered")
	}
}

func TestCreate_MissingDefinitionArg(t *testing.T) {
	r := newRig(t)
	text, isErr := r.call(t, "create_agent", map[string]any{})
	if !isErr || !strings.Contains(text, "definition is required") {
		t.Errorf("missing definition should error: %s (isErr=%v)", text, isErr)
	}
}

func TestInMemoryMode_NoPersistence(t *testing.T) {
	reg := engine.NewAgentRegistry()
	// In-memory: no agents dir.
	mem := &rig{registry: reg, dir: ""}
	mem.srv = mcp.NewServer(minimalDef())
	NewManager(reg, "", "").Register(mem.srv)
	text, isErr := mem.call(t, "create_agent", map[string]any{"definition": agentYAML("mem", "gpt-4o")})
	if isErr {
		t.Fatalf("in-memory create failed: %s", text)
	}
	if !strings.Contains(text, "\"persisted\": false") {
		t.Errorf("in-memory create should report persisted:false: %s", text)
	}
	if reg.GetForTenant("mem", "") == nil {
		t.Error("in-memory agent should still be registered/served")
	}
}

// Package mcpadmin exposes MockAgents' agent-management write API as a set of
// programmatic MCP tools (MCP-03), so an MCP client — e.g. an AI coding agent —
// can list / get / create / replace / delete / validate MockAgents agents over
// the Model Context Protocol. It registers Go-backed tools on an *mcp.Server via
// Server.RegisterTool; no kind:MCPServer document is required.
//
// It deliberately RE-EXPRESSES (rather than imports) the server package's HTTP
// write-core: internal/mcp stays decoupled from internal/server, and the MCP
// path has no HTTP / audit / multi-principal concerns. The validation,
// canonicalization, and persist+register mechanics mirror
// internal/server/agent_write_handlers.go — keep the two in sync.
package mcpadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/mcp"
	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// maxAgentBytes caps a submitted agent definition, matching the HTTP write API
// (an agent doc is small; this stops an unbounded YAML alias-bomb).
const maxAgentBytes = 1 << 20

// agentNameRe mirrors the validator's metadata.name rule; a matching name is a
// safe single path segment (no "/", "\", ".", "..").
var agentNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func safeAgentName(name string) bool {
	return len(name) <= 63 && agentNameRe.MatchString(name)
}

// Manager backs the agent-management MCP tools with a live engine registry and
// an optional agents directory for persistence. A single Manager is safe for
// concurrent use; its mutex serializes create-or-replace and delete so the
// existence check and the mutation can't race (the same guard the HTTP write
// API gets from agentWriteMu).
type Manager struct {
	registry  *engine.AgentRegistry
	agentsDir string // "" = in-memory only (no persistence)
	tenantID  string // owner of MCP-managed agents ("" = global / single-tenant)
	mu        sync.Mutex
}

// NewManager builds a Manager over registry. agentsDir may be "" to run
// in-memory (agents serve immediately but are not persisted). tenantID is the
// owner assigned to managed agents; "" is the single-tenant / global namespace.
func NewManager(registry *engine.AgentRegistry, agentsDir, tenantID string) *Manager {
	return &Manager{registry: registry, agentsDir: agentsDir, tenantID: tenantID}
}

// Register wires the agent-management tools onto s. Call before the server
// starts serving.
func (m *Manager) Register(s *mcp.Server) {
	s.RegisterTool(types.MCPTool{
		Name:        "list_agents",
		Description: "List the MockAgents agents currently served, with their model, protocol, and scenario count.",
		InputSchema: objectSchema(nil, nil),
	}, m.handleList)

	s.RegisterTool(types.MCPTool{
		Name:        "get_agent",
		Description: "Return the canonical YAML definition of a MockAgents agent by name.",
		InputSchema: objectSchema(map[string]any{
			"name": stringProp("The agent's metadata.name."),
		}, []string{"name"}),
	}, m.handleGet)

	s.RegisterTool(types.MCPTool{
		Name:        "validate_agent",
		Description: "Validate a MockAgents agent definition (YAML or JSON) without persisting it. Returns the validation report.",
		InputSchema: objectSchema(map[string]any{
			"definition": stringProp("The agent definition as a YAML or JSON string."),
		}, []string{"definition"}),
	}, m.handleValidate)

	s.RegisterTool(types.MCPTool{
		Name:        "create_agent",
		Description: "Create a new MockAgents agent from a YAML or JSON definition. Fails if an agent of that name already exists (use put_agent to replace). The agent serves immediately.",
		InputSchema: objectSchema(map[string]any{
			"definition": stringProp("The agent definition as a YAML or JSON string."),
		}, []string{"definition"}),
	}, m.handleCreate)

	s.RegisterTool(types.MCPTool{
		Name:        "put_agent",
		Description: "Create or replace a MockAgents agent from a YAML or JSON definition. The agent serves immediately.",
		InputSchema: objectSchema(map[string]any{
			"definition": stringProp("The agent definition as a YAML or JSON string."),
		}, []string{"definition"}),
	}, m.handlePut)

	s.RegisterTool(types.MCPTool{
		Name:        "delete_agent",
		Description: "Delete a MockAgents agent by name. It stops serving immediately and its persisted file (if any) is removed.",
		InputSchema: objectSchema(map[string]any{
			"name": stringProp("The agent's metadata.name."),
		}, []string{"name"}),
	}, m.handleDelete)
}

// --- handlers ---

func (m *Manager) handleList(_ context.Context, _ map[string]any) (mcp.ToolResult, error) {
	defs := m.registry.ListForTenant(m.tenantID)
	type summary struct {
		Name      string `json:"name"`
		Model     string `json:"model"`
		Protocol  string `json:"protocol"`
		Scenarios int    `json:"scenarios"`
	}
	out := make([]summary, 0, len(defs))
	for _, d := range defs {
		out = append(out, summary{
			Name:      d.Metadata.Name,
			Model:     d.Spec.Model,
			Protocol:  d.Spec.Protocol,
			Scenarios: len(d.Spec.Behavior.Scenarios),
		})
	}
	return jsonResult(map[string]any{"count": len(out), "agents": out})
}

func (m *Manager) handleGet(_ context.Context, args map[string]any) (mcp.ToolResult, error) {
	name, ok := stringArg(args, "name")
	if !ok {
		return errorResult("name is required"), nil
	}
	def := m.registry.GetForTenant(name, m.tenantID)
	if def == nil {
		return errorResult("agent %q not found", name), nil
	}
	b, err := yaml.Marshal(def)
	if err != nil {
		return mcp.ToolResult{}, fmt.Errorf("marshaling agent %q: %w", name, err)
	}
	return textResult(string(b)), nil
}

func (m *Manager) handleValidate(_ context.Context, args map[string]any) (mcp.ToolResult, error) {
	body, ok := definitionBytes(args)
	if !ok {
		return errorResult("definition is required (a YAML or JSON string)"), nil
	}
	if len(body) > maxAgentBytes {
		return errorResult("definition too large (max %d bytes)", maxAgentBytes), nil
	}
	report := config.ValidateBytes(body)
	if report.Kind != "" && report.Kind != "Agent" {
		return errorResult("expected an Agent document, got %q", report.Kind), nil
	}
	if len(report.Errors) > 0 {
		return validationResult(report), nil
	}
	return jsonResult(map[string]any{"ok": true, "kind": report.Kind})
}

func (m *Manager) handleCreate(_ context.Context, args map[string]any) (mcp.ToolResult, error) {
	return m.apply(args, false)
}

func (m *Manager) handlePut(_ context.Context, args map[string]any) (mcp.ToolResult, error) {
	return m.apply(args, true)
}

func (m *Manager) handleDelete(_ context.Context, args map[string]any) (mcp.ToolResult, error) {
	name, ok := stringArg(args, "name")
	if !ok {
		return errorResult("name is required"), nil
	}
	if !safeAgentName(name) {
		return errorResult("invalid agent name %q: must be lowercase kebab-case", name), nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 404 against the caller's own bucket (cannot delete a global agent from a
	// tenant context, and vice versa).
	if m.registry.GetOwnedForTenant(name, m.tenantID) == nil {
		return errorResult("agent %q not found", name), nil
	}

	// Remove the persisted file FIRST using the tracked source path, so a
	// "deleted" response is never a lie if the file removal fails (FB04-01/02).
	var file string
	target := m.registry.Source(name, m.tenantID)
	if target == "" {
		if t, perr := m.agentFilePath(name); perr == nil {
			target = t
		}
	}
	if target != "" {
		if err := os.Remove(target); err == nil {
			file = filepath.Base(target)
		} else if !os.IsNotExist(err) {
			return mcp.ToolResult{}, fmt.Errorf("removing persisted agent file: %w", err)
		}
	}
	_ = m.registry.RemoveForTenant(name, m.tenantID)
	return jsonResult(map[string]any{"status": "deleted", "agent": name, "file": file})
}

// apply is the shared create-or-replace core. allowReplace=false is create
// (409-equivalent on conflict); true is put (create-or-replace).
func (m *Manager) apply(args map[string]any, allowReplace bool) (mcp.ToolResult, error) {
	body, ok := definitionBytes(args)
	if !ok {
		return errorResult("definition is required (a YAML or JSON string)"), nil
	}
	if len(body) > maxAgentBytes {
		return errorResult("definition too large (max %d bytes)", maxAgentBytes), nil
	}

	var def types.AgentDefinition
	if err := yaml.Unmarshal(body, &def); err != nil {
		return errorResult("invalid agent document: %s", err), nil
	}
	name := strings.TrimSpace(def.Metadata.Name)
	if name == "" {
		return errorResult("metadata.name is required"), nil
	}
	if !safeAgentName(name) {
		return errorResult("invalid metadata.name %q: must be lowercase kebab-case", name), nil
	}

	// Ownership is set server-side, never trusted from the body.
	def.Metadata.TenantID = m.tenantID
	config.ApplyDefaults(&def)

	canonical, err := yaml.Marshal(&def)
	if err != nil {
		return mcp.ToolResult{}, fmt.Errorf("marshaling agent %q: %w", name, err)
	}
	report := config.ValidateBytes(canonical)
	if report.Kind != "" && report.Kind != "Agent" {
		return errorResult("expected an Agent document, got %q", report.Kind), nil
	}
	if len(report.Errors) > 0 {
		return validationResult(report), nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existed := m.registry.GetOwnedForTenant(name, m.tenantID) != nil
	if existed && !allowReplace {
		return errorResult("agent %q already exists (use put_agent to replace)", name), nil
	}
	file, persisted, err := m.persistAndRegister(&def, name, canonical)
	if err != nil {
		return mcp.ToolResult{}, err
	}
	status := "created"
	if existed {
		status = "updated"
	}
	return jsonResult(map[string]any{"status": status, "agent": name, "persisted": persisted, "file": file})
}

// --- persistence (mirrors server.persistAndRegister / resolveAgentTarget) ---

func (m *Manager) persistAndRegister(def *types.AgentDefinition, name string, canonical []byte) (file string, persisted bool, err error) {
	target, err := m.resolveTarget(name)
	if err != nil {
		return "", false, err
	}
	if target == "" {
		// No agents dir: register in memory only.
		m.registry.Register(def)
		return "", false, nil
	}
	if err := atomicWriteFile(target, canonical); err != nil {
		return "", false, fmt.Errorf("writing agent file: %w", err)
	}
	m.registry.RegisterWithSource(def, target)
	return filepath.Base(target), true, nil
}

// resolveTarget returns the file to persist (name) to: the agent's existing
// backing file when known (overwrite in place), else the name-derived path.
func (m *Manager) resolveTarget(name string) (string, error) {
	if m.agentsDir == "" {
		return "", nil
	}
	if src := m.registry.Source(name, m.tenantID); src != "" {
		return src, nil
	}
	return m.agentFilePath(name)
}

// agentFilePath returns the on-disk path for (name, tenant), confined to the
// agents directory as defense in depth.
func (m *Manager) agentFilePath(name string) (string, error) {
	fname := name
	if m.tenantID != "" {
		fname = sanitizeFilenamePart(m.tenantID) + "." + name
	}
	target := filepath.Join(m.agentsDir, fname+".yaml")
	absDir, err := filepath.Abs(m.agentsDir)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if absTarget != absDir && !strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("resolved agent path escapes the agents directory")
	}
	return target, nil
}

// atomicWriteFile writes via a temp file + rename so a crash mid-write can't
// truncate a live config (mirrors server.atomicWriteFile).
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".agent-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// sanitizeFilenamePart reduces an id to a safe filename fragment so a tenant id
// can never inject path separators into a persisted filename.
func sanitizeFilenamePart(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "tenant"
	}
	return b.String()
}

// --- small helpers ---

// definitionBytes extracts the `definition` argument as raw bytes, accepting
// either a YAML/JSON string or a structured object (which a client may pass when
// it builds the definition as JSON rather than a string).
func definitionBytes(args map[string]any) ([]byte, bool) {
	v, ok := args["definition"]
	if !ok {
		return nil, false
	}
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, false
		}
		return []byte(t), true
	case map[string]any:
		b, err := yaml.Marshal(t)
		if err != nil {
			return nil, false
		}
		return b, true
	default:
		return nil, false
	}
}

func stringArg(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// objectSchema builds a JSON-Schema object node for a tool's inputSchema.
func objectSchema(props map[string]any, required []string) types.JSONSchemaObject {
	if props == nil {
		props = map[string]any{}
	}
	s := types.JSONSchemaObject{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func textResult(text string) mcp.ToolResult {
	return mcp.ToolResult{Content: []types.MCPContentBlock{{Type: "text", Text: text}}}
}

func errorResult(format string, a ...any) mcp.ToolResult {
	return mcp.ToolResult{
		Content: []types.MCPContentBlock{{Type: "text", Text: fmt.Sprintf(format, a...)}},
		IsError: true,
	}
}

func jsonResult(v any) (mcp.ToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.ToolResult{}, err
	}
	return textResult(string(b)), nil
}

func validationResult(report *config.ValidateReport) mcp.ToolResult {
	errs := make([]string, len(report.Errors))
	for i, e := range report.Errors {
		errs[i] = e.Error()
	}
	b, _ := json.MarshalIndent(map[string]any{"ok": false, "kind": report.Kind, "errors": errs}, "", "  ")
	return mcp.ToolResult{
		Content: []types.MCPContentBlock{{Type: "text", Text: string(b)}},
		IsError: true,
	}
}

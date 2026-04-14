package mcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

// Server dispatches JSON-RPC 2.0 requests against an MCPServerDefinition.
// A single Server instance is safe for concurrent use.
type Server struct {
	def *types.MCPServerDefinition

	mu     sync.RWMutex
	inited bool
}

// NewServer creates a Server bound to a single MCPServerDefinition.
func NewServer(def *types.MCPServerDefinition) *Server {
	return &Server{def: def}
}

// Definition returns the underlying MCP server definition.
func (s *Server) Definition() *types.MCPServerDefinition {
	return s.def
}

// Handle routes a single JSON-RPC request and returns the response. For
// notifications (requests with no id) it returns nil so callers can skip
// writing anything back.
func (s *Server) Handle(req *Request) *Response {
	if req.JSONRPC != "2.0" {
		return newError(req.ID, ErrInvalidRequest, "jsonrpc must be 2.0", nil)
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		s.mu.Lock()
		s.inited = true
		s.mu.Unlock()
		return nil
	case "ping":
		return newResult(req.ID, map[string]any{})
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "prompts/get":
		return s.handlePromptsGet(req)
	default:
		if req.IsNotification() {
			return nil
		}
		return newError(req.ID, ErrMethodNotFound, fmt.Sprintf("method %q not supported", req.Method), nil)
	}
}

// HandleBytes decodes a raw JSON body, routes it through Handle, and
// returns the marshaled response bytes. An empty result means the
// incoming request was a notification and no reply should be sent.
func (s *Server) HandleBytes(body []byte) ([]byte, error) {
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		resp := newError(nil, ErrParseError, "invalid JSON", err.Error())
		return json.Marshal(resp)
	}
	resp := s.Handle(&req)
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}

// --- method handlers ---

type initializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	ServerInfo      map[string]string      `json:"serverInfo"`
	Capabilities    map[string]interface{} `json:"capabilities"`
}

func (s *Server) handleInitialize(req *Request) *Response {
	pv := s.def.Spec.ProtocolVersion
	if pv == "" {
		pv = types.DefaultMCPProtocolVersion
	}
	caps := map[string]any{}
	if s.def.Spec.Capabilities.Tools || len(s.def.Spec.Tools) > 0 {
		caps["tools"] = map[string]any{}
	}
	if s.def.Spec.Capabilities.Resources || len(s.def.Spec.Resources) > 0 {
		caps["resources"] = map[string]any{}
	}
	if s.def.Spec.Capabilities.Prompts || len(s.def.Spec.Prompts) > 0 {
		caps["prompts"] = map[string]any{}
	}
	if s.def.Spec.Capabilities.Logging {
		caps["logging"] = map[string]any{}
	}
	return newResult(req.ID, initializeResult{
		ProtocolVersion: pv,
		ServerInfo: map[string]string{
			"name":    s.def.Metadata.Name,
			"version": "mock",
		},
		Capabilities: caps,
	})
}

type toolListEntry struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	InputSchema types.JSONSchemaObject `json:"inputSchema,omitempty"`
}

func (s *Server) handleToolsList(req *Request) *Response {
	tools := make([]toolListEntry, 0, len(s.def.Spec.Tools))
	for _, t := range s.def.Spec.Tools {
		tools = append(tools, toolListEntry{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return newResult(req.ID, map[string]any{"tools": tools})
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type toolCallResult struct {
	Content []types.MCPContentBlock `json:"content"`
	IsError bool                    `json:"isError,omitempty"`
}

func (s *Server) handleToolsCall(req *Request) *Response {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newError(req.ID, ErrInvalidParams, "invalid params for tools/call", err.Error())
	}
	if params.Name == "" {
		return newError(req.ID, ErrInvalidParams, "tool name is required", nil)
	}

	tool := s.findTool(params.Name)
	if tool == nil {
		return newError(req.ID, ErrMethodNotFound,
			fmt.Sprintf("unknown tool %q", params.Name),
			map[string]any{"available": s.toolNames()})
	}

	resp := resolveToolResponse(tool.Responses, params.Arguments)
	if resp == nil {
		return newResult(req.ID, toolCallResult{
			Content: []types.MCPContentBlock{{Type: "text", Text: fmt.Sprintf("(mock) tool %s called with %v", tool.Name, params.Arguments)}},
		})
	}
	return newResult(req.ID, toolCallResult{Content: resp.Content, IsError: resp.IsError})
}

// resolveToolResponse picks the first MCPToolResponse whose Match is a
// subset of args, falling back to the Default entry. Nil means "no
// configured response".
func resolveToolResponse(responses []types.MCPToolResponse, args map[string]any) *types.MCPToolResponse {
	var fallback *types.MCPToolResponse
	for i := range responses {
		r := &responses[i]
		if r.Default {
			fallback = r
			continue
		}
		if matchArgs(r.Match, args) {
			return r
		}
	}
	return fallback
}

func matchArgs(match, args map[string]any) bool {
	if len(match) == 0 {
		return false
	}
	for k, v := range match {
		got, ok := args[k]
		if !ok {
			return false
		}
		if !reflect.DeepEqual(got, v) {
			return false
		}
	}
	return true
}

func (s *Server) findTool(name string) *types.MCPTool {
	for i := range s.def.Spec.Tools {
		if s.def.Spec.Tools[i].Name == name {
			return &s.def.Spec.Tools[i]
		}
	}
	return nil
}

func (s *Server) toolNames() []string {
	names := make([]string, len(s.def.Spec.Tools))
	for i, t := range s.def.Spec.Tools {
		names[i] = t.Name
	}
	return names
}

type resourceListEntry struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

func (s *Server) handleResourcesList(req *Request) *Response {
	out := make([]resourceListEntry, 0, len(s.def.Spec.Resources))
	for _, r := range s.def.Spec.Resources {
		out = append(out, resourceListEntry{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MimeType:    r.MimeType,
		})
	}
	return newResult(req.ID, map[string]any{"resources": out})
}

type resourcesReadParams struct {
	URI string `json:"uri"`
}

type resourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

func (s *Server) handleResourcesRead(req *Request) *Response {
	var params resourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newError(req.ID, ErrInvalidParams, "invalid params for resources/read", err.Error())
	}
	for _, r := range s.def.Spec.Resources {
		if r.URI != params.URI {
			continue
		}
		return newResult(req.ID, map[string]any{
			"contents": []resourceContents{{
				URI:      r.URI,
				MimeType: r.MimeType,
				Text:     r.Text,
				Blob:     r.Blob,
			}},
		})
	}
	return newError(req.ID, ErrInvalidParams,
		fmt.Sprintf("unknown resource %q", params.URI),
		map[string]any{"available": s.resourceURIs()})
}

func (s *Server) resourceURIs() []string {
	out := make([]string, len(s.def.Spec.Resources))
	for i, r := range s.def.Spec.Resources {
		out[i] = r.URI
	}
	return out
}

type promptListEntry struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Arguments   []types.MCPPromptArg `json:"arguments,omitempty"`
}

func (s *Server) handlePromptsList(req *Request) *Response {
	out := make([]promptListEntry, 0, len(s.def.Spec.Prompts))
	for _, p := range s.def.Spec.Prompts {
		out = append(out, promptListEntry{
			Name:        p.Name,
			Description: p.Description,
			Arguments:   p.Arguments,
		})
	}
	return newResult(req.ID, map[string]any{"prompts": out})
}

type promptsGetParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

func (s *Server) handlePromptsGet(req *Request) *Response {
	var params promptsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newError(req.ID, ErrInvalidParams, "invalid params for prompts/get", err.Error())
	}
	for _, p := range s.def.Spec.Prompts {
		if p.Name != params.Name {
			continue
		}
		// Expand {{arg}} placeholders inside each text content block.
		messages := make([]types.MCPPromptMessage, 0, len(p.Messages))
		for _, m := range p.Messages {
			expanded := m
			if expanded.Content.Type == "text" {
				expanded.Content.Text = expandArgs(expanded.Content.Text, params.Arguments)
			}
			messages = append(messages, expanded)
		}
		return newResult(req.ID, map[string]any{
			"description": p.Description,
			"messages":    messages,
		})
	}
	return newError(req.ID, ErrInvalidParams,
		fmt.Sprintf("unknown prompt %q", params.Name), nil)
}

// expandArgs substitutes simple {{name}} placeholders with the matching
// argument value. Missing arguments are left untouched so the client can
// see that nothing was substituted.
func expandArgs(s string, args map[string]string) string {
	for k, v := range args {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

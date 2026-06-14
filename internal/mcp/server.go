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
//
// v0.2 additions: the Server also tracks a current `logging/setLevel`
// value, an autocomplete catalog for `completion/complete`, and a
// pending-notification queue that the stdio/HTTP transports drain
// after every request. Notifications are how MCP servers signal
// list changes to clients (e.g. `notifications/tools/list_changed`)
// without a bidirectional channel — the mock keeps them in memory
// until a transport ships them out.
type Server struct {
	def *types.MCPServerDefinition

	mu         sync.RWMutex
	inited     bool
	logLvl     string
	pending    []*Notification
	subscribed map[string]bool

	// bi drives the v0.3 bidirectional transport: SSE subscribers,
	// server-initiated requests (sampling/roots), and their response
	// correlation map. Always non-nil; constructed by NewServer.
	bi *bidirectional
}

// Notification is a server-initiated JSON-RPC notification (no id).
// Transports drain the queue after each handled request and write
// these out alongside the response.
type Notification struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

// NewServer creates a Server bound to a single MCPServerDefinition.
func NewServer(def *types.MCPServerDefinition) *Server {
	return &Server{def: def, bi: newBidirectional(), subscribed: map[string]bool{}}
}

// EmitNotification enqueues a server-initiated JSON-RPC notification
// for the next transport drain. Safe for concurrent use. The mock
// emits these on demand so test harnesses can verify their MCP
// clients react correctly to list-changed events without needing a
// real upstream server.
func (s *Server) EmitNotification(method string, params map[string]any) {
	n := &Notification{Method: method, Params: params}
	s.mu.Lock()
	s.pending = append(s.pending, n)
	s.mu.Unlock()
	// Also push through the bidirectional queue so any SSE subscriber
	// sees the notification in the same ordered stream as server-
	// initiated requests. Plain-HTTP callers keep observing
	// notifications via DrainNotifications / the X-MCP-Pending-*
	// header on the next response.
	s.pushNotification(n)
}

// DrainNotifications returns any queued notifications and clears the
// queue. Transports call this after writing a response so server
// notifications are interleaved with request handling at the
// natural cadence of a real MCP server.
func (s *Server) DrainNotifications() []*Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	out := s.pending
	s.pending = nil
	return out
}

// LogLevel returns the most recent value set via `logging/setLevel`,
// or the empty string when the client has not configured it. Useful
// for tests that need to assert the server observed a level change.
func (s *Server) LogLevel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.logLvl
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
	case "resources/subscribe":
		return s.handleResourcesSubscribe(req)
	case "resources/unsubscribe":
		return s.handleResourcesUnsubscribe(req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "prompts/get":
		return s.handlePromptsGet(req)
	case "completion/complete":
		return s.handleCompletionComplete(req)
	case "logging/setLevel":
		return s.handleLoggingSetLevel(req)
	case "sampling/createMessage", "roots/list":
		// These methods are server→client in the real protocol —
		// the mock does not have a bidirectional transport so the
		// best we can do is return a clear "not supported" error
		// with a hint about why. Tests that need to verify a
		// client's handling of incoming sampling/roots requests
		// should use EmitNotification with a custom method
		// instead.
		return newError(req.ID, ErrMethodNotFound,
			fmt.Sprintf("method %q is server-initiated and not supported by the mock without a bidirectional transport", req.Method),
			map[string]any{"hint": "use Server.EmitNotification or POST /mcp/notify to drive the client side"})
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
		// The mock accepts resources/subscribe + resources/unsubscribe
		// (tracking the URI set), so advertise the subscribe capability.
		caps["resources"] = map[string]any{"subscribe": true}
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

type resourceSubscribeParams struct {
	URI string `json:"uri"`
}

// handleResourcesSubscribe records a client's interest in a resource
// URI and returns an empty result. The mock tracks the subscribed set
// so tests can assert the server observed the subscription; it does
// not push resources/updated notifications on its own (use
// EmitNotification / POST /mcp/notify to drive that side).
func (s *Server) handleResourcesSubscribe(req *Request) *Response {
	var params resourceSubscribeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newError(req.ID, ErrInvalidParams, "invalid params for resources/subscribe", err.Error())
	}
	if params.URI == "" {
		return newError(req.ID, ErrInvalidParams, "uri is required", nil)
	}
	s.mu.Lock()
	s.subscribed[params.URI] = true
	s.mu.Unlock()
	return newResult(req.ID, map[string]any{})
}

// handleResourcesUnsubscribe clears a previously recorded subscription
// and returns an empty result. Unsubscribing a URI that was never
// subscribed is a no-op (idempotent), matching how a real server
// tolerates redundant unsubscribes.
func (s *Server) handleResourcesUnsubscribe(req *Request) *Response {
	var params resourceSubscribeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newError(req.ID, ErrInvalidParams, "invalid params for resources/unsubscribe", err.Error())
	}
	if params.URI == "" {
		return newError(req.ID, ErrInvalidParams, "uri is required", nil)
	}
	s.mu.Lock()
	delete(s.subscribed, params.URI)
	s.mu.Unlock()
	return newResult(req.ID, map[string]any{})
}

// Subscribed reports whether a resource URI currently has an active
// subscription. Exposed for tests that assert subscribe/unsubscribe
// bookkeeping.
func (s *Server) Subscribed(uri string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscribed[uri]
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
		// Expand {{arg}} placeholders inside each content block. Text
		// blocks interpolate their text; embedded-resource blocks
		// interpolate the URI (and any inline text) so a prompt can
		// embed a resource whose URI is supplied as an argument.
		messages := make([]types.MCPPromptMessage, 0, len(p.Messages))
		for _, m := range p.Messages {
			expanded := m
			expanded.Content.Text = expandArgs(expanded.Content.Text, params.Arguments)
			expanded.Content.URI = expandArgs(expanded.Content.URI, params.Arguments)
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

// --- v0.2 method handlers ---

type completionRef struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type completionArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type completionParams struct {
	Ref      completionRef      `json:"ref"`
	Argument completionArgument `json:"argument"`
}

type completionValues struct {
	Values  []string `json:"values"`
	Total   int      `json:"total,omitempty"`
	HasMore bool     `json:"hasMore,omitempty"`
}

// handleCompletionComplete returns autocomplete suggestions for a
// prompt or resource argument. The mock walks the configured
// completion catalog and picks the first entry whose RefType /
// RefName / ArgName all match (empty fields in the config wildcard).
// When the request carries a non-empty argument value the result is
// further filtered by case-insensitive prefix so the response shape
// is identical to a real autocomplete server's "narrow as you type".
func (s *Server) handleCompletionComplete(req *Request) *Response {
	var params completionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newError(req.ID, ErrInvalidParams, "invalid params for completion/complete", err.Error())
	}
	if params.Argument.Name == "" {
		return newError(req.ID, ErrInvalidParams, "argument.name is required", nil)
	}

	values := s.lookupCompletion(params.Ref.Type, params.Ref.Name, params.Argument.Name)
	if params.Argument.Value != "" {
		values = filterPrefix(values, params.Argument.Value)
	}

	// Spec caps each completion response at 100 items.
	const maxValues = 100
	hasMore := false
	total := len(values)
	if len(values) > maxValues {
		hasMore = true
		values = values[:maxValues]
	}
	return newResult(req.ID, map[string]any{
		"completion": completionValues{
			Values:  values,
			Total:   total,
			HasMore: hasMore,
		},
	})
}

// lookupCompletion walks the configured catalog and returns a copy
// of the first matching entry's Values. Empty config fields in the
// entry act as wildcards so a single entry can serve every prompt.
func (s *Server) lookupCompletion(refType, refName, argName string) []string {
	for _, c := range s.def.Spec.Completions {
		if c.RefType != "" && c.RefType != refType {
			continue
		}
		if c.RefName != "" && c.RefName != refName {
			continue
		}
		if c.ArgName != argName {
			continue
		}
		out := make([]string, len(c.Values))
		copy(out, c.Values)
		return out
	}
	return nil
}

// filterPrefix returns the entries of values whose lowercased form
// starts with the lowercased prefix. The slice is reused — callers
// must not retain the original.
func filterPrefix(values []string, prefix string) []string {
	if prefix == "" {
		return values
	}
	lp := strings.ToLower(prefix)
	out := values[:0]
	for _, v := range values {
		if strings.HasPrefix(strings.ToLower(v), lp) {
			out = append(out, v)
		}
	}
	return out
}

type setLevelParams struct {
	Level string `json:"level"`
}

// handleLoggingSetLevel records the requested log verbosity and
// returns an empty result. The recorded value is observable via
// Server.LogLevel for tests; the mock does not actually filter any
// internal log output because it has none.
func (s *Server) handleLoggingSetLevel(req *Request) *Response {
	var params setLevelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newError(req.ID, ErrInvalidParams, "invalid params for logging/setLevel", err.Error())
	}
	if params.Level == "" {
		return newError(req.ID, ErrInvalidParams, "level is required", nil)
	}
	switch params.Level {
	case "debug", "info", "notice", "warning", "error", "critical", "alert", "emergency":
		// valid syslog-ish level
	default:
		return newError(req.ID, ErrInvalidParams,
			fmt.Sprintf("unknown level %q", params.Level),
			map[string]any{"valid": []string{"debug", "info", "notice", "warning", "error", "critical", "alert", "emergency"}})
	}
	s.mu.Lock()
	s.logLvl = params.Level
	s.mu.Unlock()
	return newResult(req.ID, map[string]any{})
}

package mcp

import (
	"context"

	"github.com/mockagents/mockagents/internal/types"
)

// ToolResult is the value a programmatic ToolHandler returns: the content
// blocks the client sees plus the tool-level error flag.
//
// A DOMAIN failure (bad input, not found, validation error) should be returned
// as a normal result with IsError=true and an explanatory text block — NOT as a
// Go error — so an LLM client sees the failure in-band, exactly as the MCP spec
// intends for `tools/call`. Reserve the handler's error return for an
// UNEXPECTED internal fault, which the server maps to a JSON-RPC error instead.
type ToolResult struct {
	Content []types.MCPContentBlock
	IsError bool
}

// ToolHandler backs a programmatically-registered MCP tool with Go code instead
// of the declarative canned responses a kind:MCPServer document provides. It
// receives the call's arguments and returns a ToolResult. The context is passed
// from the dispatch path for future cancellation/deadline support; today's
// transports supply a background context.
type ToolHandler func(ctx context.Context, args map[string]any) (ToolResult, error)

// registeredTool pairs a handler with the spec advertised in tools/list.
type registeredTool struct {
	spec    types.MCPTool
	handler ToolHandler
}

// RegisterTool adds a programmatic tool to the server: spec is what tools/list
// advertises (name + description + input schema) and handler runs on
// tools/call. A programmatic tool takes precedence over a declarative tool of
// the same name. Intended to be called at construction time, before the server
// starts handling requests; it is mutex-guarded for safety regardless. A blank
// name or nil handler is ignored.
func (s *Server) RegisterTool(spec types.MCPTool, handler ToolHandler) {
	if spec.Name == "" || handler == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolHandlers == nil {
		s.toolHandlers = make(map[string]registeredTool)
	}
	if _, exists := s.toolHandlers[spec.Name]; !exists {
		s.toolOrder = append(s.toolOrder, spec.Name)
	}
	s.toolHandlers[spec.Name] = registeredTool{spec: spec, handler: handler}
}

// hasToolHandlers reports whether any programmatic tool is registered. Used to
// advertise the tools capability in initialize even when the declarative
// definition carries no tools of its own.
func (s *Server) hasToolHandlers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.toolHandlers) > 0
}

// lookupToolHandler returns the registered tool for name, if any.
func (s *Server) lookupToolHandler(name string) (registeredTool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rt, ok := s.toolHandlers[name]
	return rt, ok
}

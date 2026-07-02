// Package mcp implements a mock Model Context Protocol server.
//
// The server speaks JSON-RPC 2.0 and supports the standard MCP methods
// `initialize`, `tools/list`, `tools/call`, `resources/list`,
// `resources/read`, `prompts/list`, `prompts/get`, and `ping`. Two
// transports are exposed: HTTP (POST /mcp) and stdio (line-delimited
// JSON over stdin/stdout). Streaming notifications are out of scope
// for v1.
package mcp

import (
	"encoding/json"
)

// JSON-RPC 2.0 error codes used by the server.
const (
	ErrParseError     = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
)

// Request is an incoming JSON-RPC 2.0 call or notification.
// ID is left as json.RawMessage so it can be numeric, string, or null and
// round-trip faithfully back to the client.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	// Result / RawError mark a decoded message that is actually a client
	// RESPONSE to a server-initiated request, not a request — transports may
	// legally deliver those (Streamable HTTP: ack 202, no body — round-10
	// R10-7).
	Result   json.RawMessage `json:"result,omitempty"`
	RawError json.RawMessage `json:"error,omitempty"`
}

// IsNotification returns true when the request carries no ID and must
// not receive a response per the JSON-RPC 2.0 spec.
func (r *Request) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// IsResponse reports whether the decoded message is a client response
// (no method; a result or error member instead).
func (r *Request) IsResponse() bool {
	return r.Method == "" && (len(r.Result) > 0 || len(r.RawError) > 0)
}

// Response is an outgoing JSON-RPC 2.0 result or error.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError models the error object in a JSON-RPC 2.0 response.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// newError builds a Response carrying an error for the given request id.
// JSON-RPC 2.0 requires the id member in every response; when it cannot be
// determined (parse error, invalid request) it MUST be null — omitting it
// entirely is invalid (round-10 R10-9).
func newError(id json.RawMessage, code int, message string, data any) *Response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message, Data: data},
	}
}

// newResult builds a successful Response for the given request id.
func newResult(id json.RawMessage, result any) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Result: result}
}

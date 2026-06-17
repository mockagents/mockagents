// Package a2a implements a mock A2A (Agent2Agent) server (NF-04). A2A is
// Google's agent-to-agent protocol (now Linux-Foundation-governed): a public
// "Agent Card" served at /.well-known/agent-card.json plus a JSON-RPC 2.0
// surface (message/send, tasks/get, tasks/cancel, …). This package serves the
// declared card and answers message/send with canned, match-based responses,
// modeling the task lifecycle — mirroring how internal/mcp mocks the Model
// Context Protocol. Streaming (message/stream), push notifications, and signed
// cards are documented follow-ons.
package a2a

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// JSON-RPC + A2A error codes.
const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
	errTaskNotFound   = -32001
	errNotCancelable  = -32002
)

// --- JSON-RPC envelope ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// --- A2A wire types ---

// Part is one piece of a message or artifact (the mock models text parts).
type Part struct {
	Kind string `json:"kind"` // "text"
	Text string `json:"text,omitempty"`
}

// Message is a turn in the conversation.
type Message struct {
	Role      string `json:"role"` // "user" | "agent"
	Parts     []Part `json:"parts"`
	MessageID string `json:"messageId"`
	Kind      string `json:"kind"` // "message"
	TaskID    string `json:"taskId,omitempty"`
	ContextID string `json:"contextId,omitempty"`
}

// Artifact is a named output the agent produced for a task.
type Artifact struct {
	ArtifactID string `json:"artifactId"`
	Name       string `json:"name,omitempty"`
	Parts      []Part `json:"parts"`
}

// TaskStatus is a task's current state plus the latest agent message.
type TaskStatus struct {
	State     string   `json:"state"`
	Message   *Message `json:"message,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
}

// Task is the unit of work A2A tracks.
type Task struct {
	ID        string     `json:"id"`
	ContextID string     `json:"contextId"`
	Status    TaskStatus `json:"status"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	History   []Message  `json:"history,omitempty"`
	Kind      string     `json:"kind"` // "task"
}

// --- server ---

// Server is a mock A2A server bound to one A2AServerDefinition. Safe for
// concurrent use.
type Server struct {
	def   *types.A2AServerDefinition
	mu    sync.Mutex
	tasks map[string]*Task
	seq   int
}

// NewServer builds a Server for def.
func NewServer(def *types.A2AServerDefinition) *Server {
	return &Server{def: def, tasks: make(map[string]*Task)}
}

// Card returns the public Agent Card, filling url/protocolVersion/capabilities
// defaults. baseURL is the externally-reachable origin (scheme://host); the A2A
// JSON-RPC endpoint is that origin's root.
func (s *Server) Card(baseURL string) types.A2AAgentCard {
	c := s.def.Spec.Card
	if c.URL == "" {
		c.URL = strings.TrimRight(baseURL, "/") + "/"
	}
	if c.ProtocolVersion == "" {
		c.ProtocolVersion = types.DefaultA2AProtocolVersion
	}
	if len(c.DefaultInputModes) == 0 {
		c.DefaultInputModes = []string{"text/plain"}
	}
	if len(c.DefaultOutputModes) == 0 {
		c.DefaultOutputModes = []string{"text/plain"}
	}
	// Streaming is not served yet (a documented follow-on), so never advertise it
	// regardless of what the document declares.
	c.Capabilities.Streaming = false
	return c
}

// HandleBytes decodes a JSON-RPC request body, dispatches it, and returns the
// marshaled response. A nil response (notification) yields nil bytes.
func (s *Server) HandleBytes(body []byte) ([]byte, error) {
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return json.Marshal(newError(nil, errParse, "invalid JSON", err.Error()))
	}
	resp := s.dispatch(&req)
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}

func (s *Server) dispatch(req *rpcRequest) *rpcResponse {
	if req.JSONRPC != "2.0" {
		return newError(req.ID, errInvalidRequest, "jsonrpc must be \"2.0\"", nil)
	}
	switch req.Method {
	case "message/send":
		return s.handleMessageSend(req)
	case "tasks/get":
		return s.handleTasksGet(req)
	case "tasks/cancel":
		return s.handleTasksCancel(req)
	case "message/stream":
		return newError(req.ID, errMethodNotFound,
			"message/stream (streaming) is not supported by this mock yet", nil)
	default:
		if len(req.ID) == 0 {
			return nil // a notification we don't handle
		}
		return newError(req.ID, errMethodNotFound, fmt.Sprintf("method %q not supported", req.Method), nil)
	}
}

type messageSendParams struct {
	Message struct {
		Role      string `json:"role"`
		Parts     []Part `json:"parts"`
		MessageID string `json:"messageId"`
		ContextID string `json:"contextId"`
		TaskID    string `json:"taskId"`
	} `json:"message"`
}

func (s *Server) handleMessageSend(req *rpcRequest) *rpcResponse {
	var p messageSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return newError(req.ID, errInvalidParams, "invalid params for message/send", err.Error())
	}
	userText := partsText(p.Message.Parts)
	resp := s.matchResponse(userText)

	s.mu.Lock()
	defer s.mu.Unlock()

	contextID := p.Message.ContextID
	if contextID == "" {
		contextID = s.nextID("ctx")
	}
	taskID := s.nextID("task")
	now := time.Now().UTC().Format(time.RFC3339)

	userMsg := Message{
		Role: "user", Parts: p.Message.Parts, MessageID: orDefault(p.Message.MessageID, s.nextID("msg")),
		Kind: "message", TaskID: taskID, ContextID: contextID,
	}
	state := "completed"
	replyText := "(no response configured)"
	if resp != nil {
		if resp.State != "" {
			state = resp.State
		}
		if resp.Text != "" {
			replyText = resp.Text
		}
	}
	agentMsg := Message{
		Role: "agent", Parts: []Part{{Kind: "text", Text: replyText}},
		MessageID: s.nextID("msg"), Kind: "message", TaskID: taskID, ContextID: contextID,
	}

	task := &Task{
		ID: taskID, ContextID: contextID, Kind: "task",
		Status:    TaskStatus{State: state, Message: &agentMsg, Timestamp: now},
		Artifacts: []Artifact{{ArtifactID: s.nextID("artifact"), Name: "response", Parts: []Part{{Kind: "text", Text: replyText}}}},
		History:   []Message{userMsg, agentMsg},
	}
	s.tasks[taskID] = task
	return newResult(req.ID, task)
}

type taskIDParams struct {
	ID string `json:"id"`
}

func (s *Server) handleTasksGet(req *rpcRequest) *rpcResponse {
	var p taskIDParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return newError(req.ID, errInvalidParams, "invalid params for tasks/get", err.Error())
	}
	s.mu.Lock()
	task, ok := s.tasks[p.ID]
	s.mu.Unlock()
	if !ok {
		return newError(req.ID, errTaskNotFound, fmt.Sprintf("task %q not found", p.ID), nil)
	}
	return newResult(req.ID, task)
}

func (s *Server) handleTasksCancel(req *rpcRequest) *rpcResponse {
	var p taskIDParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return newError(req.ID, errInvalidParams, "invalid params for tasks/cancel", err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[p.ID]
	if !ok {
		return newError(req.ID, errTaskNotFound, fmt.Sprintf("task %q not found", p.ID), nil)
	}
	if isTerminal(task.Status.State) {
		return newError(req.ID, errNotCancelable,
			fmt.Sprintf("task %q is in terminal state %q and cannot be canceled", p.ID, task.Status.State), nil)
	}
	task.Status.State = "canceled"
	task.Status.Timestamp = time.Now().UTC().Format(time.RFC3339)
	return newResult(req.ID, task)
}

// matchResponse picks the first response whose Match is a substring of text,
// falling back to the Default entry.
func (s *Server) matchResponse(text string) *types.A2AMessageResponse {
	var fallback *types.A2AMessageResponse
	for i := range s.def.Spec.Responses {
		r := &s.def.Spec.Responses[i]
		if r.Default {
			fallback = r
			continue
		}
		if r.Match != "" && strings.Contains(text, r.Match) {
			return r
		}
	}
	return fallback
}

func (s *Server) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%d", prefix, s.seq)
}

// --- HTTP handlers ---

// CardHandler serves GET /.well-known/agent-card.json, deriving the card URL
// from the incoming request so the advertised endpoint is reachable.
func (s *Server) CardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		base := requestBaseURL(r)
		writeJSON(w, http.StatusOK, s.Card(base))
	}
}

// RPCHandler serves the JSON-RPC endpoint (POST). A notification (no id)
// produces 204 No Content.
func (s *Server) RPCHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBounded(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, newError(nil, errParse, "could not read request body", err.Error()))
			return
		}
		out, err := s.HandleBytes(body)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, newError(nil, errInternal, "internal error", err.Error()))
			return
		}
		if out == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
	}
}

// --- helpers ---

func newResult(id json.RawMessage, result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func newError(id json.RawMessage, code int, msg string, data any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}}
}

func partsText(parts []Part) string {
	var b []string
	for _, p := range parts {
		if p.Text != "" {
			b = append(b, p.Text)
		}
	}
	return strings.Join(b, " ")
}

func isTerminal(state string) bool {
	switch state {
	case "completed", "canceled", "failed", "rejected":
		return true
	}
	return false
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}

func readBounded(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	const maxBody = 4 << 20 // 4 MiB
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBody))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

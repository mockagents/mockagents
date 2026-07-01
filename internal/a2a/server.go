// Package a2a implements a mock A2A (Agent2Agent) server (NF-04). A2A is
// Google's agent-to-agent protocol (now Linux-Foundation-governed): a public
// "Agent Card" served at /.well-known/agent-card.json plus a JSON-RPC 2.0
// surface (message/send, message/stream, tasks/get, tasks/cancel, …). This
// package serves the declared card and answers message/send with canned,
// match-based responses, modeling the task lifecycle — mirroring how
// internal/mcp mocks the Model Context Protocol. message/stream is served over
// SSE as a Task/status-update/artifact-update event sequence. Push
// notifications and signed cards are documented follow-ons.
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
	JSONRPC string `json:"jsonrpc"`
	// ID is NOT omitempty: JSON-RPC 2.0 §5.1 requires the response id to be
	// null when it can't be determined (parse error / invalid request). A nil
	// json.RawMessage marshals to `null`, which is exactly what we want.
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// --- A2A wire types ---

// Part is one piece of a message or artifact. A2A defines three kinds: text,
// file (bytes or a uri), and data (an arbitrary JSON object). Only the field for
// the part's kind is populated.
type Part struct {
	Kind string    `json:"kind"` // "text" | "file" | "data"
	Text string    `json:"text,omitempty"`
	File *FilePart `json:"file,omitempty"`
	Data any       `json:"data,omitempty"`
}

// FilePart is the payload of a kind:"file" Part — inline base64 `bytes` or a
// `uri`, with optional name/mimeType.
type FilePart struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Bytes    string `json:"bytes,omitempty"`
	URI      string `json:"uri,omitempty"`
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

// statusUpdateEvent / artifactUpdateEvent are the streamed events message/stream
// emits over SSE (each wrapped in a JSON-RPC result). `final:true` on the last
// status update tells the client the stream is complete.
type statusUpdateEvent struct {
	TaskID    string     `json:"taskId"`
	ContextID string     `json:"contextId"`
	Kind      string     `json:"kind"` // "status-update"
	Status    TaskStatus `json:"status"`
	Final     bool       `json:"final"`
}

type artifactUpdateEvent struct {
	TaskID    string   `json:"taskId"`
	ContextID string   `json:"contextId"`
	Kind      string   `json:"kind"` // "artifact-update"
	Artifact  Artifact `json:"artifact"`
	Append    bool     `json:"append"`
	LastChunk bool     `json:"lastChunk"`
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
	if c.PreferredTransport == "" {
		c.PreferredTransport = types.DefaultA2ATransport
	}
	// version + description are required Agent Card fields; default them so a
	// document that omits them still yields a spec-valid card.
	if c.Version == "" {
		c.Version = "0.0.0"
	}
	if c.Description == "" {
		c.Description = "Mock A2A agent."
	}
	if len(c.DefaultInputModes) == 0 {
		c.DefaultInputModes = []string{"text/plain"}
	}
	if len(c.DefaultOutputModes) == 0 {
		c.DefaultOutputModes = []string{"text/plain"}
	}
	// skills is a required array and each skill's `tags` must render as a JSON
	// array, never null/omitted. Copy into a fresh (non-nil) slice so an empty
	// skill set serializes as [] and we never mutate the stored def.
	skills := make([]types.A2ASkill, len(c.Skills))
	copy(skills, c.Skills)
	for i := range skills {
		if skills[i].Tags == nil {
			skills[i].Tags = []string{}
		}
	}
	c.Skills = skills
	// The mock serves message/stream over SSE, so advertise streaming regardless
	// of what the document declares (a client should be able to discover it).
	c.Capabilities.Streaming = true
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
		// Streaming is served over SSE by RPCHandler. A non-SSE (unary) caller of
		// HandleBytes still gets a sensible single result: the completed task.
		return s.handleMessageSend(req)
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
	parts, state, asMessage := s.replyFor(partsText(p.Message.Parts))

	s.mu.Lock()
	defer s.mu.Unlock()

	contextID := p.Message.ContextID
	if contextID == "" {
		contextID = s.nextID("ctx")
	}

	// A2A result is Task|Message: a response flagged as_message returns a bare
	// agent Message (no task is created/stored), modeling a quick stateless reply.
	if asMessage {
		return newResult(req.ID, Message{
			Role: "agent", Parts: parts, MessageID: s.nextID("msg"), Kind: "message", ContextID: contextID,
		})
	}

	taskID := s.nextID("task")
	now := time.Now().UTC().Format(time.RFC3339)
	userMsg := Message{
		Role: "user", Parts: p.Message.Parts, MessageID: orDefault(p.Message.MessageID, s.nextID("msg")),
		Kind: "message", TaskID: taskID, ContextID: contextID,
	}
	agentMsg := Message{
		Role: "agent", Parts: parts, MessageID: s.nextID("msg"), Kind: "message", TaskID: taskID, ContextID: contextID,
	}
	task := &Task{
		ID: taskID, ContextID: contextID, Kind: "task",
		Status:    TaskStatus{State: state, Message: &agentMsg, Timestamp: now},
		Artifacts: []Artifact{{ArtifactID: s.nextID("artifact"), Name: "response", Parts: parts}},
		History:   []Message{userMsg, agentMsg},
	}
	s.tasks[taskID] = task
	return newResult(req.ID, task)
}

// replyFor resolves the canned reply for an incoming user text: the agent's
// reply Parts (a text Part, plus a data Part when the matched response sets
// `data`), the terminal task state, and whether to return a bare Message.
func (s *Server) replyFor(userText string) (parts []Part, state string, asMessage bool) {
	resp := s.matchResponse(userText)
	state = "completed"
	replyText := "(no response configured)"
	if resp != nil {
		if resp.State != "" {
			state = resp.State
		}
		if resp.Text != "" {
			replyText = resp.Text
		}
		asMessage = resp.AsMessage
	}
	parts = []Part{{Kind: "text", Text: replyText}}
	if resp != nil && resp.Data != nil {
		parts = append(parts, Part{Kind: "data", Data: resp.Data})
	}
	return parts, state, asMessage
}

// StreamResults builds the ordered events message/stream emits over SSE: the
// initial Task (working), a working status-update, the response artifact, and a
// final status-update carrying the terminal state. Streaming always yields a
// Task (the as_message shortcut applies to message/send only). The task is
// stored in its terminal form so a later tasks/get is consistent.
func (s *Server) StreamResults(req *rpcRequest) ([]any, *rpcResponse) {
	if req.JSONRPC != "2.0" {
		return nil, newError(req.ID, errInvalidRequest, "jsonrpc must be \"2.0\"", nil)
	}
	var p messageSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return nil, newError(req.ID, errInvalidParams, "invalid params for message/stream", err.Error())
	}
	parts, state, _ := s.replyFor(partsText(p.Message.Parts))

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
	agentMsg := Message{
		Role: "agent", Parts: parts, MessageID: s.nextID("msg"), Kind: "message", TaskID: taskID, ContextID: contextID,
	}
	artifact := Artifact{ArtifactID: s.nextID("artifact"), Name: "response", Parts: parts}

	working := Task{
		ID: taskID, ContextID: contextID, Kind: "task",
		Status: TaskStatus{State: "working", Timestamp: now}, History: []Message{userMsg},
	}
	finalStatus := TaskStatus{State: state, Message: &agentMsg, Timestamp: now}

	// Persist the terminal task for a follow-up tasks/get.
	s.tasks[taskID] = &Task{
		ID: taskID, ContextID: contextID, Kind: "task",
		Status: finalStatus, Artifacts: []Artifact{artifact}, History: []Message{userMsg, agentMsg},
	}

	return []any{
		working,
		statusUpdateEvent{TaskID: taskID, ContextID: contextID, Kind: "status-update",
			Status: TaskStatus{State: "working", Timestamp: now}, Final: false},
		artifactUpdateEvent{TaskID: taskID, ContextID: contextID, Kind: "artifact-update",
			Artifact: artifact, Append: false, LastChunk: true},
		statusUpdateEvent{TaskID: taskID, ContextID: contextID, Kind: "status-update",
			Status: finalStatus, Final: true},
	}, nil
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
// produces 204 No Content. A message/stream request is answered with an SSE
// stream of events; every other method is a single JSON response.
func (s *Server) RPCHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBounded(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, newError(nil, errParse, "could not read request body", err.Error()))
			return
		}
		// Peek the method so message/stream can be served as Server-Sent Events.
		var probe rpcRequest
		if json.Unmarshal(body, &probe) == nil && probe.Method == "message/stream" {
			s.serveStream(w, r, &probe)
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

// serveStream answers message/stream as Server-Sent Events: each event's data
// is a JSON-RPC result wrapping one streamed Task/status-update/artifact-update.
// A params/validation error is returned as a single JSON-RPC error response.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, req *rpcRequest) {
	// A notification (no id) is not a valid streaming request — answer 204 like
	// any other notification rather than opening an id-less SSE stream.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	events, rerr := s.StreamResults(req)
	if rerr != nil {
		writeJSON(w, http.StatusOK, rerr)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for _, ev := range events {
		data, err := json.Marshal(newResult(req.ID, ev))
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return // client went away
		}
		if flusher != nil {
			flusher.Flush()
		}
		if r.Context().Err() != nil {
			return
		}
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

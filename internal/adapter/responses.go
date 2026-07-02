package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// ProtocolOpenAIResponses is the wire-protocol label recorded on
// interaction logs for the OpenAI Responses API surface (POST /v1/responses).
// It is the default transport of the OpenAI Agents SDK, so giving it its own
// label keeps Responses traffic distinguishable from /v1/chat/completions in
// the logs/cost views.
const ProtocolOpenAIResponses = "openai-responses"

// --- Request types ---

// ResponsesRequest is an OpenAI Responses API request (POST /v1/responses).
// `input` is polymorphic (a bare string or an array of typed input items) so
// it is captured as RawMessage and decoded by parseResponsesInput. Tool
// definitions and the tool_choice / text / reasoning blocks are captured
// verbatim because the engine matches on the agent's own configured tools and
// scenarios — the request copies are only echoed back on the response so an
// SDK round-trips its own settings.
type ResponsesRequest struct {
	Model              string            `json:"model"`
	Input              json.RawMessage   `json:"input"`
	Instructions       *string           `json:"instructions,omitempty"`
	Tools              []json.RawMessage `json:"tools,omitempty"`
	ToolChoice         json.RawMessage   `json:"tool_choice,omitempty"`
	PreviousResponseID *string           `json:"previous_response_id,omitempty"`
	Stream             bool              `json:"stream,omitempty"`
	Temperature        *float64          `json:"temperature,omitempty"`
	TopP               *float64          `json:"top_p,omitempty"`
	MaxOutputTokens    *int              `json:"max_output_tokens,omitempty"`
	Metadata           map[string]any    `json:"metadata,omitempty"`
	Store              *bool             `json:"store,omitempty"`
	// Conversation references an OpenAI Conversation (NF-02): a string id or an
	// object {"id": "..."}. Its stored Items are replayed as prior turns, and
	// (when store != false) this turn's input + output are appended to it. It is
	// mutually exclusive with previous_response_id.
	Conversation      json.RawMessage `json:"conversation,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	Text              json.RawMessage `json:"text,omitempty"`
	Reasoning         json.RawMessage `json:"reasoning,omitempty"`
	User              *string         `json:"user,omitempty"`
}

// responsesInputItem is one element of the `input` array. The Responses API
// overloads this single shape for several item kinds, discriminated by Type
// (and, for plain messages, Role): a "message" (or a role-only object), a
// "function_call_output" (a tool result fed back into the next turn), and an
// echoed "function_call". Content/Output are RawMessage because each can be a
// string or an array of content parts.
type responsesInputItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	CallID    string          `json:"call_id"`
	Output    json.RawMessage `json:"output"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"` // echoed function_call args (fingerprint material)
}

// --- Response types ---

// ResponsesResponse is the OpenAI Responses API response object. Fields that
// the real API always renders (even as null) are plain `any` without omitempty
// so the wire shape matches what an SDK expects to deserialize.
type ResponsesResponse struct {
	ID                 string `json:"id"`
	Object             string `json:"object"`
	CreatedAt          int64  `json:"created_at"`
	Status             string `json:"status"`
	Error              any    `json:"error"`
	IncompleteDetails  any    `json:"incomplete_details"`
	Instructions       any    `json:"instructions"`
	MaxOutputTokens    any    `json:"max_output_tokens"`
	Model              string `json:"model"`
	Output             []any  `json:"output"`
	ParallelToolCalls  bool   `json:"parallel_tool_calls"`
	PreviousResponseID any    `json:"previous_response_id"`
	Reasoning          any    `json:"reasoning"`
	Store              bool   `json:"store"`
	// Conversation echoes the referenced conversation as {"id": …} (NF-02), or
	// null. SDKs read response.conversation.id to continue a thread.
	Conversation any            `json:"conversation"`
	Temperature  float64        `json:"temperature"`
	Text         any            `json:"text"`
	ToolChoice   any            `json:"tool_choice"`
	Tools        []any          `json:"tools"`
	TopP         float64        `json:"top_p"`
	Truncation   string         `json:"truncation"`
	Usage        ResponsesUsage `json:"usage"`
	User         any            `json:"user"`
	Metadata     map[string]any `json:"metadata"`
}

// ResponsesUsage is the Responses-API token-usage shape (input_tokens /
// output_tokens with the nested *_details blocks), distinct from the Chat
// Completions prompt/completion naming.
type ResponsesUsage struct {
	InputTokens         int                        `json:"input_tokens"`
	InputTokensDetails  responsesInputTokenDetail  `json:"input_tokens_details"`
	OutputTokens        int                        `json:"output_tokens"`
	OutputTokensDetails responsesOutputTokenDetail `json:"output_tokens_details"`
	TotalTokens         int                        `json:"total_tokens"`
}

type responsesInputTokenDetail struct {
	CachedTokens int `json:"cached_tokens"`
}

type responsesOutputTokenDetail struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// responseMessageItem is an output item of type "message" — the assistant's
// textual reply, holding one or more content parts.
type responseMessageItem struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Status  string `json:"status"`
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

// responseOutputText is a content part of type "output_text".
type responseOutputText struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

// responseRefusalPart is a content part of type "refusal".
type responseRefusalPart struct {
	Type    string `json:"type"`
	Refusal string `json:"refusal"`
}

// responseFunctionCallItem is an output item of type "function_call".
type responseFunctionCallItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Status    string `json:"status"`
}

// --- Stateful response store ---

// responseStore keeps the flattened conversation behind each emitted response
// id so a follow-up request carrying `previous_response_id` can replay the
// prior turns. The Agents SDK drives multi-step tool loops this way: each
// step sends only the new function_call_output and references the previous
// response, expecting the server to remember everything before it. It is a
// bounded FIFO (oldest evicted past maxStoredResponses) so a long-running
// mock cannot grow without limit.
type responseStore struct {
	mu    sync.Mutex
	m     map[string][]engine.RequestMessage
	order []string
}

const maxStoredResponses = 1024

func newResponseStore() *responseStore {
	return &responseStore{m: make(map[string][]engine.RequestMessage)}
}

func (s *responseStore) get(id string) ([]engine.RequestMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs, ok := s.m[id]
	return msgs, ok
}

func (s *responseStore) put(id string, msgs []engine.RequestMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.m[id]; !exists {
		s.order = append(s.order, id)
		for len(s.order) > maxStoredResponses {
			oldest := s.order[0]
			s.order = s.order[1:]
			delete(s.m, oldest)
		}
	}
	s.m[id] = msgs
}

// --- Handler ---

// ResponsesHandler serves the OpenAI Responses API (POST /v1/responses). It is
// a second OpenAI-compatible surface alongside OpenAIHandler (Chat
// Completions); both translate into the same engine, but the Responses surface
// adds server-side conversation state (previous_response_id) and its own
// output-item / SSE-event wire shape.
type ResponsesHandler struct {
	Engine *engine.Engine
	store  *responseStore
	// convStore backs the Responses `conversation` param (NF-02). It is shared
	// with ConversationsHandler so a conversation created via /v1/conversations
	// can be read and extended here. May be nil (conversation param then 404s).
	convStore *conversationStore
}

// NewResponsesHandler builds a ResponsesHandler with an initialized response
// store. The store must outlive individual requests (it backs
// previous_response_id), so it is created once here rather than per request.
// conv is the shared conversation store (NF-02); pass nil to disable the
// conversation param.
func NewResponsesHandler(eng *engine.Engine, conv *conversationStore) *ResponsesHandler {
	return &ResponsesHandler{Engine: eng, store: newResponseStore(), convStore: conv}
}

// Name identifies this adapter in logs and diagnostics.
func (h *ResponsesHandler) Name() string { return "openai-responses" }

// Routes returns the Responses-API route mounted through the adapter Registry.
func (h *ResponsesHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/responses", Handler: h.HandleResponses},
	}
}

// HandleResponses handles POST /v1/responses.
func (h *ResponsesHandler) HandleResponses(w http.ResponseWriter, r *http.Request) {
	meta := engine.RequestMetaFromContext(r.Context())
	if meta != nil {
		meta.Protocol = ProtocolOpenAIResponses
	}

	var req ResponsesRequest
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	// Parse the polymorphic `input` into flat engine messages.
	inputMsgs, err := parseResponsesInput(req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	tenant := engine.TenantIDFromContext(r.Context())

	// Resolve an optional conversation reference (NF-02). It is mutually
	// exclusive with previous_response_id, matching the real API.
	convID, err := resolveConversationID(req.Conversation)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if convID != "" && req.PreviousResponseID != nil && *req.PreviousResponseID != "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error",
			"conversation and previous_response_id cannot both be set")
		return
	}
	var conv *conversationState
	if convID != "" {
		if h.convStore == nil {
			writeConversationNotFound(w, convID)
			return
		}
		c, ok := h.convStore.get(tenant, convID)
		if !ok {
			writeConversationNotFound(w, convID)
			return
		}
		conv = c
	}

	// Assemble the conversation: prior turns (a conversation's items, or a
	// previous_response_id), or a fresh `instructions` system message, then this
	// request's input.
	var messages []engine.RequestMessage
	switch {
	case conv != nil:
		prior := conv.messages()
		if len(prior) == 0 && req.Instructions != nil && *req.Instructions != "" {
			messages = append(messages, engine.RequestMessage{Role: "system", Content: *req.Instructions})
		}
		messages = append(messages, prior...)
	case req.PreviousResponseID != nil && *req.PreviousResponseID != "":
		prior, ok := h.store.get(*req.PreviousResponseID)
		if !ok {
			writeError(w, http.StatusNotFound, "invalid_request_error",
				fmt.Sprintf("Previous response with id '%s' not found.", *req.PreviousResponseID))
			return
		}
		messages = append(messages, prior...)
	default:
		if req.Instructions != nil && *req.Instructions != "" {
			messages = append(messages, engine.RequestMessage{Role: "system", Content: *req.Instructions})
		}
	}
	messages = append(messages, inputMsgs...)

	if len(messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "input is required and must not be empty")
		return
	}

	inbound := &engine.InboundRequest{
		Model:            req.Model,
		SessionID:        responsesSessionID(r, req.PreviousResponseID),
		Messages:         messages,
		Stream:           req.Stream,
		ToolChoice:       parseResponsesToolChoice(req.ToolChoice, req.ParallelToolCalls),
		RequestToolNames: responsesToolNames(req.Tools),
		StrictFunctions:  responsesStrictFunctions(req.Tools),
	}
	if meta != nil {
		meta.SessionID = inbound.SessionID
	}

	resp, err := h.Engine.ProcessRequestContext(r.Context(), inbound)
	if err != nil {
		if meta != nil {
			meta.Error = err.Error()
		}
		if ce := engine.AsChaosError(err); ce != nil {
			if ce.Connection != "" {
				if !connectionFault(w, ce.Connection) {
					writeError(w, http.StatusBadGateway, "server_error", "connection fault could not be delivered")
				}
				return
			}
			if ra, ok := chaosRetryAfter(ce); ok {
				w.Header().Set("Retry-After", ra)
			}
			errType, code := openAIChaosError(ce.StatusCode)
			writeErrorCode(w, ce.StatusCode, errType, code, ce.Message)
			return
		}
		if se := engine.AsStrictToolError(err); se != nil {
			writeResponsesStrictError(w, se)
			return
		}
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "empty") {
			status = http.StatusBadRequest
		}
		writeError(w, status, "invalid_request_error", err.Error())
		return
	}

	if meta != nil {
		meta.AgentName = resp.AgentName
		meta.Model = req.Model
		meta.ScenarioName = resp.ScenarioName
		meta.ToolCallsCount = len(resp.ToolCalls)
	}

	// Persist this turn (conversation + assistant reply) for a later
	// previous_response_id, and stamp the new id onto the response object.
	respID := "resp_" + generateID()
	stored := make([]engine.RequestMessage, 0, len(messages)+2)
	stored = append(stored, messages...)
	if resp.Content != "" {
		stored = append(stored, engine.RequestMessage{Role: "assistant", Content: resp.Content})
	}
	// Persist the emitted function_call items too (round-9 R9-2): a
	// previous_response_id tool loop replays this thread, and the convergence
	// guard needs the fingerprint material — previously only text survived.
	if len(resp.ToolCalls) > 0 {
		echoed := make([]engine.EchoedToolCall, 0, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			e := engine.EchoedToolCall{
				Name: tc.Name, Arguments: tc.Arguments, RawArguments: tc.RawArguments}
			// Persist the emitted call_id too (round-11): strict round-trip id
			// validation must see the id across a previous_response_id hop —
			// this store was the one place the material didn't survive.
			if i < len(resp.ToolResults) {
				e.ID = resp.ToolResults[i].ID
			}
			echoed = append(echoed, e)
		}
		stored = append(stored, engine.RequestMessage{Role: "assistant", ToolCalls: echoed})
	}
	h.store.put(respID, stored)

	// When this turn referenced a conversation, append the new input + the
	// assistant output to it so the next turn replays them (the thread-replacement
	// behavior). This is independent of `store`: a conversation's items have their
	// own lifecycle, so `store:false` still records them (store only governs
	// retention of the standalone Response object).
	if conv != nil {
		appended := make([]conversationItem, 0, len(inputMsgs)+1)
		for _, m := range inputMsgs {
			appended = append(appended, userMessageItem(m.Role, m.Content))
		}
		appended = append(appended, assistantItemsFromResponse(resp)...)
		conv.appendItems(appended)
	}

	inputTokens := sumMessageTokens(messages)
	out := buildResponsesResponse(respID, &req, resp, inputTokens)
	if convID != "" {
		out.Conversation = map[string]any{"id": convID}
	}

	setHallucinationHeader(w, resp)
	setStrictViolationHeader(w, resp)

	if req.Stream {
		streamCfg := h.streamConfigFor(r, req.Model)
		if err := streamResponses(r.Context(), w, out, resp, streamCfg); err != nil {
			return // already mid-stream; cannot rewrite headers
		}
		return
	}

	writeJSON(w, http.StatusOK, out)
}

// streamConfigFor resolves the matched agent's streaming physics (chunk size /
// delay / TTFT) the same way the Chat Completions path does, so Responses SSE
// inherits the identical pacing config.
func (h *ResponsesHandler) streamConfigFor(r *http.Request, model string) *types.StreamingConfig {
	tenantID := engine.TenantIDFromContext(r.Context())
	agent := h.Engine.Registry.GetByModelForTenant(model, tenantID)
	if agent == nil {
		if agents := h.Engine.Registry.ListForTenant(tenantID); len(agents) == 1 {
			agent = agents[0]
		}
	}
	if agent != nil {
		return agent.Spec.Behavior.Streaming
	}
	return nil
}

// --- translate-in ---

// parseResponsesInput decodes the polymorphic `input` field into flat engine
// messages. It accepts a bare string (a single user turn) or an array of typed
// input items (messages, function_call_output tool results, echoed
// function_calls).
func parseResponsesInput(raw json.RawMessage) ([]engine.RequestMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Bare string form: {"input": "hello"}.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil, nil
		}
		return []engine.RequestMessage{{Role: "user", Content: s}}, nil
	}

	var items []responsesInputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("input must be a string or an array of input items")
	}

	msgs := make([]engine.RequestMessage, 0, len(items))
	for _, it := range items {
		msgs = append(msgs, responsesItemToMessage(it.Type, it.Role, it.Content, it.Output, it.Name, it.Arguments, it.CallID))
	}
	return msgs, nil
}

// responsesToolNames collects the request's declared function names from the
// raw tools echoes — flat Responses form ({"type":"function","name":…}) with
// the nested Chat form tolerated (round-11).
func responsesToolNames(tools []json.RawMessage) []string {
	var names []string
	for _, raw := range tools {
		var t struct {
			Name     string `json:"name"`
			Function *struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(raw, &t) != nil {
			continue
		}
		name := t.Name
		if name == "" && t.Function != nil {
			name = t.Function.Name
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// responsesStrictFunctions collects strict:true function tools from the raw
// tools echoes (flat Responses form; nested Chat form tolerated) for
// schema-subset validation (round-11 R9-16b).
func responsesStrictFunctions(tools []json.RawMessage) []engine.StrictFunction {
	var out []engine.StrictFunction
	for i, raw := range tools {
		var t struct {
			Name       string         `json:"name"`
			Strict     *bool          `json:"strict"`
			Parameters map[string]any `json:"parameters"`
			Function   *struct {
				Name       string         `json:"name"`
				Strict     *bool          `json:"strict"`
				Parameters map[string]any `json:"parameters"`
			} `json:"function"`
		}
		if json.Unmarshal(raw, &t) != nil {
			continue
		}
		name, strict, params := t.Name, t.Strict, t.Parameters
		if name == "" && t.Function != nil {
			name, strict, params = t.Function.Name, t.Function.Strict, t.Function.Parameters
		}
		if strict != nil && *strict {
			out = append(out, engine.StrictFunction{Index: i, Name: name, Parameters: params})
		}
	}
	return out
}

// parseResponsesToolChoice maps the Responses tool_choice (a string, the flat
// {"type":"function","name":…} object, or the nested Chat form) plus
// parallel_tool_calls into the engine's provider-neutral contract.
func parseResponsesToolChoice(raw json.RawMessage, parallel *bool) engine.ToolChoice {
	out := engine.ToolChoice{ParallelDisabled: parallel != nil && !*parallel}
	if len(raw) == 0 {
		return out
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch s {
		case "none":
			out.None = true
		case "required":
			out.Required = true
		}
		return out
	}
	var obj struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function *struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Type == "function" {
		name := obj.Name
		if name == "" && obj.Function != nil {
			name = obj.Function.Name
		}
		if name != "" {
			out.Name = name
			out.Required = true
		}
	}
	return out
}

// responsesItemToMessage maps a single Responses/Conversations Item to a flat
// engine message. It is the ONE owner of this mapping, shared by
// parseResponsesInput (inline /v1/responses input) and conversations.go's
// conversationItemsToMessages (replayed conversation items) so the two can never
// drift — sending items inline and replaying them from a conversation must
// produce identical history (review finding X-001). The discriminator is the
// item type (plus role for plain messages); content/output are the raw JSON
// payloads each kind carries.
func responsesItemToMessage(itemType, role string, content, output json.RawMessage, name, arguments, callID string) engine.RequestMessage {
	switch itemType {
	case "function_call_output":
		// A tool result fed back in. Map to a "tool" role so it joins the
		// history; its text never becomes the matched user message. The
		// IsToolResult mark feeds the convergence guard (round-9); the call_id
		// is the strict-mode round-trip material (round-11).
		rm := engine.RequestMessage{Role: "tool", Content: rawToString(output), IsToolResult: true}
		if callID != "" {
			rm.ToolResultIDs = []string{callID}
		}
		return rm
	case "function_call":
		// An echoed prior tool call. Keep it in history as an assistant turn; it
		// carries no user-visible text to match on, but its name+arguments are
		// the convergence guard's fingerprint material (round-9).
		echoed := engine.EchoToolCall(name, arguments)
		echoed.ID = callID
		return engine.RequestMessage{Role: "assistant", Content: "",
			ToolCalls: []engine.EchoedToolCall{echoed}}
	default:
		// "message" or a role-only object.
		if role == "" {
			role = "user"
		}
		return engine.RequestMessage{Role: role, Content: extractStringContent(decodeContent(content))}
	}
}

// decodeContent unmarshals an input item's `content` (a string or an array of
// content parts) into the `any` shape extractStringContent already flattens —
// reusing the Chat Completions content-flattening rather than duplicating it.
func decodeContent(raw json.RawMessage) any {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return v
}

// rawToString renders a function_call_output `output` (a string, or an array of
// output parts each carrying a "text"/"output"/"content" field) as plain text.
func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return extractStringContent(decodeContent(raw))
}

// responsesSessionID derives a stable session id so multi-turn conversations
// keyed by previous_response_id share engine session state. An explicit
// X-Session-Id header wins; otherwise a chained turn reuses the prior response
// id as the thread key, and a brand-new conversation gets a fresh id.
func responsesSessionID(r *http.Request, prev *string) string {
	if id := r.Header.Get("X-Session-Id"); id != "" {
		return id
	}
	if prev != nil && *prev != "" {
		return "resp-thread-" + *prev
	}
	return "sess-" + generateID()
}

// --- translate-out ---

// buildResponsesResponse assembles the full (status=completed) Responses
// object from the engine result, echoing the request's own settings back.
func buildResponsesResponse(id string, req *ResponsesRequest, resp *engine.Response, inputTokens int) *ResponsesResponse {
	output, outputTokens := buildResponsesOutput(resp)

	status := "completed"
	var incomplete any // null
	if resp.FinishReason == "length" {
		status = "incomplete"
		incomplete = map[string]any{"reason": "max_output_tokens"}
	}

	out := &ResponsesResponse{
		ID:                id,
		Object:            "response",
		CreatedAt:         time.Now().Unix(),
		Status:            status,
		Error:             nil,
		IncompleteDetails: incomplete,
		Model:             resp.Model,
		Output:            output,
		ParallelToolCalls: boolOr(req.ParallelToolCalls, true),
		Store:             boolOr(req.Store, true),
		Temperature:       floatOr(req.Temperature, 1.0),
		TopP:              floatOr(req.TopP, 1.0),
		Truncation:        "disabled",
		Usage: ResponsesUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
		},
		Metadata: req.Metadata,
	}
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}

	// Echo nullable request fields.
	if req.Instructions != nil {
		out.Instructions = *req.Instructions
	}
	if req.MaxOutputTokens != nil {
		out.MaxOutputTokens = *req.MaxOutputTokens
	}
	if req.PreviousResponseID != nil {
		out.PreviousResponseID = *req.PreviousResponseID
	}
	if req.User != nil {
		out.User = *req.User
	}

	// tool_choice / text / reasoning / tools: echo what the caller sent,
	// else the API defaults.
	out.ToolChoice = rawOr(req.ToolChoice, "auto")
	out.Text = rawOrJSON(req.Text, map[string]any{"format": map[string]any{"type": "text"}})
	out.Reasoning = rawOrJSON(req.Reasoning, map[string]any{"effort": nil, "summary": nil})
	out.Tools = make([]any, 0, len(req.Tools))
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, t)
	}
	return out
}

// buildResponsesOutput renders the engine result into Responses output items
// (a message item for text/refusal plus one function_call item per tool call)
// and returns the items together with the estimated output-token count.
func buildResponsesOutput(resp *engine.Response) ([]any, int) {
	output := make([]any, 0, 1+len(resp.ToolCalls))
	outputTokens := 0

	// A refusal renders as a message item with a single refusal part.
	if resp.Refusal != "" {
		output = append(output, responseMessageItem{
			Type:   "message",
			ID:     "msg_" + generateID(),
			Status: "completed",
			Role:   "assistant",
			Content: []any{responseRefusalPart{
				Type:    "refusal",
				Refusal: resp.Refusal,
			}},
		})
		outputTokens += EstimateTokens(resp.Refusal)
	} else if resp.Content != "" {
		output = append(output, responseMessageItem{
			Type:   "message",
			ID:     "msg_" + generateID(),
			Status: "completed",
			Role:   "assistant",
			Content: []any{responseOutputText{
				Type:        "output_text",
				Text:        resp.Content,
				Annotations: []any{},
			}},
		})
		outputTokens += EstimateTokens(resp.Content)
	}

	for i, tc := range resp.ToolCalls {
		callID := fmt.Sprintf("call_%s", generateID())
		if i < len(resp.ToolResults) && resp.ToolResults[i].ID != "" {
			callID = resp.ToolResults[i].ID
		}
		args := tc.ArgumentsJSON() // raw verbatim or structured, nil → "{}" (R9-6)
		output = append(output, responseFunctionCallItem{
			Type:      "function_call",
			ID:        "fc_" + generateID(),
			CallID:    callID,
			Name:      tc.Name,
			Arguments: args,
			Status:    "completed",
		})
		outputTokens += EstimateTokens(args)
	}

	return output, outputTokens
}

// --- small echo helpers ---

func boolOr(p *bool, def bool) bool {
	if p != nil {
		return *p
	}
	return def
}

func floatOr(p *float64, def float64) float64 {
	if p != nil {
		return *p
	}
	return def
}

// rawOr echoes a raw JSON value, or returns the string default when absent.
func rawOr(raw json.RawMessage, def string) any {
	if len(raw) > 0 {
		return raw
	}
	return def
}

// rawOrJSON echoes a raw JSON value, or returns the structured default.
func rawOrJSON(raw json.RawMessage, def any) any {
	if len(raw) > 0 {
		return raw
	}
	return def
}

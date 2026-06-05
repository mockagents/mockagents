package adapter

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/types"
)

// --- OpenAI Request Types ---

// ChatCompletionRequest represents an OpenAI Chat Completions API request.
type ChatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Tools       []OpenAITool    `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
}

// OpenAIMessage represents a message in an OpenAI request/response.
type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// OpenAITool represents a tool definition in an OpenAI request.
type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction describes a function tool.
type OpenAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// OpenAIToolCall represents a tool call in an OpenAI response.
type OpenAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAIFunctionCall `json:"function"`
}

// OpenAIFunctionCall represents the function invocation in a tool call.
type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// --- OpenAI Response Types ---

// ChatCompletionResponse represents an OpenAI Chat Completions API response.
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   OpenAIUsage            `json:"usage"`
}

// ChatCompletionChoice represents a single choice in the response.
type ChatCompletionChoice struct {
	Index        int                   `json:"index"`
	Message      OpenAIResponseMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

// OpenAIResponseMessage is the assistant message in a choice.
type OpenAIResponseMessage struct {
	Role      string           `json:"role"`
	Content   *string          `json:"content"`
	ToolCalls []OpenAIToolCall `json:"tool_calls,omitempty"`
}

// OpenAIUsage represents token usage in the response.
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- OpenAI Handler ---

// OpenAIHandler handles OpenAI Chat Completions API requests.
type OpenAIHandler struct {
	Engine *engine.Engine
}

// ProtocolOpenAIChat is the wire-protocol label recorded on interaction
// logs for this endpoint; it matches the agent-spec `protocol` enum value.
const ProtocolOpenAIChat = "openai-chat-completions"

// HandleChatCompletions handles POST /v1/chat/completions.
func (h *OpenAIHandler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// Stamp the protocol first so even a malformed request that never
	// reaches the engine still logs which surface it hit.
	meta := engine.RequestMetaFromContext(r.Context())
	if meta != nil {
		meta.Protocol = ProtocolOpenAIChat
	}

	var req ChatCompletionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "messages array is required and must not be empty")
		return
	}

	// Convert to engine request.
	inbound := &engine.InboundRequest{
		Model:     req.Model,
		SessionID: extractSessionID(r),
		Messages:  convertOpenAIMessages(req.Messages),
		Stream:    req.Stream,
	}
	if meta != nil {
		meta.SessionID = inbound.SessionID
	}

	// Process through engine.
	resp, err := h.Engine.ProcessRequestContext(r.Context(), inbound)
	if err != nil {
		if meta != nil {
			meta.Error = err.Error()
		}
		if ce := engine.AsChaosError(err); ce != nil {
			if ce.RetryAfter > 0 {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(ce.RetryAfter.Seconds())))
			}
			writeError(w, ce.StatusCode, chaosErrorType(ce), ce.Message)
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

	// Stamp the matched agent + scenario onto the request meta so the
	// InteractionCapture middleware can record the real agent name and
	// scenario (instead of falling back to a model-name probe of the body).
	if meta != nil {
		meta.AgentName = resp.AgentName
		meta.Model = req.Model
		meta.ScenarioName = resp.ScenarioName
		meta.ToolCallsCount = len(resp.ToolCalls)
	}

	// Stream or JSON response.
	if req.Stream {
		tenantID := engine.TenantIDFromContext(r.Context())
		agent := h.Engine.Registry.GetByModelForTenant(req.Model, tenantID)
		if agent == nil {
			agents := h.Engine.Registry.ListForTenant(tenantID)
			if len(agents) == 1 {
				agent = agents[0]
			}
		}
		var streamCfg *types.StreamingConfig
		if agent != nil {
			streamCfg = agent.Spec.Behavior.Streaming
		}
		if err := streaming.StreamOpenAI(r.Context(), w, resp, streamCfg); err != nil {
			// Already started streaming; can't write error JSON.
			return
		}
		return
	}

	// Build non-streaming response. Count prompt tokens off the already-flattened
	// inbound.Messages rather than re-extracting req.Messages (PERF-19).
	promptTokens := sumMessageTokens(inbound.Messages)
	completionTokens := EstimateTokens(resp.Content)

	openAIResp := formatOpenAIResponse(resp, promptTokens, completionTokens)
	writeJSON(w, http.StatusOK, openAIResp)
}

// HandleModels handles GET /v1/models.
func (h *OpenAIHandler) HandleModels(w http.ResponseWriter, r *http.Request) {
	agents := h.Engine.Registry.ListForTenant(engine.TenantIDFromContext(r.Context()))
	models := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		models = append(models, map[string]any{
			"id":       a.Spec.Model,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": "mockagents",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   models,
	})
}

// --- Conversion Helpers ---

func convertOpenAIMessages(msgs []OpenAIMessage) []engine.RequestMessage {
	result := make([]engine.RequestMessage, 0, len(msgs))
	for _, m := range msgs {
		content := extractStringContent(m.Content)
		result = append(result, engine.RequestMessage{
			Role:    m.Role,
			Content: content,
		})
	}
	return result
}

func extractStringContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		// Content array format — extract text parts.
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, " ")
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

func formatOpenAIResponse(resp *engine.Response, promptTokens, completionTokens int) *ChatCompletionResponse {
	finishReason := "stop"
	var toolCalls []OpenAIToolCall

	if len(resp.ToolCalls) > 0 {
		finishReason = "tool_calls"
		for i, tc := range resp.ToolCalls {
			callID := fmt.Sprintf("call_%d", i)
			if i < len(resp.ToolResults) {
				callID = resp.ToolResults[i].ID
			}
			argsJSON, _ := json.Marshal(tc.Arguments)
			toolCalls = append(toolCalls, OpenAIToolCall{
				ID:   callID,
				Type: "function",
				Function: OpenAIFunctionCall{
					Name:      tc.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	var content *string
	if resp.Content != "" {
		content = &resp.Content
	}

	return &ChatCompletionResponse{
		ID:      "chatcmpl-" + generateID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   resp.Model,
		Choices: []ChatCompletionChoice{
			{
				Index: 0,
				Message: OpenAIResponseMessage{
					Role:      "assistant",
					Content:   content,
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// sumMessageTokens counts prompt tokens off the already-flattened engine
// messages. Both adapters convert their wire messages into
// []engine.RequestMessage — flattening multi-part content via
// extractStringContent / extractAnthropicContent — before calling the engine,
// so re-extracting the raw wire content for the token estimate would repeat that
// work, including a strings.Join + slice alloc per multi-part message (PERF-19).
// For Anthropic the prepended system message is already part of the flattened
// slice, so the totals match the old system-plus-messages split exactly.
func sumMessageTokens(msgs []engine.RequestMessage) int {
	total := 0
	for i := range msgs {
		total += EstimateTokens(msgs[i].Content)
	}
	return total
}

func extractSessionID(r *http.Request) string {
	if id := r.Header.Get("X-Session-Id"); id != "" {
		return id
	}
	return "sess-" + generateID()
}

// generateID returns a unique, non-cryptographic id for responses, sessions,
// tool calls, and messages. These label interactions; they are not security
// tokens, so uniqueness (not unpredictability) is the requirement. math/rand/v2
// avoids the crypto/rand syscall and the fmt.Sprintf hex format the old path
// paid on every id (PERF-07).
func generateID() string {
	return strconv.FormatUint(rand.Uint64(), 16)
}

// openAIError is the OpenAI error envelope ({"error":{"type","message"}}). A
// fixed struct avoids the two nested map allocations the literal incurred on
// every 4xx/5xx — the hot path under chaos error storms (PERF-16).
type openAIError struct {
	Error openAIErrorBody `json:"error"`
}

type openAIErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, openAIError{Error: openAIErrorBody{Type: errType, Message: message}})
}

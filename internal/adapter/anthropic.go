package adapter

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/types"
)

// --- Anthropic Request Types ---

// AnthropicRequest represents an Anthropic Messages API request.
type AnthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []AnthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
	Tools     []AnthropicTool    `json:"tools,omitempty"`
	MaxTokens int                `json:"max_tokens,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
}

// AnthropicMessage represents a message in an Anthropic request.
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// AnthropicTool represents a tool in an Anthropic request.
type AnthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// --- Anthropic Response Types ---

// AnthropicResponse represents an Anthropic Messages API response.
type AnthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []AnthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        AnthropicUsage     `json:"usage"`
}

// AnthropicContent represents a content block in the response.
type AnthropicContent struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

// AnthropicUsage represents token usage.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// --- Anthropic Handler ---

// AnthropicHandler handles Anthropic Messages API requests.
type AnthropicHandler struct {
	Engine *engine.Engine
}

// Name identifies this adapter in logs and diagnostics.
func (h *AnthropicHandler) Name() string { return "anthropic" }

// Routes returns the Anthropic-compatible routes this adapter serves,
// mounted by the server through the adapter Registry (REF-05).
func (h *AnthropicHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/messages", Handler: h.HandleMessages},
	}
}

// ProtocolAnthropicMessages is the wire-protocol label recorded on
// interaction logs for this endpoint; it matches the agent-spec
// `protocol` enum value.
const ProtocolAnthropicMessages = "anthropic-messages"

// HandleMessages handles POST /v1/messages.
func (h *AnthropicHandler) HandleMessages(w http.ResponseWriter, r *http.Request) {
	// Stamp the protocol first so even a malformed request that never
	// reaches the engine still logs which surface it hit.
	meta := engine.RequestMetaFromContext(r.Context())
	if meta != nil {
		meta.Protocol = ProtocolAnthropicMessages
	}

	var req AnthropicRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()

	if req.Model == "" {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "messages is required")
		return
	}

	// Validate API key header.
	apiKey := r.Header.Get("X-Api-Key")
	authHeader := r.Header.Get("Authorization")
	if apiKey == "" && !strings.HasPrefix(authHeader, "Bearer ") {
		writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "missing API key")
		return
	}

	// Convert to engine request.
	inbound := &engine.InboundRequest{
		Model:     req.Model,
		SessionID: extractSessionID(r),
		Messages:  convertAnthropicMessages(req.Messages, req.System),
		Stream:    req.Stream,
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
			if ce.RetryAfter > 0 {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(ce.RetryAfter.Seconds())))
			}
			writeAnthropicError(w, ce.StatusCode, chaosErrorType(ce), ce.Message)
			return
		}
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "empty") {
			status = http.StatusBadRequest
		}
		writeAnthropicError(w, status, "invalid_request_error", err.Error())
		return
	}

	// Stamp the matched agent + scenario onto the request meta so the
	// InteractionCapture middleware can record the real agent name and
	// scenario instead of probing the response body for a model name.
	if meta != nil {
		meta.AgentName = resp.AgentName
		meta.Model = req.Model
		meta.ScenarioName = resp.ScenarioName
		meta.ToolCallsCount = len(resp.ToolCalls)
	}

	// Stream or JSON.
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
		if err := streaming.StreamAnthropic(r.Context(), w, resp, streamCfg); err != nil {
			return
		}
		return
	}

	// Non-streaming response. Count prompt tokens off the already-flattened
	// inbound.Messages (the system message is prepended there) rather than
	// re-extracting req.Messages + req.System (PERF-19).
	inputTokens := sumMessageTokens(inbound.Messages)
	outputTokens := EstimateTokens(resp.Content)

	anthropicResp := formatAnthropicResponse(resp, inputTokens, outputTokens)
	writeJSON(w, http.StatusOK, anthropicResp)
}

// --- Conversion Helpers ---

func convertAnthropicMessages(msgs []AnthropicMessage, system string) []engine.RequestMessage {
	// Pre-size for the worst case (every message + an optional system prepend) so
	// the append loop never grows the slice (PERF-15; the OpenAI twin already
	// pre-sizes).
	result := make([]engine.RequestMessage, 0, len(msgs)+1)

	// Prepend system message if provided.
	if system != "" {
		result = append(result, engine.RequestMessage{
			Role:    "system",
			Content: system,
		})
	}

	for _, m := range msgs {
		content := extractAnthropicContent(m.Content)
		result = append(result, engine.RequestMessage{
			Role:    m.Role,
			Content: content,
		})
	}
	return result
}

func extractAnthropicContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				blockType, _ := m["type"].(string)
				switch blockType {
				case "text":
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				case "tool_result":
					if c, ok := m["content"].(string); ok {
						parts = append(parts, c)
					}
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

func formatAnthropicResponse(resp *engine.Response, inputTokens, outputTokens int) *AnthropicResponse {
	var content []AnthropicContent

	// Text content block.
	if resp.Content != "" {
		content = append(content, AnthropicContent{
			Type: "text",
			Text: resp.Content,
		})
	}

	// Tool use blocks.
	stopReason := "end_turn"
	if len(resp.ToolCalls) > 0 {
		stopReason = "tool_use"
		for i, tc := range resp.ToolCalls {
			toolID := "toolu_" + generateID()
			if i < len(resp.ToolResults) {
				toolID = resp.ToolResults[i].ID
			}
			content = append(content, AnthropicContent{
				Type:  "tool_use",
				ID:    toolID,
				Name:  tc.Name,
				Input: tc.Arguments,
			})
		}
	}

	return &AnthropicResponse{
		ID:         "msg_" + generateID(),
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		Model:      resp.Model,
		StopReason: stopReason,
		Usage: AnthropicUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
	}
}

// anthropicErrorEnvelope is the Anthropic error shape
// ({"type":"error","error":{"type","message"}}). Fixed struct, no nested map
// allocations on the chaos-storm 4xx/5xx path (PERF-16).
type anthropicErrorEnvelope struct {
	Type  string             `json:"type"` // always "error"
	Error anthropicErrorBody `json:"error"`
}

type anthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func writeAnthropicError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, anthropicErrorEnvelope{
		Type:  "error",
		Error: anthropicErrorBody{Type: errType, Message: message},
	})
}

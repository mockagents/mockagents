package adapter

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
)

const ProtocolBedrockConverse = "bedrock-converse"

type BedrockHandler struct{ Engine *engine.Engine }

func (h *BedrockHandler) Name() string { return "bedrock" }
func (h *BedrockHandler) Routes() []Route {
	return []Route{{Pattern: "POST /model/{modelId}/converse", Handler: h.HandleConverse}}
}

type BedrockConverseRequest struct {
	Messages   []BedrockMessage      `json:"messages"`
	System     []BedrockContentBlock `json:"system,omitempty"`
	ToolConfig *BedrockToolConfig    `json:"toolConfig,omitempty"`
}

type BedrockMessage struct {
	Role    string                `json:"role"`
	Content []BedrockContentBlock `json:"content"`
}

type BedrockContentBlock struct {
	Text       string             `json:"text,omitempty"`
	Image      *BedrockImageBlock `json:"image,omitempty"`
	ToolUse    *BedrockToolUse    `json:"toolUse,omitempty"`
	ToolResult *BedrockToolResult `json:"toolResult,omitempty"`
}

type BedrockImageBlock struct {
	Format string         `json:"format"`
	Source map[string]any `json:"source"`
}

type BedrockToolUse struct {
	ToolUseID string         `json:"toolUseId"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
}

type BedrockToolResult struct {
	ToolUseID string                `json:"toolUseId"`
	Content   []BedrockContentBlock `json:"content"`
	Status    string                `json:"status,omitempty"`
}

type BedrockToolConfig struct {
	Tools []BedrockTool `json:"tools"`
}

type BedrockTool struct {
	ToolSpec BedrockToolSpec `json:"toolSpec"`
}

type BedrockToolSpec struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	InputSchema BedrockInputSchema `json:"inputSchema"`
}

type BedrockInputSchema struct {
	JSON map[string]any `json:"json"`
}

type BedrockConverseResponse struct {
	Output     BedrockOutput  `json:"output"`
	StopReason string         `json:"stopReason"`
	Usage      BedrockUsage   `json:"usage"`
	Metrics    BedrockMetrics `json:"metrics"`
}

type BedrockOutput struct {
	Message BedrockMessage `json:"message"`
}
type BedrockUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}
type BedrockMetrics struct {
	LatencyMS int `json:"latencyMs"`
}

func (h *BedrockHandler) HandleConverse(w http.ResponseWriter, r *http.Request) {
	model := strings.TrimSpace(r.PathValue("modelId"))
	if model == "" {
		writeBedrockError(w, http.StatusBadRequest, "ValidationException", "modelId is required")
		return
	}
	if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
		meta.Protocol, meta.Model = ProtocolBedrockConverse, model
	}
	var req BedrockConverseRequest
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeBedrockError(w, http.StatusRequestEntityTooLarge, "ValidationException", "request body too large")
			return
		}
		writeBedrockError(w, http.StatusBadRequest, "ValidationException", "invalid JSON: "+err.Error())
		return
	}
	defer r.Body.Close()
	if len(req.Messages) == 0 {
		writeBedrockError(w, http.StatusBadRequest, "ValidationException", "messages is required")
		return
	}
	messages, images := convertBedrockMessages(req.System, req.Messages)
	inbound := &engine.InboundRequest{Model: model, SessionID: extractSessionID(r), Messages: messages, RequestToolNames: bedrockToolNames(req.ToolConfig)}
	if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
		meta.SessionID = inbound.SessionID
	}
	resp, err := h.Engine.ProcessRequestContext(r.Context(), inbound)
	if err != nil {
		if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
			meta.Error = err.Error()
		}
		if ce := engine.AsChaosError(err); ce != nil {
			if ce.Connection != "" {
				if !connectionFault(w, ce.Connection) {
					writeBedrockError(w, http.StatusBadGateway, "InternalServerException", "connection fault could not be delivered")
				}
				return
			}
			if retry, ok := chaosRetryAfter(ce); ok {
				w.Header().Set("Retry-After", retry)
			}
			writeBedrockError(w, ce.StatusCode, bedrockErrorType(ce.StatusCode), ce.Message)
			return
		}
		if engine.AsStrictToolError(err) != nil {
			writeBedrockError(w, http.StatusBadRequest, "ValidationException", err.Error())
			return
		}
		status, kind := http.StatusInternalServerError, "InternalServerException"
		if strings.Contains(err.Error(), "not found") {
			status, kind = http.StatusNotFound, "ResourceNotFoundException"
		}
		writeBedrockError(w, status, kind, err.Error())
		return
	}
	if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
		meta.AgentName, meta.ScenarioName, meta.ToolCallsCount = resp.AgentName, resp.ScenarioName, len(resp.ToolCalls)
	}
	setHallucinationHeader(w, resp)
	setStrictViolationHeader(w, resp)
	setImageCountHeader(w, images)
	content := []BedrockContentBlock{}
	if resp.Content != "" {
		content = append(content, BedrockContentBlock{Text: resp.Content})
	}
	for i, call := range resp.ToolCalls {
		id := "tooluse_" + generateID()
		if i < len(resp.ToolResults) && resp.ToolResults[i].ID != "" {
			id = "tooluse_" + strings.TrimPrefix(resp.ToolResults[i].ID, "call_")
		}
		content = append(content, BedrockContentBlock{ToolUse: &BedrockToolUse{ToolUseID: id, Name: call.Name, Input: call.ArgumentsObject()}})
	}
	stop := bedrockStopReason(resp.FinishReason, len(resp.ToolCalls) > 0)
	inTokens, outTokens := sumMessageTokens(messages), EstimateTokens(resp.Content)
	writeJSON(w, http.StatusOK, BedrockConverseResponse{Output: BedrockOutput{Message: BedrockMessage{Role: "assistant", Content: content}}, StopReason: stop, Usage: BedrockUsage{InputTokens: inTokens, OutputTokens: outTokens, TotalTokens: inTokens + outTokens}, Metrics: BedrockMetrics{LatencyMS: 0}})
}

func convertBedrockMessages(system []BedrockContentBlock, messages []BedrockMessage) ([]engine.RequestMessage, int) {
	out := make([]engine.RequestMessage, 0, len(messages)+1)
	if text := bedrockContentText(system); text != "" {
		out = append(out, engine.RequestMessage{Role: "system", Content: text})
	}
	images := 0
	for _, message := range messages {
		rm := engine.RequestMessage{Role: message.Role, Content: bedrockContentText(message.Content)}
		for _, block := range message.Content {
			if block.Image != nil {
				rm.ImageCount++
				images++
			}
			if block.ToolUse != nil {
				raw, _ := json.Marshal(block.ToolUse.Input)
				rm.ToolCalls = append(rm.ToolCalls, engine.EchoedToolCall{ID: block.ToolUse.ToolUseID, Name: block.ToolUse.Name, Arguments: block.ToolUse.Input, RawArguments: string(raw)})
			}
			if block.ToolResult != nil {
				rm.IsToolResult = true
				rm.ToolResultIDs = append(rm.ToolResultIDs, block.ToolResult.ToolUseID)
				rm.Content += bedrockContentText(block.ToolResult.Content)
			}
		}
		out = append(out, rm)
	}
	return out, images
}

func bedrockContentText(blocks []BedrockContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
func bedrockToolNames(config *BedrockToolConfig) []string {
	if config == nil {
		return nil
	}
	names := make([]string, 0, len(config.Tools))
	for _, tool := range config.Tools {
		if tool.ToolSpec.Name != "" {
			names = append(names, tool.ToolSpec.Name)
		}
	}
	return names
}
func bedrockStopReason(reason string, tools bool) string {
	if tools {
		return "tool_use"
	}
	switch reason {
	case "length", "max_tokens":
		return "max_tokens"
	case "stop_sequence":
		return "stop_sequence"
	case "content_filter", "content_filtered":
		return "content_filtered"
	default:
		return "end_turn"
	}
}
func bedrockErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "ValidationException"
	case http.StatusNotFound:
		return "ResourceNotFoundException"
	case http.StatusTooManyRequests:
		return "ThrottlingException"
	case http.StatusServiceUnavailable:
		return "ServiceUnavailableException"
	default:
		return "InternalServerException"
	}
}
func writeBedrockError(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("x-amzn-errortype", kind)
	writeJSON(w, status, struct {
		Message string `json:"message"`
	}{Message: message})
}

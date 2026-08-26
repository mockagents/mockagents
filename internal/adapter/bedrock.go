package adapter

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
)

const ProtocolBedrockConverse = "bedrock-converse"

type BedrockHandler struct{ Engine *engine.Engine }

func (h *BedrockHandler) Name() string { return "bedrock" }
func (h *BedrockHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /model/{modelId}/converse", Handler: h.HandleConverse},
		{Pattern: "POST /model/{modelId}/converse-stream", Handler: h.HandleConverseStream},
	}
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

type bedrockCapture struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (c *bedrockCapture) Header() http.Header    { return c.header }
func (c *bedrockCapture) WriteHeader(status int) { c.status = status }
func (c *bedrockCapture) Write(data []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(data)
}

// HandleConverseStream emits the same AWS EventStream frames that SDK
// ConverseStream decoders consume. Each frame carries the required pseudo
// headers and CRC checksums; the JSON payload is one Bedrock stream event.
func (h *BedrockHandler) HandleConverseStream(w http.ResponseWriter, r *http.Request) {
	model := strings.TrimSpace(r.PathValue("modelId"))
	if model == "" {
		writeBedrockError(w, http.StatusBadRequest, "ValidationException", "modelId is required")
		return
	}
	capture := &bedrockCapture{header: make(http.Header)}
	inner := r.Clone(r.Context())
	inner.SetPathValue("modelId", model)
	h.HandleConverse(capture, inner)
	for key, values := range capture.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if capture.status != http.StatusOK {
		w.WriteHeader(capture.status)
		_, _ = w.Write(capture.body.Bytes())
		return
	}
	var response BedrockConverseResponse
	if err := json.Unmarshal(capture.body.Bytes(), &response); err != nil {
		writeBedrockError(w, http.StatusInternalServerError, "InternalServerException", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	w.WriteHeader(http.StatusOK)
	writeBedrockEvent(w, "messageStart", map[string]any{"role": "assistant"})
	for index, block := range response.Output.Message.Content {
		if block.ToolUse != nil {
			writeBedrockEvent(w, "contentBlockStart", map[string]any{"contentBlockIndex": index, "start": map[string]any{"toolUse": map[string]any{"toolUseId": block.ToolUse.ToolUseID, "name": block.ToolUse.Name}}})
			input, _ := json.Marshal(block.ToolUse.Input)
			writeBedrockEvent(w, "contentBlockDelta", map[string]any{"contentBlockIndex": index, "delta": map[string]any{"toolUse": map[string]any{"input": string(input)}}})
		} else {
			writeBedrockEvent(w, "contentBlockDelta", map[string]any{"contentBlockIndex": index, "delta": map[string]any{"text": block.Text}})
		}
		writeBedrockEvent(w, "contentBlockStop", map[string]any{"contentBlockIndex": index})
	}
	writeBedrockEvent(w, "messageStop", map[string]any{"stopReason": response.StopReason})
	writeBedrockEvent(w, "metadata", map[string]any{"usage": response.Usage, "metrics": response.Metrics})
}

func writeBedrockEvent(w http.ResponseWriter, eventType string, value any) {
	payload, _ := json.Marshal(value)
	headers := appendBedrockEventHeader(nil, ":message-type", "event")
	headers = appendBedrockEventHeader(headers, ":event-type", eventType)
	headers = appendBedrockEventHeader(headers, ":content-type", "application/json")
	total := 16 + len(headers) + len(payload)
	message := make([]byte, 12, total)
	binary.BigEndian.PutUint32(message[0:4], uint32(total))
	binary.BigEndian.PutUint32(message[4:8], uint32(len(headers)))
	binary.BigEndian.PutUint32(message[8:12], crc32.ChecksumIEEE(message[:8]))
	message = append(message, headers...)
	message = append(message, payload...)
	checksum := crc32.ChecksumIEEE(message)
	message = binary.BigEndian.AppendUint32(message, checksum)
	_, _ = w.Write(message)
}

func appendBedrockEventHeader(dst []byte, name, value string) []byte {
	dst = append(dst, byte(len(name)))
	dst = append(dst, name...)
	dst = append(dst, 7) // AWS EventStream string header type
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(value)))
	dst = append(dst, value...)
	return dst
}

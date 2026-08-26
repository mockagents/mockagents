package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
)

const ProtocolOllamaChat = "ollama-chat"

type OllamaHandler struct {
	Engine *engine.Engine
}

func (h *OllamaHandler) Name() string { return "ollama" }

func (h *OllamaHandler) Routes() []Route {
	return []Route{{Pattern: "POST /api/chat", Handler: h.HandleChat}}
}

type OllamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Tools    []OpenAITool    `json:"tools,omitempty"`
	Stream   *bool           `json:"stream,omitempty"`
}

type OllamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking,omitempty"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
}

type OllamaToolCall struct {
	Function OllamaFunctionCall `json:"function"`
}

type OllamaFunctionCall struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Arguments   map[string]any `json:"arguments"`
}

type OllamaChatResponse struct {
	Model              string        `json:"model"`
	CreatedAt          string        `json:"created_at"`
	Message            OllamaMessage `json:"message"`
	Done               bool          `json:"done"`
	DoneReason         string        `json:"done_reason,omitempty"`
	TotalDuration      int64         `json:"total_duration,omitempty"`
	LoadDuration       int64         `json:"load_duration,omitempty"`
	PromptEvalCount    int           `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64         `json:"prompt_eval_duration,omitempty"`
	EvalCount          int           `json:"eval_count,omitempty"`
	EvalDuration       int64         `json:"eval_duration,omitempty"`
}

func (h *OllamaHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
	if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
		meta.Protocol = ProtocolOllamaChat
	}
	var req OllamaChatRequest
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeOllamaError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeOllamaError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	defer r.Body.Close()
	if strings.TrimSpace(req.Model) == "" {
		writeOllamaError(w, http.StatusBadRequest, "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeOllamaError(w, http.StatusBadRequest, "messages is required")
		return
	}

	messages, images := convertOllamaMessages(req.Messages)
	inbound := &engine.InboundRequest{
		Model:            req.Model,
		SessionID:        extractSessionID(r),
		Messages:         messages,
		Stream:           req.Stream == nil || *req.Stream,
		RequestToolNames: openAIToolNames(req.Tools),
		StrictFunctions:  openAIStrictFunctions(req.Tools),
	}
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
					writeOllamaError(w, http.StatusBadGateway, "connection fault could not be delivered")
				}
				return
			}
			if ra, ok := chaosRetryAfter(ce); ok {
				w.Header().Set("Retry-After", ra)
			}
			writeOllamaError(w, ce.StatusCode, ce.Message)
			return
		}
		if engine.AsStrictToolError(err) != nil {
			writeOllamaError(w, http.StatusBadRequest, err.Error())
			return
		}
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "empty") {
			status = http.StatusBadRequest
		}
		writeOllamaError(w, status, err.Error())
		return
	}
	if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
		meta.AgentName = resp.AgentName
		meta.Model = req.Model
		meta.ScenarioName = resp.ScenarioName
		meta.ToolCallsCount = len(resp.ToolCalls)
	}
	setHallucinationHeader(w, resp)
	setStrictViolationHeader(w, resp)
	setImageCountHeader(w, images)

	message := OllamaMessage{Role: "assistant", Content: resp.Content, ToolCalls: ollamaToolCalls(resp)}
	doneReason := resp.FinishReason
	if doneReason == "" {
		doneReason = "stop"
		if len(message.ToolCalls) > 0 {
			doneReason = "tool_calls"
		}
	}
	created := time.Now().UTC().Format(time.RFC3339Nano)
	promptTokens := sumMessageTokens(messages)
	completionTokens := EstimateTokens(resp.Content)
	if inbound.Stream {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		_ = enc.Encode(OllamaChatResponse{Model: req.Model, CreatedAt: created, Message: message, Done: false})
		_ = enc.Encode(OllamaChatResponse{Model: req.Model, CreatedAt: created, Message: OllamaMessage{Role: "assistant", Content: ""}, Done: true, DoneReason: doneReason, PromptEvalCount: promptTokens, EvalCount: completionTokens})
		return
	}
	writeJSON(w, http.StatusOK, OllamaChatResponse{Model: req.Model, CreatedAt: created, Message: message, Done: true, DoneReason: doneReason, PromptEvalCount: promptTokens, EvalCount: completionTokens})
}

func convertOllamaMessages(messages []OllamaMessage) ([]engine.RequestMessage, int) {
	out := make([]engine.RequestMessage, 0, len(messages))
	images := 0
	for _, message := range messages {
		rm := engine.RequestMessage{Role: message.Role, Content: message.Content, ImageCount: len(message.Images), IsToolResult: message.Role == "tool"}
		images += len(message.Images)
		for _, call := range message.ToolCalls {
			arguments, _ := json.Marshal(call.Function.Arguments)
			rm.ToolCalls = append(rm.ToolCalls, engine.EchoToolCall(call.Function.Name, string(arguments)))
		}
		out = append(out, rm)
	}
	return out, images
}

func ollamaToolCalls(resp *engine.Response) []OllamaToolCall {
	if len(resp.ToolCalls) == 0 {
		return nil
	}
	out := make([]OllamaToolCall, 0, len(resp.ToolCalls))
	for _, call := range resp.ToolCalls {
		out = append(out, OllamaToolCall{Function: OllamaFunctionCall{Name: call.Name, Arguments: call.ArgumentsObject()}})
	}
	return out
}

func writeOllamaError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: fmt.Sprint(message)})
}

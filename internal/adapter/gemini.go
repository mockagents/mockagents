package adapter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/types"
)

// --- Gemini Request Types ---

// GeminiRequest represents a Google Gemini generateContent request body.
// The model and method live in the URL path
// (`/v1beta/models/{model}:generateContent`), not the body.
type GeminiRequest struct {
	Contents          []GeminiContent           `json:"contents"`
	SystemInstruction *GeminiContent            `json:"systemInstruction,omitempty"`
	Tools             []GeminiToolDeclaration   `json:"tools,omitempty"`
	GenerationConfig  map[string]any            `json:"generationConfig,omitempty"`
}

// GeminiContent is one turn ("user" / "model") or the system instruction,
// carrying one or more parts.
type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart is a single content part. Text, functionCall, and functionResponse
// are modeled; other part kinds (inlineData, fileData) are accepted and ignored.
type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

// GeminiFunctionResponse is a tool result the client sends back on a follow-up
// turn (role "user"/"function"). Its content is surfaced into the flattened
// message so scenario matching sees tool-result follow-ups.
type GeminiFunctionResponse struct {
	Name     string         `json:"name,omitempty"`
	Response map[string]any `json:"response,omitempty"`
}

// GeminiToolDeclaration wraps the functionDeclarations array of a tool.
type GeminiToolDeclaration struct {
	FunctionDeclarations []GeminiFunctionDecl `json:"functionDeclarations,omitempty"`
}

// GeminiFunctionDecl describes a callable function tool.
type GeminiFunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// GeminiFunctionCall is an emitted function/tool call.
type GeminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// --- Gemini Response Types ---

// GeminiResponse represents a Gemini generateContent response.
type GeminiResponse struct {
	Candidates    []GeminiCandidate    `json:"candidates"`
	UsageMetadata GeminiUsageMetadata  `json:"usageMetadata"`
	ModelVersion  string               `json:"modelVersion,omitempty"`
}

// GeminiCandidate is a single generation candidate.
type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
	Index        int           `json:"index"`
}

// GeminiUsageMetadata reports token counts in Gemini's field names.
type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// --- Gemini Handler ---

// GeminiHandler handles Google Gemini generateContent API requests.
type GeminiHandler struct {
	Engine *engine.Engine
}

// Name identifies this adapter in logs and diagnostics.
func (h *GeminiHandler) Name() string { return "gemini" }

// Routes returns the Gemini-compatible routes this adapter serves, mounted by
// the server through the adapter Registry (REF-05). Gemini encodes the model
// and method in the final path segment ("{model}:generateContent" or
// "{model}:streamGenerateContent"), so a single wildcard route captures both.
func (h *GeminiHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1beta/models/{modelmethod}", Handler: h.HandleGenerate},
	}
}

// ProtocolGoogleGemini is the wire-protocol label recorded on interaction logs
// for this endpoint; it matches the agent-spec `protocol` enum value.
const ProtocolGoogleGemini = "google-gemini"

// HandleGenerate handles POST /v1beta/models/{model}:generateContent and
// :streamGenerateContent.
func (h *GeminiHandler) HandleGenerate(w http.ResponseWriter, r *http.Request) {
	// Stamp the protocol first so even a malformed request that never reaches
	// the engine still logs which surface it hit.
	meta := engine.RequestMetaFromContext(r.Context())
	if meta != nil {
		meta.Protocol = ProtocolGoogleGemini
	}

	// The final path segment is "{model}:{method}", e.g.
	// "gemini-1.5-pro:generateContent". Model names contain no colon, so a
	// single Cut on the first colon is unambiguous.
	model, method, ok := strings.Cut(r.PathValue("modelmethod"), ":")
	if !ok || model == "" {
		writeGeminiError(w, http.StatusBadRequest, "INVALID_ARGUMENT",
			"path must be /v1beta/models/{model}:generateContent")
		return
	}
	stream := method == "streamGenerateContent"

	var req GeminiRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeGeminiError(w, http.StatusBadRequest, "INVALID_ARGUMENT", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()

	if len(req.Contents) == 0 {
		writeGeminiError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "contents is required and must not be empty")
		return
	}

	inbound := &engine.InboundRequest{
		Model:     model,
		SessionID: extractSessionID(r),
		Messages:  convertGeminiContents(req.Contents, req.SystemInstruction),
		Stream:    stream,
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
			if ra, ok := chaosRetryAfter(ce); ok {
				w.Header().Set("Retry-After", ra)
			}
			writeGeminiError(w, ce.StatusCode, geminiStatusFor(ce.StatusCode), ce.Message)
			return
		}
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "empty") {
			status = http.StatusBadRequest
		}
		writeGeminiError(w, status, geminiStatusFor(status), err.Error())
		return
	}

	// Stamp the matched agent + scenario onto the request meta so the
	// InteractionCapture middleware records the real agent name and scenario.
	if meta != nil {
		meta.AgentName = resp.AgentName
		meta.Model = model
		meta.ScenarioName = resp.ScenarioName
		meta.ToolCallsCount = len(resp.ToolCalls)
	}

	setHallucinationHeader(w, resp)

	// Count prompt tokens off the already-flattened inbound.Messages (the system
	// instruction is prepended there) rather than re-extracting req.Contents
	// (PERF-19, matching the OpenAI/Anthropic twins).
	promptTokens := sumMessageTokens(inbound.Messages)
	candidateTokens := EstimateTokens(resp.Content)

	// streamGenerateContent has two wire modes: with ?alt=sse it returns a
	// text/event-stream; WITHOUT alt=sse the real API returns a streamed JSON
	// array of GenerateContentResponse objects (application/json).
	if stream && r.URL.Query().Get("alt") == "sse" {
		tenantID := engine.TenantIDFromContext(r.Context())
		agent := h.Engine.Registry.GetByModelForTenant(model, tenantID)
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
		if err := streaming.StreamGemini(r.Context(), w, resp, streamCfg, promptTokens, candidateTokens); err != nil {
			return
		}
		return
	}

	full := formatGeminiResponse(resp, model, promptTokens, candidateTokens)
	if stream {
		// Non-SSE streamGenerateContent: a JSON array of GenerateContentResponse.
		writeJSON(w, http.StatusOK, []*GeminiResponse{full})
		return
	}
	writeJSON(w, http.StatusOK, full)
}

// --- Conversion Helpers ---

func convertGeminiContents(contents []GeminiContent, system *GeminiContent) []engine.RequestMessage {
	// Pre-size for the worst case (every content + an optional system prepend)
	// so the append loop never grows the slice (PERF-15).
	result := make([]engine.RequestMessage, 0, len(contents)+1)

	if system != nil {
		if text := joinGeminiParts(system.Parts); text != "" {
			result = append(result, engine.RequestMessage{Role: "system", Content: text})
		}
	}

	for _, c := range contents {
		// Gemini uses "model" for the assistant turn; normalize so scenario
		// matching and turn counting line up with the other adapters.
		role := c.Role
		if role == "model" {
			role = "assistant"
		}
		result = append(result, engine.RequestMessage{Role: role, Content: joinGeminiParts(c.Parts)})
	}
	return result
}

func joinGeminiParts(parts []GeminiPart) string {
	var out []string
	for _, p := range parts {
		if p.Text != "" {
			out = append(out, p.Text)
		}
		// Surface tool-result content so scenario matching sees tool-result
		// follow-up turns (mirrors the OpenAI/Anthropic tool_result handling).
		if p.FunctionResponse != nil {
			if p.FunctionResponse.Name != "" {
				out = append(out, p.FunctionResponse.Name)
			}
			if len(p.FunctionResponse.Response) > 0 {
				if b, err := json.Marshal(p.FunctionResponse.Response); err == nil {
					out = append(out, string(b))
				}
			}
		}
	}
	return strings.Join(out, " ")
}

func formatGeminiResponse(resp *engine.Response, model string, promptTokens, candidateTokens int) *GeminiResponse {
	var parts []GeminiPart
	if resp.Content != "" {
		parts = append(parts, GeminiPart{Text: resp.Content})
	}
	// Refusal surfaces as a text part (Gemini has no structured refusal field).
	if resp.Refusal != "" {
		parts = append(parts, GeminiPart{Text: resp.Refusal})
	}
	for _, tc := range resp.ToolCalls {
		parts = append(parts, GeminiPart{
			FunctionCall: &GeminiFunctionCall{Name: tc.Name, Args: tc.Arguments},
		})
	}

	// Scenario-forced finish reason (e.g. "length" -> MAX_TOKENS) wins (FB-03).
	finishReason := "STOP"
	if resp.FinishReason != "" {
		finishReason = streaming.GeminiFinishReason(resp.FinishReason)
	} else if resp.Refusal != "" {
		finishReason = "SAFETY"
	}

	return &GeminiResponse{
		Candidates: []GeminiCandidate{{
			Content:      GeminiContent{Role: "model", Parts: parts},
			FinishReason: finishReason,
			Index:        0,
		}},
		UsageMetadata: GeminiUsageMetadata{
			PromptTokenCount:     promptTokens,
			CandidatesTokenCount: candidateTokens,
			TotalTokenCount:      promptTokens + candidateTokens,
		},
		ModelVersion: model,
	}
}

// geminiError is Gemini's error envelope ({"error":{"code","message","status"}}).
type geminiError struct {
	Error geminiErrorBody `json:"error"`
}

type geminiErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func writeGeminiError(w http.ResponseWriter, code int, status, message string) {
	writeJSON(w, code, geminiError{Error: geminiErrorBody{Code: code, Message: message, Status: status}})
}

// geminiStatusFor maps an HTTP status to Gemini's canonical status string.
func geminiStatusFor(code int) string {
	switch code {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	case http.StatusGatewayTimeout:
		return "DEADLINE_EXCEEDED"
	default:
		return "INTERNAL"
	}
}

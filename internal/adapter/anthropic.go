package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"sync"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/types"
)

// --- Anthropic Request Types ---

// AnthropicRequest represents an Anthropic Messages API request.
type AnthropicRequest struct {
	Model    string             `json:"model"`
	Messages []AnthropicMessage `json:"messages"`
	// System is a string OR an array of content blocks. The Anthropic Messages
	// API accepts both forms, and real clients (the Anthropic SDK, the Claude
	// CLI / Agent SDK) send the array form, so it must be decoded as `any` and
	// flattened to text via extractAnthropicContent rather than typed as string.
	System    any             `json:"system,omitempty"`
	Tools     []AnthropicTool `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
	Stream    bool            `json:"stream,omitempty"`
	// Thinking is the extended-thinking gate; type "enabled" turns on a
	// synthesized thinking content block (A-04). budget_tokens is advisory.
	Thinking *AnthropicThinkingReq `json:"thinking,omitempty"`
}

// AnthropicThinkingReq is the extended-thinking gate on a request.
type AnthropicThinkingReq struct {
	Type         string `json:"type"` // "enabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"`
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
	// CacheControl, when present, marks the tool definition as a prompt-cache
	// breakpoint (A-04).
	CacheControl any `json:"cache_control,omitempty"`
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
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Thinking / Signature carry an extended-thinking block (type "thinking").
	Thinking  string         `json:"thinking,omitempty"`
	Signature string         `json:"signature,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
}

// AnthropicUsage represents token usage. The cache_* fields are pointers so
// they are absent entirely when a request carries no cache_control markers, but
// appear as a MATCHED PAIR (one side may be 0) whenever caching is active —
// matching the real API, where both are present together (A-04).
type AnthropicUsage struct {
	InputTokens              int  `json:"input_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens,omitempty"`
	OutputTokens             int  `json:"output_tokens"`
}

// --- Anthropic Handler ---

// AnthropicHandler handles Anthropic Messages API requests.
type AnthropicHandler struct {
	Engine *engine.Engine
	// cache tracks which prompt-cache breakpoints have been seen, so a first
	// request with a cache_control marker bills cache_creation and an identical
	// repeat bills cache_read (A-04). It is bounded (FIFO eviction) and scoped
	// per tenant. The zero value is usable, so DefaultRegistry's struct-literal
	// construction needs no constructor.
	cache anthropicCacheTracker
}

// anthropicCacheKey scopes a prompt-cache breakpoint by tenant so the
// first-vs-repeat signal never leaks across tenants (the real API caches
// per-organization).
type anthropicCacheKey struct {
	tenant string
	hash   uint64
}

// anthropicCacheTracker is a bounded, tenant-scoped "seen" set for prompt-cache
// breakpoints. It evicts oldest-first past maxAnthropicCacheEntries so a long-
// running mock can't grow without bound (eviction only flips a later repeat
// from cache_read back to cache_creation — a cosmetic mock detail).
type anthropicCacheTracker struct {
	mu    sync.Mutex
	seen  map[anthropicCacheKey]struct{}
	order []anthropicCacheKey
}

const maxAnthropicCacheEntries = 4096

// seenOrStore returns whether key was already present, recording it otherwise.
func (t *anthropicCacheTracker) seenOrStore(key anthropicCacheKey) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen == nil {
		t.seen = make(map[anthropicCacheKey]struct{})
	}
	if _, ok := t.seen[key]; ok {
		return true
	}
	t.seen[key] = struct{}{}
	t.order = append(t.order, key)
	for len(t.order) > maxAnthropicCacheEntries {
		oldest := t.order[0]
		t.order = t.order[1:]
		delete(t.seen, oldest)
	}
	return false
}

// Name identifies this adapter in logs and diagnostics.
func (h *AnthropicHandler) Name() string { return "anthropic" }

// Routes returns the Anthropic-compatible routes this adapter serves,
// mounted by the server through the adapter Registry (REF-05).
func (h *AnthropicHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/messages", Handler: h.HandleMessages},
		{Pattern: "POST /v1/messages/count_tokens", Handler: h.HandleCountTokens},
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
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAnthropicError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
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

	if !checkAnthropicAuth(w, r) {
		return
	}

	// Convert to engine request.
	convertedMsgs, imageCount := convertAnthropicMessages(req.Messages, req.System)
	inbound := &engine.InboundRequest{
		Model:     req.Model,
		SessionID: extractSessionID(r),
		Messages:  convertedMsgs,
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
			if ce.Connection != "" {
				if !connectionFault(w, ce.Connection) {
					writeAnthropicError(w, http.StatusBadGateway, "api_error", "connection fault could not be delivered")
				}
				return
			}
			if ra, ok := chaosRetryAfter(ce); ok {
				w.Header().Set("Retry-After", ra)
			}
			writeAnthropicError(w, ce.StatusCode, anthropicChaosErrorType(ce.StatusCode), ce.Message)
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

	setHallucinationHeader(w, resp)
	setImageCountHeader(w, imageCount)

	// Stream or JSON.
	if req.Stream {
		// TODO(A-04 streaming): the streaming path does not yet emit synthesized
		// thinking blocks or cache_creation/cache_read usage — those A-04
		// additions are non-streaming only for now.
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

	// Prompt caching (A-04): cache_control-marked tokens are billed as
	// cache_creation (first sight) or cache_read (repeat), scoped per tenant.
	// Only the message/system marked tokens are removed from input_tokens — tool
	// tokens were never part of the input_tokens base, so subtracting them would
	// under-report.
	markedTokens, markedFromMessages, contentHash := cacheMarkedTokens(&req)
	tenantID := engine.TenantIDFromContext(r.Context())
	cacheCreation, cacheRead := h.cacheUsageFields(markedTokens, anthropicCacheKey{tenant: tenantID, hash: contentHash})
	if inputTokens -= markedFromMessages; inputTokens < 0 {
		inputTokens = 0
	}

	// Extended thinking (A-04): when enabled (the thinking request param), a
	// synthesized thinking block leads the content and its tokens count toward
	// output.
	var thinkingText, thinkingSig string
	if thinkingEnabled(&req) {
		thinkingText, thinkingSig = synthesizeThinking(&req)
		outputTokens += EstimateTokens(thinkingText)
	}

	anthropicResp := formatAnthropicResponse(resp, inputTokens, outputTokens, cacheCreation, cacheRead, thinkingText, thinkingSig)
	writeJSON(w, http.StatusOK, anthropicResp)
}

// --- Conversion Helpers ---

// convertAnthropicMessages flattens the wire messages (and optional system) to
// engine messages and returns the total number of image parts across the user
// messages (A-05). Image-bearing messages get a "[image]" marker appended so
// image-only turns aren't empty and scenarios can match via content_contains.
func convertAnthropicMessages(msgs []AnthropicMessage, system any) ([]engine.RequestMessage, int) {
	// Pre-size for the worst case (every message + an optional system prepend) so
	// the append loop never grows the slice (PERF-15; the OpenAI twin already
	// pre-sizes).
	result := make([]engine.RequestMessage, 0, len(msgs)+1)

	// Prepend system message if provided. `system` may be a string or an array
	// of content blocks (both are valid per the Messages API); flatten either to
	// text the same way request message content is flattened.
	if sys := extractAnthropicContent(system); sys != "" {
		result = append(result, engine.RequestMessage{
			Role:    "system",
			Content: sys,
		})
	}

	totalImages := 0
	for _, m := range msgs {
		content, imgCount := extractAnthropicContentWithImages(m.Content)
		totalImages += imgCount
		result = append(result, engine.RequestMessage{
			Role:       m.Role,
			Content:    content,
			ImageCount: imgCount,
		})
	}
	return result, totalImages
}

// extractAnthropicContentWithImages flattens content to text (preserving the
// text + tool_result handling of extractAnthropicContent) and counts image
// blocks. The text is marker-free; the count is carried out-of-band.
func extractAnthropicContentWithImages(content any) (string, int) {
	blocks, ok := content.([]any)
	if !ok {
		return extractAnthropicContent(content), 0
	}
	var parts []string
	images := 0
	for _, block := range blocks {
		m, ok := block.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "text":
			if text, ok := m["text"].(string); ok {
				parts = append(parts, text)
			}
		case "image":
			images++
		case "tool_result":
			switch c := m["content"].(type) {
			case string:
				if c != "" {
					parts = append(parts, c)
				}
			case []any:
				if s := extractAnthropicContent(c); s != "" {
					parts = append(parts, s)
				}
			}
		}
	}
	return strings.Join(parts, " "), images
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
					// tool_result content is a string OR an array of content
					// blocks (both valid per the Messages API). Real clients (the
					// Anthropic SDK, the Claude CLI / Agent SDK) send the array
					// form, so recurse to flatten it rather than only handling the
					// string form — otherwise the tool-result turn flattens to ""
					// and the engine rejects it as an empty user message.
					switch c := m["content"].(type) {
					case string:
						if c != "" {
							parts = append(parts, c)
						}
					case []any:
						if s := extractAnthropicContent(c); s != "" {
							parts = append(parts, s)
						}
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

func formatAnthropicResponse(resp *engine.Response, inputTokens, outputTokens int, cacheCreation, cacheRead *int, thinkingText, thinkingSig string) *AnthropicResponse {
	var content []AnthropicContent

	// Extended-thinking block leads the content array (A-04).
	if thinkingText != "" {
		content = append(content, AnthropicContent{
			Type:      "thinking",
			Thinking:  thinkingText,
			Signature: thinkingSig,
		})
	}

	// Text content block.
	if resp.Content != "" {
		content = append(content, AnthropicContent{
			Type: "text",
			Text: resp.Content,
		})
	}

	// Refusal is surfaced as a text block (Anthropic has no structured refusal
	// field) with a refusal stop reason (FB-03).
	if resp.Refusal != "" {
		content = append(content, AnthropicContent{Type: "text", Text: resp.Refusal})
	}

	// Tool use blocks.
	stopReason := "end_turn"
	if resp.Refusal != "" {
		stopReason = "refusal"
	}
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

	// Scenario-forced stop reason (e.g. "length" -> max_tokens) wins (FB-03).
	if resp.FinishReason != "" {
		stopReason = streaming.AnthropicStopReason(resp.FinishReason)
	}

	return &AnthropicResponse{
		ID:         "msg_" + generateID(),
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		Model:      resp.Model,
		StopReason: stopReason,
		Usage: AnthropicUsage{
			InputTokens:              inputTokens,
			CacheCreationInputTokens: cacheCreation,
			CacheReadInputTokens:     cacheRead,
			OutputTokens:             outputTokens,
		},
	}
}

// HandleCountTokens handles POST /v1/messages/count_tokens. It is engine-free:
// it decodes the request and returns only an input_tokens count. The endpoint
// is free on the real API, so it is not quota-counted or logged.
func (h *AnthropicHandler) HandleCountTokens(w http.ResponseWriter, r *http.Request) {
	var req AnthropicRequest
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAnthropicError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
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
	if !checkAnthropicAuth(w, r) {
		return
	}

	converted, _ := convertAnthropicMessages(req.Messages, req.System)
	n := sumMessageTokens(converted)
	writeJSON(w, http.StatusOK, map[string]int{"input_tokens": n})
}

// checkAnthropicAuth enforces the inline API-key requirement (X-Api-Key or a
// Bearer token) shared by the Messages and count_tokens handlers. On a miss it
// writes the 401 and returns false.
func checkAnthropicAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Api-Key") == "" && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "missing API key")
		return false
	}
	return true
}

// thinkingEnabled reports whether extended thinking is on for this request. The
// gate is the thinking request param ({"type":"enabled"}) — the same gate the
// real API uses to emit thinking blocks. (A beta header alone, e.g.
// interleaved-thinking, does NOT by itself produce a standalone thinking block
// on the real API, so it is not a gate here.)
func thinkingEnabled(req *AnthropicRequest) bool {
	return req.Thinking != nil && req.Thinking.Type == "enabled"
}

// synthesizeThinking produces a deterministic thinking trace + signature seeded
// from the last user message. The mock does not actually reason; the trace is a
// stable placeholder so thinking-trace handling is testable offline.
func synthesizeThinking(req *AnthropicRequest) (thinking, signature string) {
	seed := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			seed = extractAnthropicContent(req.Messages[i].Content)
			break
		}
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	preview := seed
	if len(preview) > 60 {
		preview = preview[:60] + "..."
	}
	thinking = fmt.Sprintf("Let me reason about: %s [mock-hash:%016x]", preview, h.Sum64())
	// base64("mockagents-thinking-sig") — a stable opaque placeholder signature.
	signature = "bW9ja2FnZW50cy10aGlua2luZy1zaWc="
	return thinking, signature
}

// cacheMarkedTokens scans the request's system, messages, and tools for content
// blocks tagged with cache_control and returns: the total marked token count
// (billed as cache_creation/cache_read), the subset of those tokens that came
// from system/messages (the only part subtractable from input_tokens, since
// tool tokens are never in that base), and a stable content hash for
// first-vs-repeat detection. Returns (0,0,0) when nothing is marked.
//
// Detection keys on the marked-BLOCK content, not the full rendered prefix, so
// it is a deterministic mock simplification rather than the real API's strict
// prefix-match cache.
func cacheMarkedTokens(req *AnthropicRequest) (total, fromMessages int, contentHash uint64) {
	h := fnv.New64a()
	// addBlock counts a marked block's weight (its text, or — for non-text
	// blocks like image/document — its serialized size, so a document/image
	// breakpoint still registers) and folds it into the hash.
	addBlock := func(m map[string]any, fromMsg bool) {
		var n int
		inBase := false
		if text := extractAnthropicContent([]any{m}); text != "" {
			n = EstimateTokens(text)
			_, _ = h.Write([]byte(text))
			inBase = true // text flows into the flattened input_tokens base
		} else if b, err := json.Marshal(m); err == nil {
			// Non-text block (image/document): estimate from serialized size so
			// the breakpoint still registers; it is NOT in the input base.
			if n = len(b) / 4; n < 1 {
				n = 1
			}
			_, _ = h.Write(b)
		}
		if n == 0 {
			return
		}
		_, _ = h.Write([]byte{0})
		total += n
		if fromMsg && inBase {
			fromMessages += n
		}
	}
	walk := func(content any, fromMsg bool) {
		blocks, ok := content.([]any)
		if !ok {
			return
		}
		for _, b := range blocks {
			if m, ok := b.(map[string]any); ok && m["cache_control"] != nil {
				addBlock(m, fromMsg)
			}
		}
	}
	walk(req.System, true)
	for i := range req.Messages {
		walk(req.Messages[i].Content, true)
	}
	for i := range req.Tools {
		if req.Tools[i].CacheControl != nil {
			text := req.Tools[i].Name + " " + req.Tools[i].Description
			n := EstimateTokens(text)
			_, _ = h.Write([]byte(text))
			_, _ = h.Write([]byte{0})
			total += n // tools are NOT part of the input_tokens base
		}
	}
	if total == 0 {
		return 0, 0, 0
	}
	return total, fromMessages, h.Sum64()
}

// cacheUsageFields resolves the cache_creation / cache_read split as a matched
// pointer pair (both nil when nothing is marked; both non-nil — one possibly 0
// — when caching is active, matching the real wire shape). The first request
// carrying a given (tenant, content) bills creation; an identical repeat bills
// read.
func (h *AnthropicHandler) cacheUsageFields(markedTokens int, key anthropicCacheKey) (creation, read *int) {
	if markedTokens == 0 {
		return nil, nil
	}
	zero, n := 0, markedTokens
	if h.cache.seenOrStore(key) {
		return &zero, &n // cache_read
	}
	return &n, &zero // cache_creation
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

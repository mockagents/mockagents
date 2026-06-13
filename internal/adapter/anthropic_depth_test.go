package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doAnthropicRaw posts a raw JSON body with custom headers (for the count_tokens
// path and the anthropic-beta header cases).
func doAnthropicRaw(t *testing.T, handler http.HandlerFunc, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-key")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func anthropicHandler() *AnthropicHandler {
	return &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
}

// --- count_tokens ---

func TestCountTokens_Shape(t *testing.T) {
	h := anthropicHandler()
	rec := doAnthropicRaw(t, h.HandleCountTokens, "/v1/messages/count_tokens",
		`{"model":"claude-3-opus","messages":[{"role":"user","content":"hello world test"}]}`, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	assert.Len(t, m, 1, "count_tokens returns only input_tokens")
	var n int
	require.NoError(t, json.Unmarshal(m["input_tokens"], &n))
	assert.Greater(t, n, 0)
}

func TestCountTokens_Errors(t *testing.T) {
	h := anthropicHandler()
	// missing model
	rec := doAnthropicRaw(t, h.HandleCountTokens, "/v1/messages/count_tokens",
		`{"messages":[{"role":"user","content":"hi"}]}`, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	// missing messages
	rec = doAnthropicRaw(t, h.HandleCountTokens, "/v1/messages/count_tokens",
		`{"model":"claude-3-opus"}`, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCountTokens_MissingAPIKey(t *testing.T) {
	h := anthropicHandler()
	req := httptest.NewRequest("POST", "/v1/messages/count_tokens",
		strings.NewReader(`{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json") // deliberately NO api key
	rec := httptest.NewRecorder()
	h.HandleCountTokens(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCountTokens_SystemCounts(t *testing.T) {
	h := anthropicHandler()
	without := doAnthropicRaw(t, h.HandleCountTokens, "/v1/messages/count_tokens",
		`{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"}]}`, nil)
	with := doAnthropicRaw(t, h.HandleCountTokens, "/v1/messages/count_tokens",
		`{"model":"claude-3-opus","system":"You are a very helpful detailed assistant","messages":[{"role":"user","content":"hi"}]}`, nil)
	var a, b struct {
		InputTokens int `json:"input_tokens"`
	}
	require.NoError(t, json.Unmarshal(without.Body.Bytes(), &a))
	require.NoError(t, json.Unmarshal(with.Body.Bytes(), &b))
	assert.Greater(t, b.InputTokens, a.InputTokens, "system text must be counted")
}

// --- prompt caching usage ---

func cacheMarkedBody() string {
	return `{"model":"claude-3-opus","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"a long cached system context block here","cache_control":{"type":"ephemeral"}},` +
		`{"type":"text","text":"hello"}]}]}`
}

// usageMap returns the response's usage object as raw keys (so absence vs a
// present-zero can be distinguished).
func usageMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code)
	var outer struct {
		Usage map[string]json.RawMessage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &outer))
	return outer.Usage
}

func jint(t *testing.T, raw json.RawMessage) int {
	t.Helper()
	var n int
	require.NoError(t, json.Unmarshal(raw, &n))
	return n
}

func TestCacheUsage_CreationThenRead(t *testing.T) {
	h := anthropicHandler() // shared handler across the two requests
	body := cacheMarkedBody()

	// First request: matched pair present; creation>0, read==0.
	u1 := usageMap(t, doAnthropicRaw(t, h.HandleMessages, "/v1/messages", body, nil))
	require.Contains(t, u1, "cache_creation_input_tokens", "matched pair must always be present when caching")
	require.Contains(t, u1, "cache_read_input_tokens")
	assert.Greater(t, jint(t, u1["cache_creation_input_tokens"]), 0)
	assert.Equal(t, 0, jint(t, u1["cache_read_input_tokens"]))

	// Identical repeat: creation==0, read>0 — both still present.
	u2 := usageMap(t, doAnthropicRaw(t, h.HandleMessages, "/v1/messages", body, nil))
	require.Contains(t, u2, "cache_creation_input_tokens")
	require.Contains(t, u2, "cache_read_input_tokens")
	assert.Equal(t, 0, jint(t, u2["cache_creation_input_tokens"]))
	assert.Greater(t, jint(t, u2["cache_read_input_tokens"]), 0)
}

func TestCacheUsage_ToolMarked_InputTokensStaysSane(t *testing.T) {
	// A short message (1 input token) + a cache_control-marked TOOL. The tool's
	// tokens bill cache_creation but must NOT be subtracted from input_tokens
	// (they were never part of the input base).
	h := anthropicHandler()
	body := `{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"}],` +
		`"tools":[{"name":"search","description":"search the web for results","cache_control":{"type":"ephemeral"}}]}`
	u := usageMap(t, doAnthropicRaw(t, h.HandleMessages, "/v1/messages", body, nil))
	assert.Greater(t, jint(t, u["cache_creation_input_tokens"]), 0)
	assert.Greater(t, jint(t, u["input_tokens"]), 0, "tool-marked tokens must not zero out input_tokens")
}

func TestCacheUsage_NonTextBlockRegisters(t *testing.T) {
	// A non-text marked block (image) must still register cache tokens via the
	// serialized-size fallback, and must not zero out input_tokens.
	h := anthropicHandler()
	body := `{"model":"claude-3-opus","messages":[{"role":"user","content":[` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"},"cache_control":{"type":"ephemeral"}},` +
		`{"type":"text","text":"hello"}]}]}`
	u := usageMap(t, doAnthropicRaw(t, h.HandleMessages, "/v1/messages", body, nil))
	assert.Greater(t, jint(t, u["cache_creation_input_tokens"]), 0, "a non-text marked block must register")
	assert.GreaterOrEqual(t, jint(t, u["input_tokens"]), 0)
}

func TestCacheUsage_AbsentWithoutCacheControl(t *testing.T) {
	h := anthropicHandler()
	rec := doAnthropicRaw(t, h.HandleMessages, "/v1/messages",
		`{"model":"claude-3-opus","messages":[{"role":"user","content":"hello"}]}`, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// The cache_* keys must be omitted entirely when no cache_control is present.
	var outer struct {
		Usage map[string]json.RawMessage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &outer))
	assert.NotContains(t, outer.Usage, "cache_creation_input_tokens")
	assert.NotContains(t, outer.Usage, "cache_read_input_tokens")
}

// --- extended thinking ---

func TestThinking_BlockPresent_ViaParam(t *testing.T) {
	h := anthropicHandler()
	rec := doAnthropicRaw(t, h.HandleMessages, "/v1/messages",
		`{"model":"claude-3-opus","thinking":{"type":"enabled","budget_tokens":1000},"messages":[{"role":"user","content":"hello"}]}`, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp AnthropicResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.GreaterOrEqual(t, len(resp.Content), 2)
	assert.Equal(t, "thinking", resp.Content[0].Type)
	assert.NotEmpty(t, resp.Content[0].Thinking)
	assert.NotEmpty(t, resp.Content[0].Signature)
	assert.Equal(t, "text", resp.Content[1].Type)
	assert.Equal(t, "Bonjour!", resp.Content[1].Text)
}

func TestThinking_BetaHeaderAloneDoesNotEnable(t *testing.T) {
	// Faithful to the real API: a thinking beta header by itself does NOT emit a
	// standalone thinking block — only the thinking request param does.
	h := anthropicHandler()
	rec := doAnthropicRaw(t, h.HandleMessages, "/v1/messages",
		`{"model":"claude-3-opus","messages":[{"role":"user","content":"hello"}]}`,
		map[string]string{"anthropic-beta": "interleaved-thinking-2025-05-14"})
	require.Equal(t, http.StatusOK, rec.Code)
	var resp AnthropicResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "text", resp.Content[0].Type, "beta header alone must not emit a thinking block")
}

func TestThinking_AbsentWhenNotEnabled(t *testing.T) {
	h := anthropicHandler()
	rec := doAnthropicRaw(t, h.HandleMessages, "/v1/messages",
		`{"model":"claude-3-opus","messages":[{"role":"user","content":"hello"}]}`, nil)
	var resp AnthropicResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "text", resp.Content[0].Type, "no thinking block when not enabled")
}

func TestThinking_OutputTokensIncludeThinking_AndDeterministic(t *testing.T) {
	h := anthropicHandler()
	plain := doAnthropicRaw(t, h.HandleMessages, "/v1/messages",
		`{"model":"claude-3-opus","messages":[{"role":"user","content":"hello"}]}`, nil)
	think1 := doAnthropicRaw(t, h.HandleMessages, "/v1/messages",
		`{"model":"claude-3-opus","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hello"}]}`, nil)
	think2 := doAnthropicRaw(t, h.HandleMessages, "/v1/messages",
		`{"model":"claude-3-opus","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hello"}]}`, nil)

	var p, t1, t2 AnthropicResponse
	require.NoError(t, json.Unmarshal(plain.Body.Bytes(), &p))
	require.NoError(t, json.Unmarshal(think1.Body.Bytes(), &t1))
	require.NoError(t, json.Unmarshal(think2.Body.Bytes(), &t2))

	assert.Greater(t, t1.Usage.OutputTokens, p.Usage.OutputTokens, "thinking tokens count toward output")
	assert.Equal(t, t1.Content[0].Thinking, t2.Content[0].Thinking, "thinking synthesis is deterministic")
}

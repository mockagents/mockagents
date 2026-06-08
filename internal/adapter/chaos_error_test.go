package adapter

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPreset_RateLimited_EndToEnd closes the load-time→wire boundary (FB03-TG-02):
// a chaos PRESET expanded by config.ApplyDefaults must actually produce the
// expected wire status + envelope through an adapter, not just expand in config.
func TestPreset_RateLimited_EndToEnd(t *testing.T) {
	def := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "p-rl"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions", Model: "p-rl",
			Behavior: types.BehaviorConfig{
				Chaos:     &types.ChaosConfig{Preset: "rate-limited"},
				Scenarios: []types.Scenario{{Name: "default", Response: types.ScenarioResponse{Content: "ok"}}},
			},
		},
	}
	config.ApplyDefaults(def) // expands the preset into errors{rate:1,status:429}

	h := &OpenAIHandler{Engine: testEngine(def)}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "p-rl", Messages: []OpenAIMessage{{Role: "user", Content: "hi"}},
	})
	require.Equal(t, http.StatusTooManyRequests, rec.Code, "preset rate-limited should 429 at the wire")
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	var body openAIError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "rate_limit_exceeded", body.Error.Code)
}

// TestPreset_Flaky_EndToEnd verifies the flaky preset (fail_first 2) recovers
// on the 3rd request through the adapter.
func TestPreset_Flaky_EndToEnd(t *testing.T) {
	def := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "p-flaky"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions", Model: "p-flaky",
			Behavior: types.BehaviorConfig{
				Chaos:     &types.ChaosConfig{Preset: "flaky"},
				Scenarios: []types.Scenario{{Name: "default", Response: types.ScenarioResponse{Content: "recovered"}}},
			},
		},
	}
	config.ApplyDefaults(def)
	h := &OpenAIHandler{Engine: testEngine(def)}
	call := func() int {
		return doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
			Model: "p-flaky", Messages: []OpenAIMessage{{Role: "user", Content: "hi"}},
		}).Code
	}
	assert.Equal(t, http.StatusServiceUnavailable, call(), "call 1")
	assert.Equal(t, http.StatusServiceUnavailable, call(), "call 2")
	assert.Equal(t, http.StatusOK, call(), "call 3 recovers")
}

// chaosAgent builds an agent whose FIRST request deterministically fails with
// the given HTTP status (via the fail_first stateful trigger), so adapter tests
// can assert the wire-accurate error envelope per provider.
func chaosAgent(name, model string, status int) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: name},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Model:    model,
			Behavior: types.BehaviorConfig{
				Chaos: &types.ChaosConfig{
					Errors: &types.ChaosErrorConfig{FailFirst: 1, StatusCode: status, Message: "injected"},
				},
				Scenarios: []types.Scenario{
					{Name: "default", Response: types.ScenarioResponse{Content: "ok"}},
				},
			},
		},
	}
}

func TestOpenAI_ChaosErrorBodies(t *testing.T) {
	cases := []struct {
		status    int
		wantType  string
		wantCode  string
		wantRetry bool
	}{
		{http.StatusUnauthorized, "invalid_request_error", "invalid_api_key", false},
		{http.StatusForbidden, "invalid_request_error", "", false},
		{http.StatusTooManyRequests, "requests", "rate_limit_exceeded", true},
		{http.StatusInternalServerError, "server_error", "", false},
	}
	for _, tc := range cases {
		h := &OpenAIHandler{Engine: testEngine(chaosAgent("oa", "oa-model", tc.status))}
		rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
			Model:    "oa-model",
			Messages: []OpenAIMessage{{Role: "user", Content: "hi"}},
		})
		require.Equal(t, tc.status, rec.Code, "status %d", tc.status)
		var body openAIError
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
		assert.Equal(t, tc.wantType, body.Error.Type, "type for %d", tc.status)
		assert.Equal(t, tc.wantCode, body.Error.Code, "code for %d", tc.status)
		if tc.wantRetry {
			assert.NotEmpty(t, rec.Header().Get("Retry-After"), "429 must carry Retry-After")
		}
	}
}

func TestAnthropic_ChaosErrorBodies(t *testing.T) {
	cases := []struct {
		status   int
		wantType string
	}{
		{http.StatusUnauthorized, "authentication_error"},
		{http.StatusForbidden, "permission_error"},
		{http.StatusTooManyRequests, "rate_limit_error"},
		{http.StatusServiceUnavailable, "overloaded_error"},
	}
	for _, tc := range cases {
		h := &AnthropicHandler{Engine: testEngine(chaosAgent("an", "an-model", tc.status))}
		rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
			Model:     "an-model",
			Messages:  []AnthropicMessage{{Role: "user", Content: "hi"}},
			MaxTokens: 16,
		})
		require.Equal(t, tc.status, rec.Code, "status %d", tc.status)
		var env anthropicErrorEnvelope
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
		assert.Equal(t, "error", env.Type)
		assert.Equal(t, tc.wantType, env.Error.Type, "type for %d", tc.status)
	}
	// 429 carries Retry-After.
	h := &AnthropicHandler{Engine: testEngine(chaosAgent("an2", "an2-model", http.StatusTooManyRequests))}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model: "an2-model", Messages: []AnthropicMessage{{Role: "user", Content: "hi"}}, MaxTokens: 16,
	})
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestGemini_ChaosErrorStatuses(t *testing.T) {
	cases := []struct {
		status     int
		wantStatus string
	}{
		{http.StatusUnauthorized, "UNAUTHENTICATED"},
		{http.StatusForbidden, "PERMISSION_DENIED"},
		{http.StatusTooManyRequests, "RESOURCE_EXHAUSTED"},
		{http.StatusServiceUnavailable, "UNAVAILABLE"},
	}
	for _, tc := range cases {
		h := &GeminiHandler{Engine: testEngine(chaosAgent("gm", "gm-model", tc.status))}
		rec := doGeminiRequest(t, h, "gm-model", "generateContent", GeminiRequest{
			Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "hi"}}}},
		})
		require.Equal(t, tc.status, rec.Code, "status %d", tc.status)
		var body struct {
			Error struct {
				Status string `json:"status"`
			} `json:"error"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
		assert.Equal(t, tc.wantStatus, body.Error.Status, "status for %d", tc.status)
	}
}

func TestChaosRetryAfter(t *testing.T) {
	cases := []struct {
		name    string
		ce      engine.ChaosError
		wantVal string
		wantOK  bool
	}{
		{"bare 429 default", engine.ChaosError{StatusCode: http.StatusTooManyRequests}, "1", true},
		{"503 no hint", engine.ChaosError{StatusCode: http.StatusServiceUnavailable}, "", false},
		// math.Ceil: a sub-second remainder rounds UP to 1; 1500ms -> 2 (the
		// case that distinguishes Ceil from truncation/floor).
		{"250ms ceil to 1", engine.ChaosError{StatusCode: 429, RetryAfter: 250 * time.Millisecond}, "1", true},
		{"1500ms ceil to 2", engine.ChaosError{StatusCode: 429, RetryAfter: 1500 * time.Millisecond}, "2", true},
		{"2s exact", engine.ChaosError{StatusCode: 429, RetryAfter: 2 * time.Second}, "2", true},
		// A timeout (504) must NOT emit Retry-After even though it carries a
		// RetryAfter duration (FB03-S2-1).
		{"timeout no header", engine.ChaosError{StatusCode: http.StatusGatewayTimeout, RetryAfter: 30 * time.Second, Timeout: true}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := chaosRetryAfter(&tc.ce)
			if v != tc.wantVal || ok != tc.wantOK {
				t.Errorf("chaosRetryAfter = (%q,%v), want (%q,%v)", v, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

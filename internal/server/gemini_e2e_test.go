package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Full-mux Gemini conformance (previously only handler-level) ---

func TestConformance_Gemini_ResponseFormat(t *testing.T) {
	agent := testFullAgent("gemini-agent", "gemini-1.5-pro")
	agent.Spec.Protocol = "google-gemini"
	_, addr := setupTestServer(t, agent)

	body := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	req, _ := http.NewRequest("POST", addr+"/v1beta/models/gemini-1.5-pro:generateContent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	cands := result["candidates"].([]any)
	require.GreaterOrEqual(t, len(cands), 1)
	cand := cands[0].(map[string]any)
	assert.Equal(t, "STOP", cand["finishReason"])
	parts := cand["content"].(map[string]any)["parts"].([]any)
	require.GreaterOrEqual(t, len(parts), 1)
	assert.Equal(t, "Hi there!", parts[0].(map[string]any)["text"])
	assert.NotNil(t, result["usageMetadata"])
}

func TestConformance_Gemini_Streaming(t *testing.T) {
	agent := testFullAgent("gemini-agent", "gemini-1.5-pro")
	agent.Spec.Protocol = "google-gemini"
	agent.Spec.Behavior.Streaming = &types.StreamingConfig{Enabled: true, ChunkSize: 2}
	_, addr := setupTestServer(t, agent)

	body := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	req, _ := http.NewRequest("POST", addr+"/v1beta/models/gemini-1.5-pro:streamGenerateContent?alt=sse", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	raw, _ := io.ReadAll(resp.Body)
	out := string(raw)
	assert.Contains(t, out, "data:")
	assert.Contains(t, out, `"finishReason":"STOP"`)
	assert.Contains(t, out, "usageMetadata")
	assert.NotContains(t, out, "[DONE]")
}

// --- RR-13 drop-in recipe accuracy guard ---
// Anchors the base-URL paths the drop-in-recipes guide tells users to use to
// executable routes, so a route move fails CI instead of the doc silently rotting.

func TestE2E_DropInRecipeRoutes(t *testing.T) {
	openai := testFullAgent("oa", "gpt-4o")
	anthropic := testFullAgent("an", "claude-3")
	anthropic.Spec.Protocol = "anthropic-messages"
	gemini := testFullAgent("ge", "gemini-1.5-pro")
	gemini.Spec.Protocol = "google-gemini"
	_, addr := setupTestServer(t, openai, anthropic, gemini)

	cases := []struct {
		name, path, body string
		headers          map[string]string
	}{
		{
			name: "openai /v1/chat/completions",
			path: "/v1/chat/completions",
			body: `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name:    "anthropic /v1/messages",
			path:    "/v1/messages",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`,
			headers: map[string]string{"X-Api-Key": "mock"},
		},
		{
			name: "gemini /v1beta/models/{model}:generateContent",
			path: "/v1beta/models/gemini-1.5-pro:generateContent",
			body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", addr+c.path, strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode, "documented base-URL route must return 200")
		})
	}
}

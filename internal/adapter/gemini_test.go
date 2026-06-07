package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testGeminiAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "gemini-test-agent"},
		Spec: types.AgentSpec{
			Protocol: "google-gemini",
			Model:    "gemini-1.5-pro",
			Tools: []types.ToolDefinition{
				{
					Name: "get_weather",
					Responses: []types.ToolResponseRule{
						{IsDefault: true, Response: map[string]any{"tempC": 22}},
					},
				},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:     "greeting",
						Match:    &types.MatchRule{ContentContains: "hello"},
						Response: types.ScenarioResponse{Content: "Hi there!"},
					},
					{
						Name:  "weather",
						Match: &types.MatchRule{ContentContains: "weather"},
						Response: types.ScenarioResponse{
							Content: "Checking weather.",
							ToolCalls: []types.ToolCallSpec{
								{Name: "get_weather", Arguments: map[string]any{"city": "London"}},
							},
						},
					},
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "How can I help?"},
					},
				},
				Streaming: &types.StreamingConfig{Enabled: true, ChunkSize: 2, ChunkDelayMs: 0},
			},
		},
	}
}

// doGeminiRequest invokes the handler with the model+method encoded in the
// path value, as the server's ServeMux wildcard route would populate it.
func doGeminiRequest(t *testing.T, h *GeminiHandler, model, method string, req GeminiRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	target := "/v1beta/models/" + model + ":" + method
	if method == "streamGenerateContent" {
		target += "?alt=sse" // SSE wire mode
	}
	httpReq := httptest.NewRequest("POST", target, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.SetPathValue("modelmethod", model+":"+method)
	rec := httptest.NewRecorder()
	h.HandleGenerate(rec, httpReq)
	return rec
}

func TestGemini_BasicGeneration(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(testGeminiAgent())}
	rec := doGeminiRequest(t, h, "gemini-1.5-pro", "generateContent", GeminiRequest{
		Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "hello"}}}},
	})

	require.Equal(t, http.StatusOK, rec.Code)

	var resp GeminiResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Candidates, 1)
	assert.Equal(t, "model", resp.Candidates[0].Content.Role)
	assert.Equal(t, "STOP", resp.Candidates[0].FinishReason)
	require.Len(t, resp.Candidates[0].Content.Parts, 1)
	assert.Equal(t, "Hi there!", resp.Candidates[0].Content.Parts[0].Text)
	assert.Greater(t, resp.UsageMetadata.TotalTokenCount, 0)
}

func TestGemini_SystemInstructionAndToolCall(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(testGeminiAgent())}
	rec := doGeminiRequest(t, h, "gemini-1.5-pro", "generateContent", GeminiRequest{
		SystemInstruction: &GeminiContent{Parts: []GeminiPart{{Text: "Be terse."}}},
		Contents:          []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "what's the weather"}}}},
	})

	require.Equal(t, http.StatusOK, rec.Code)

	var resp GeminiResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Candidates, 1)

	var fc *GeminiFunctionCall
	for _, p := range resp.Candidates[0].Content.Parts {
		if p.FunctionCall != nil {
			fc = p.FunctionCall
		}
	}
	require.NotNil(t, fc, "expected a functionCall part")
	assert.Equal(t, "get_weather", fc.Name)
	assert.Equal(t, "London", fc.Args["city"])
}

func TestGemini_Streaming(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(testGeminiAgent())}
	rec := doGeminiRequest(t, h, "gemini-1.5-pro", "streamGenerateContent", GeminiRequest{
		Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "hello"}}}},
	})

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "data:")
	assert.Contains(t, body, "\"finishReason\":\"STOP\"")
	assert.Contains(t, body, "usageMetadata") // streamed usage on the final event
	assert.NotContains(t, body, "[DONE]")     // Gemini has no DONE sentinel
}

// Without ?alt=sse, streamGenerateContent returns a JSON array of
// GenerateContentResponse objects (not SSE).
func TestGemini_StreamGenerateContent_NonSSE(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(testGeminiAgent())}
	body, _ := json.Marshal(GeminiRequest{
		Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "hello"}}}},
	})
	// No ?alt=sse on the target.
	httpReq := httptest.NewRequest("POST", "/v1beta/models/gemini-1.5-pro:streamGenerateContent", bytes.NewReader(body))
	httpReq.SetPathValue("modelmethod", "gemini-1.5-pro:streamGenerateContent")
	rec := httptest.NewRecorder()
	h.HandleGenerate(rec, httpReq)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "data:") // not SSE
	var arr []GeminiResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &arr), "non-SSE mode must return a JSON array")
	require.Len(t, arr, 1)
	assert.Equal(t, "Hi there!", arr[0].Candidates[0].Content.Parts[0].Text)
}

// A functionResponse-only follow-up turn contributes matchable content so
// scenario matching still routes correctly.
func TestGemini_FunctionResponsePartMatches(t *testing.T) {
	contents := []GeminiContent{
		{Role: "user", Parts: []GeminiPart{{Text: "what's the weather"}}},
		{Role: "model", Parts: []GeminiPart{{FunctionCall: &GeminiFunctionCall{Name: "get_weather"}}}},
		{Role: "user", Parts: []GeminiPart{{FunctionResponse: &GeminiFunctionResponse{
			Name: "get_weather", Response: map[string]any{"weather": "sunny"},
		}}}},
	}
	got := joinGeminiParts(contents[2].Parts)
	assert.Contains(t, got, "get_weather")
	assert.Contains(t, got, "sunny")
}

func TestGemini_EmptyContents(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(testGeminiAgent())}
	rec := doGeminiRequest(t, h, "gemini-1.5-pro", "generateContent", GeminiRequest{Contents: nil})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "INVALID_ARGUMENT")
}

func TestGemini_ModelNotFound(t *testing.T) {
	// Two agents -> no single-agent fallback, so an unknown model is a 404.
	agent2 := testGeminiAgent()
	agent2.Metadata.Name = "gemini-test-agent-2"
	agent2.Spec.Model = "gemini-flash"
	h := &GeminiHandler{Engine: testEngine(testGeminiAgent(), agent2)}
	rec := doGeminiRequest(t, h, "no-such-model", "generateContent", GeminiRequest{
		Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "hi"}}}},
	})
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "NOT_FOUND")
}

func TestGemini_MalformedJSONBody(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(testGeminiAgent())}
	httpReq := httptest.NewRequest("POST", "/v1beta/models/gemini-1.5-pro:generateContent",
		bytes.NewReader([]byte(`{"contents": [ BROKEN`)))
	httpReq.SetPathValue("modelmethod", "gemini-1.5-pro:generateContent")
	rec := httptest.NewRecorder()
	h.HandleGenerate(rec, httpReq)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "INVALID_ARGUMENT")
}

func TestGemini_BadPath(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(testGeminiAgent())}
	// No "{model}:{method}" colon → 400 INVALID_ARGUMENT.
	body, _ := json.Marshal(GeminiRequest{Contents: []GeminiContent{{Parts: []GeminiPart{{Text: "hi"}}}}})
	httpReq := httptest.NewRequest("POST", "/v1beta/models/gemini-1.5-pro", bytes.NewReader(body))
	httpReq.SetPathValue("modelmethod", "gemini-1.5-pro") // no colon
	rec := httptest.NewRecorder()
	h.HandleGenerate(rec, httpReq)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "INVALID_ARGUMENT"))
}

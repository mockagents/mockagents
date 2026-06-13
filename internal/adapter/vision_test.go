package adapter

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrTrue() *bool { b := true; return &b }

func visionScenarios() types.BehaviorConfig {
	return types.BehaviorConfig{
		Scenarios: []types.Scenario{
			{Name: "image-present", Match: &types.MatchRule{HasImage: ptrTrue()},
				Response: types.ScenarioResponse{Content: "I see an image."}},
			{Name: "greeting", Match: &types.MatchRule{ContentContains: "hello"},
				Response: types.ScenarioResponse{Content: "Hi there!"}},
			{Name: "default", Response: types.ScenarioResponse{Content: "No image."}},
		},
		Streaming: &types.StreamingConfig{Enabled: true, ChunkSize: 2, ChunkDelayMs: 0},
	}
}

func visionOpenAIAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "vision-openai"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions", Model: "gpt-4o-vision",
			Behavior: visionScenarios(),
		},
	}
}

func visionAnthropicAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "vision-anthropic"},
		Spec: types.AgentSpec{
			Protocol: "anthropic-messages", Model: "claude-vision",
			Behavior: visionScenarios(),
		},
	}
}

func imageURLPart(url string) map[string]any {
	return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}
}

func anthropicImagePart() map[string]any {
	return map[string]any{"type": "image", "source": map[string]any{
		"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="}}
}

// --- unit: flatteners ---

func TestExtractStringContentWithImages(t *testing.T) {
	cases := []struct {
		name      string
		content   any
		wantText  string
		wantCount int
	}{
		{"text only", "hello", "hello", 0},
		{"text part only", []any{map[string]any{"type": "text", "text": "hi"}}, "hi", 0},
		{"image only (text stays empty, count out-of-band)", []any{imageURLPart("https://x/a.png")}, "", 1},
		{"data-url image", []any{imageURLPart("data:image/png;base64,iVBOR")}, "", 1},
		{"text + 2 images (text marker-free)", []any{
			map[string]any{"type": "text", "text": "look"},
			imageURLPart("https://x/a.png"), imageURLPart("data:image/png;base64,z"),
		}, "look", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, n := extractStringContentWithImages(tc.content)
			assert.Equal(t, tc.wantText, text)
			assert.Equal(t, tc.wantCount, n)
		})
	}
}

func TestExtractAnthropicContentWithImages(t *testing.T) {
	// image only — text empty, count out-of-band
	text, n := extractAnthropicContentWithImages([]any{anthropicImagePart()})
	assert.Equal(t, "", text)
	assert.Equal(t, 1, n)

	// text + image — text marker-free
	text, n = extractAnthropicContentWithImages([]any{
		map[string]any{"type": "text", "text": "describe"}, anthropicImagePart()})
	assert.Equal(t, "describe", text)
	assert.Equal(t, 1, n)

	// tool_result still flattened, image still counted
	text, n = extractAnthropicContentWithImages([]any{
		map[string]any{"type": "tool_result", "content": "result text"}, anthropicImagePart()})
	assert.Equal(t, "result text", text)
	assert.Equal(t, 1, n)
}

// --- OpenAI e2e ---

func TestOpenAI_Vision_ImageURLAndDataURL(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(visionOpenAIAgent())}
	for _, url := range []string{"https://example.com/a.png", "data:image/png;base64,iVBORw0KGgo="} {
		rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
			Model:    "gpt-4o-vision",
			Messages: []OpenAIMessage{{Role: "user", Content: []any{imageURLPart(url)}}},
		})
		require.Equal(t, http.StatusOK, rec.Code, url)
		assert.Equal(t, "1", rec.Header().Get("X-Mockagents-Image-Count"), url)
		var resp ChatCompletionResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "I see an image.", *resp.Choices[0].Message.Content, url)
	}
}

func TestOpenAI_Vision_ImageOnly_NoEmptyMessageError(t *testing.T) {
	// An image-only message used to flatten to "" and 400 as an empty message.
	h := &OpenAIHandler{Engine: testEngine(visionOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o-vision",
		Messages: []OpenAIMessage{{Role: "user", Content: []any{imageURLPart("https://x/a.png")}}},
	})
	assert.Equal(t, http.StatusOK, rec.Code, "image-only message must not be rejected as empty")
}

func TestOpenAI_Vision_MultiImageCount(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(visionOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o-vision",
		Messages: []OpenAIMessage{{Role: "user", Content: []any{
			imageURLPart("https://x/a.png"), imageURLPart("https://x/b.png")}}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "2", rec.Header().Get("X-Mockagents-Image-Count"))
}

func TestOpenAI_Vision_TextAndImage_TextStillMatches(t *testing.T) {
	// Use the standard agent (greeting first, no [image] scenario): the appended
	// marker is a SUFFIX, so a content_contains "hello" rule still matches.
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "hello"}, imageURLPart("https://x/a.png")}}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("X-Mockagents-Image-Count"))
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Hi there!", *resp.Choices[0].Message.Content, "text match unaffected by appended marker")
}

func TestOpenAI_Vision_NoImage_HeaderAbsent(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(visionOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o-vision",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("X-Mockagents-Image-Count"), "text-only must not set the image header")
}

func TestOpenAI_Vision_StreamingHeaderSet(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(visionOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o-vision",
		Stream:   true,
		Messages: []OpenAIMessage{{Role: "user", Content: []any{imageURLPart("https://x/a.png")}}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("X-Mockagents-Image-Count"))
	assert.Contains(t, rec.Body.String(), "[DONE]")
}

// --- Anthropic e2e ---

func TestAnthropic_Vision_Base64AndURL(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(visionAnthropicAgent())}
	parts := []map[string]any{
		anthropicImagePart(),
		{"type": "image", "source": map[string]any{"type": "url", "url": "https://x/a.png"}},
	}
	for _, p := range parts {
		rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
			Model:    "claude-vision",
			Messages: []AnthropicMessage{{Role: "user", Content: []any{p}}},
		})
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "1", rec.Header().Get("X-Mockagents-Image-Count"))
		var resp AnthropicResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "I see an image.", resp.Content[0].Text)
	}
}

func TestAnthropic_Vision_ImageOnly_NoEmptyMessageError(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(visionAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:    "claude-vision",
		Messages: []AnthropicMessage{{Role: "user", Content: []any{anthropicImagePart()}}},
	})
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAnthropic_Vision_TextAndImage_TextStillMatches(t *testing.T) {
	// Standard agent (greeting "hello" -> "Bonjour!", no [image] scenario): the
	// suffix marker doesn't disturb the text match.
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model: "claude-3-opus",
		Messages: []AnthropicMessage{{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "hello"}, anthropicImagePart()}}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("X-Mockagents-Image-Count"))
	var resp AnthropicResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Bonjour!", resp.Content[0].Text)
}

// TestOpenAI_Vision_AnchoredRegexUnaffectedByImage pins the out-of-band fix:
// an anchored content_regex still matches the SAME text when an image is
// attached (the marker is no longer injected into the matched text).
func TestOpenAI_Vision_AnchoredRegexUnaffectedByImage(t *testing.T) {
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "rx"},
		Spec: types.AgentSpec{Protocol: "openai-chat-completions", Model: "gpt-4o-rx",
			Behavior: types.BehaviorConfig{Scenarios: []types.Scenario{
				{Name: "exact", Match: &types.MatchRule{ContentRegex: "^hello$"},
					Response: types.ScenarioResponse{Content: "exact match"}},
				{Name: "default", Response: types.ScenarioResponse{Content: "no match"}},
			}}}}
	h := &OpenAIHandler{Engine: testEngine(agent)}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o-rx",
		Messages: []OpenAIMessage{{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "hello"}, imageURLPart("https://x/a.png")}}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "exact match", *resp.Choices[0].Message.Content, "anchored regex must still match with an image attached")
}

// TestOpenAI_Vision_TemplateNoMarkerLeak pins that a {{ .Message }} echo response
// does not leak any vision marker into the output.
func TestOpenAI_Vision_TemplateNoMarkerLeak(t *testing.T) {
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "echo"},
		Spec: types.AgentSpec{Protocol: "openai-chat-completions", Model: "gpt-4o-echo",
			Behavior: types.BehaviorConfig{Scenarios: []types.Scenario{
				{Name: "echo", Match: &types.MatchRule{ContentContains: "say"},
					Response: types.ScenarioResponse{Content: "You said: {{ .Message }}"}},
			}}}}
	h := &OpenAIHandler{Engine: testEngine(agent)}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o-echo",
		Messages: []OpenAIMessage{{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "say something"}, imageURLPart("https://x/a.png")}}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	out := *resp.Choices[0].Message.Content
	assert.Equal(t, "You said: say something", out)
	assert.NotContains(t, out, "[image]", "no vision marker may leak into a templated response")
}

func TestAnthropic_Vision_NoImage_HeaderAbsent(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(visionAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:    "claude-vision",
		Messages: []AnthropicMessage{{Role: "user", Content: "hello"}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("X-Mockagents-Image-Count"))
}

package adapter

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAnthropic_UsageIdenticalWithAndWithoutStreaming is the audit M-18
// guard at the wire: the same request must report the same input and output
// token counts whether or not stream:true is set, because cost annotation
// and the spend quota are computed from these numbers.
func TestAnthropic_UsageIdenticalWithAndWithoutStreaming(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	req := AnthropicRequest{
		Model:    "claude-3-opus",
		Messages: []AnthropicMessage{{Role: "user", Content: "hello there, how are you doing today"}},
	}

	plain := doAnthropicRequest(t, h.HandleMessages, req)
	require.Equal(t, http.StatusOK, plain.Code)
	var nonStream struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(plain.Body.Bytes(), &nonStream))
	require.Greater(t, nonStream.Usage.OutputTokens, 0)

	req.Stream = true
	streamed := doAnthropicRequest(t, h.HandleMessages, req)
	require.Equal(t, http.StatusOK, streamed.Code)
	var streamIn, streamOut int
	for _, block := range strings.Split(streamed.Body.String(), "\n\n") {
		var event, data string
		for _, line := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				event = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				data = v
			}
		}
		switch event {
		case "message_start":
			var m struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			require.NoError(t, json.Unmarshal([]byte(data), &m))
			streamIn = m.Message.Usage.InputTokens
		case "message_delta":
			var m struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			require.NoError(t, json.Unmarshal([]byte(data), &m))
			streamOut = m.Usage.OutputTokens
		}
	}
	require.Equal(t, nonStream.Usage.InputTokens, streamIn, "input_tokens differ by transport")
	require.Equal(t, nonStream.Usage.OutputTokens, streamOut, "output_tokens differ by transport")
}

func TestAnthropic_ThinkingAndCacheUsageIdenticalWithStreaming(t *testing.T) {
	req := AnthropicRequest{
		Model:    "claude-3-opus",
		Thinking: &AnthropicThinkingReq{Type: "enabled", BudgetTokens: 1024},
		Messages: []AnthropicMessage{{
			Role: "user",
			Content: []any{map[string]any{
				"type":          "text",
				"text":          "hello there, how are you doing today",
				"cache_control": map[string]any{"type": "ephemeral"},
			}},
		}},
	}

	plainHandler := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	plain := doAnthropicRequest(t, plainHandler.HandleMessages, req)
	require.Equal(t, http.StatusOK, plain.Code)
	var nonStream AnthropicResponse
	require.NoError(t, json.Unmarshal(plain.Body.Bytes(), &nonStream))
	require.Len(t, nonStream.Content, 2)
	require.Equal(t, "thinking", nonStream.Content[0].Type)
	require.NotEmpty(t, nonStream.Content[0].Thinking)
	require.NotEmpty(t, nonStream.Content[0].Signature)
	require.NotNil(t, nonStream.Usage.CacheCreationInputTokens)
	require.NotNil(t, nonStream.Usage.CacheReadInputTokens)

	streamHandler := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	req.Stream = true
	streamed := doAnthropicRequest(t, streamHandler.HandleMessages, req)
	require.Equal(t, http.StatusOK, streamed.Code)

	var blockTypes []string
	var blockIndices []int
	var thinkingLifecycle []string
	var thinking, signature string
	var streamInput, streamOutput int
	var streamCacheCreation, streamCacheRead int
	for _, block := range strings.Split(streamed.Body.String(), "\n\n") {
		var event, data string
		for _, line := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				event = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				data = v
			}
		}
		if data == "" {
			continue
		}
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(data), &payload))
		switch event {
		case "message_start":
			usage := payload["message"].(map[string]any)["usage"].(map[string]any)
			streamInput = int(usage["input_tokens"].(float64))
		case "content_block_start":
			contentBlock := payload["content_block"].(map[string]any)
			blockTypes = append(blockTypes, contentBlock["type"].(string))
			blockIndices = append(blockIndices, int(payload["index"].(float64)))
			if payload["index"] == float64(0) {
				thinkingLifecycle = append(thinkingLifecycle, "start:"+contentBlock["type"].(string))
			}
		case "content_block_delta":
			delta := payload["delta"].(map[string]any)
			switch delta["type"] {
			case "thinking_delta":
				thinking += delta["thinking"].(string)
				thinkingLifecycle = append(thinkingLifecycle, "thinking_delta")
			case "signature_delta":
				signature += delta["signature"].(string)
				thinkingLifecycle = append(thinkingLifecycle, "signature_delta")
			}
		case "content_block_stop":
			if payload["index"] == float64(0) {
				thinkingLifecycle = append(thinkingLifecycle, "stop")
			}
		case "message_delta":
			usage := payload["usage"].(map[string]any)
			streamOutput = int(usage["output_tokens"].(float64))
			require.Contains(t, usage, "cache_creation_input_tokens")
			require.Contains(t, usage, "cache_read_input_tokens")
			streamCacheCreation = int(usage["cache_creation_input_tokens"].(float64))
			streamCacheRead = int(usage["cache_read_input_tokens"].(float64))
		}
	}

	require.Equal(t, []string{"thinking", "text"}, blockTypes)
	require.Equal(t, []int{0, 1}, blockIndices)
	require.Equal(t, []string{"start:thinking", "thinking_delta", "signature_delta", "stop"}, thinkingLifecycle)
	require.Equal(t, nonStream.Content[0].Thinking, thinking)
	require.Equal(t, nonStream.Content[0].Signature, signature)
	require.Equal(t, nonStream.Usage.InputTokens, streamInput)
	require.Equal(t, nonStream.Usage.OutputTokens, streamOutput)
	require.Equal(t, *nonStream.Usage.CacheCreationInputTokens, streamCacheCreation)
	require.Equal(t, *nonStream.Usage.CacheReadInputTokens, streamCacheRead)

	repeated := doAnthropicRequest(t, streamHandler.HandleMessages, req)
	var repeatedUsage map[string]any
	for _, block := range strings.Split(repeated.Body.String(), "\n\n") {
		if !strings.Contains(block, "event: message_delta") {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var payload map[string]any
				require.NoError(t, json.Unmarshal([]byte(data), &payload))
				repeatedUsage = payload["usage"].(map[string]any)
			}
		}
	}
	require.NotNil(t, repeatedUsage)
	require.EqualValues(t, 0, repeatedUsage["cache_creation_input_tokens"])
	require.Greater(t, repeatedUsage["cache_read_input_tokens"].(float64), float64(0))
}

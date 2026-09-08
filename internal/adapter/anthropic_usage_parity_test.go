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

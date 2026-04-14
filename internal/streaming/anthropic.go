package streaming

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// Anthropic SSE event types.

type anthropicMessageStart struct {
	Type    string                  `json:"type"`
	Message anthropicMessageHeader  `json:"message"`
}

type anthropicMessageHeader struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Role       string `json:"role"`
	Content    []any  `json:"content"`
	Model      string `json:"model"`
	StopReason *string `json:"stop_reason"`
	Usage      anthropicUsage `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicContentBlockStart struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock any    `json:"content_block"`
}

type anthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicToolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type anthropicContentBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta any    `json:"delta"`
}

type anthropicTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicInputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

type anthropicContentBlockStop struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type anthropicMessageDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage anthropicUsage `json:"usage"`
}

type anthropicMessageStop struct {
	Type string `json:"type"`
}

// StreamAnthropic writes an engine Response as an Anthropic-format SSE stream.
func StreamAnthropic(
	ctx context.Context,
	w http.ResponseWriter,
	resp *engine.Response,
	streamCfg *types.StreamingConfig,
) error {
	sse, err := NewSSEWriter(w)
	if err != nil {
		return err
	}

	chunkSize := DefaultChunkSize
	delayMs := DefaultChunkDelayMs
	if streamCfg != nil {
		if streamCfg.ChunkSize > 0 {
			chunkSize = streamCfg.ChunkSize
		}
		if streamCfg.ChunkDelayMs >= 0 {
			delayMs = streamCfg.ChunkDelayMs
		}
	}

	msgID := fmt.Sprintf("msg_%s", generateAnthropicID())

	// 1. message_start
	if err := sse.WriteEvent("message_start", anthropicMessageStart{
		Type: "message_start",
		Message: anthropicMessageHeader{
			ID: msgID, Type: "message", Role: "assistant",
			Content: []any{}, Model: resp.Model,
			Usage: anthropicUsage{InputTokens: 25, OutputTokens: 1},
		},
	}); err != nil {
		return err
	}

	blockIndex := 0

	// 2. Text content blocks.
	if resp.Content != "" {
		// content_block_start
		if err := sse.WriteEvent("content_block_start", anthropicContentBlockStart{
			Type: "content_block_start", Index: blockIndex,
			ContentBlock: anthropicTextBlock{Type: "text", Text: ""},
		}); err != nil {
			return err
		}

		// content_block_delta(s)
		chunker := NewChunker(chunkSize)
		chunks := chunker.Chunk(resp.Content)
		for _, chunk := range chunks {
			if err := ctx.Err(); err != nil {
				return err
			}
			if delayMs > 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
			if err := sse.WriteEvent("content_block_delta", anthropicContentBlockDelta{
				Type: "content_block_delta", Index: blockIndex,
				Delta: anthropicTextDelta{Type: "text_delta", Text: chunk},
			}); err != nil {
				return err
			}
		}

		// content_block_stop
		if err := sse.WriteEvent("content_block_stop", anthropicContentBlockStop{
			Type: "content_block_stop", Index: blockIndex,
		}); err != nil {
			return err
		}
		blockIndex++
	}

	// 3. Tool use blocks.
	for i, tc := range resp.ToolCalls {
		if err := ctx.Err(); err != nil {
			return err
		}

		toolID := fmt.Sprintf("toolu_%s", generateAnthropicID())
		if i < len(resp.ToolResults) {
			toolID = resp.ToolResults[i].ID
		}

		// content_block_start with tool_use
		if err := sse.WriteEvent("content_block_start", anthropicContentBlockStart{
			Type: "content_block_start", Index: blockIndex,
			ContentBlock: anthropicToolUseBlock{
				Type: "tool_use", ID: toolID, Name: tc.Name,
				Input: map[string]any{},
			},
		}); err != nil {
			return err
		}

		// input_json_delta chunks
		argsJSON, _ := json.Marshal(tc.Arguments)
		argChunks := chunkString(string(argsJSON), 20)
		for _, argChunk := range argChunks {
			if err := ctx.Err(); err != nil {
				return err
			}
			if delayMs > 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
			if err := sse.WriteEvent("content_block_delta", anthropicContentBlockDelta{
				Type: "content_block_delta", Index: blockIndex,
				Delta: anthropicInputJSONDelta{Type: "input_json_delta", PartialJSON: argChunk},
			}); err != nil {
				return err
			}
		}

		// content_block_stop
		if err := sse.WriteEvent("content_block_stop", anthropicContentBlockStop{
			Type: "content_block_stop", Index: blockIndex,
		}); err != nil {
			return err
		}
		blockIndex++
	}

	// 4. message_delta
	stopReason := "end_turn"
	if len(resp.ToolCalls) > 0 {
		stopReason = "tool_use"
	}
	outputTokens := len(resp.Content)/4 + 1
	if err := sse.WriteEvent("message_delta", anthropicMessageDelta{
		Type:  "message_delta",
		Delta: struct {
			StopReason string `json:"stop_reason"`
		}{StopReason: stopReason},
		Usage: anthropicUsage{OutputTokens: outputTokens},
	}); err != nil {
		return err
	}

	// 5. message_stop
	return sse.WriteEvent("message_stop", anthropicMessageStop{Type: "message_stop"})
}

func generateAnthropicID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

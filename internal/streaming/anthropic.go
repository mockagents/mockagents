package streaming

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// Anthropic SSE event types.

type anthropicMessageStart struct {
	Type    string                 `json:"type"`
	Message anthropicMessageHeader `json:"message"`
}

type anthropicMessageHeader struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []any          `json:"content"`
	Model      string         `json:"model"`
	StopReason *string        `json:"stop_reason"`
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
	// Delta usage carries ONLY output_tokens: an input_tokens:0 here clobbers
	// the message_start value in SDK accumulation (round-9 R9-10).
	Usage anthropicDeltaUsage `json:"usage"`
}

// anthropicDeltaUsage is message_delta's cumulative output count.
type anthropicDeltaUsage struct {
	OutputTokens int `json:"output_tokens"`
}

type anthropicMessageStop struct {
	Type string `json:"type"`
}

// StreamAnthropic writes an engine Response as an Anthropic-format SSE
// stream. The optional promptTokens is the computed input-token count for
// message_start's usage (round-9 R9-10 — a hardcoded value diverged from the
// non-streaming path); omitted, a deterministic default applies.
func StreamAnthropic(
	ctx context.Context,
	w http.ResponseWriter,
	resp *engine.Response,
	streamCfg *types.StreamingConfig,
	promptTokens ...int,
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

	pacer := newPacer(streamCfg)

	// Time-to-first-token: sleep before the FIRST emitted frame so a client's
	// first-byte timing reflects the configured TTFT (FB-05).
	if err := pacer.firstByte(ctx); err != nil {
		return err
	}

	// 1. message_start
	inputTokens := 25 // deterministic default for direct callers
	if len(promptTokens) > 0 {
		inputTokens = promptTokens[0]
	}
	if err := sse.WriteEvent("message_start", anthropicMessageStart{
		Type: "message_start",
		Message: anthropicMessageHeader{
			ID: msgID, Type: "message", Role: "assistant",
			Content: []any{}, Model: resp.Model,
			Usage: anthropicUsage{InputTokens: inputTokens, OutputTokens: 1},
		},
	}); err != nil {
		return err
	}

	blockIndex := 0

	// 2. Text content blocks (with stream-timing physics + fault injection).
	// A refusal-only response (FB-03) streams its refusal text as the text block.
	textBody := resp.Content
	if textBody == "" {
		textBody = resp.Refusal
	}
	if textBody != "" {
		// content_block_start
		if err := sse.WriteEvent("content_block_start", anthropicContentBlockStart{
			Type: "content_block_start", Index: blockIndex,
			ContentBlock: anthropicTextBlock{Type: "text", Text: ""},
		}); err != nil {
			return err
		}

		// content_block_delta(s)
		chunks := NewChunker(chunkSize).Chunk(textBody)
		for i, chunk := range chunks {
			if err := ctx.Err(); err != nil {
				return err
			}
			truncate, err := pacer.beforeChunk(ctx, i, tokenLen(chunk))
			if err != nil {
				return err
			}
			if truncate {
				return pacer.writeStop(sse) // truncated/malformed: no stop events
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

	if pacer.malformed {
		return pacer.writeStop(sse)
	}

	// 3. Tool use blocks.
	for i, tc := range resp.ToolCalls {
		if err := ctx.Err(); err != nil {
			return err
		}

		// The engine mints provider-neutral call_<hex> ids; the Anthropic wire
		// uses toolu_ (round-9 R9-7). Reuse the hex so logs still correlate.
		toolID := fmt.Sprintf("toolu_%s", generateAnthropicID())
		if i < len(resp.ToolResults) && resp.ToolResults[i].ID != "" {
			toolID = "toolu_" + strings.TrimPrefix(resp.ToolResults[i].ID, "call_")
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

		// input_json_delta chunks. An EMPTY input streams no deltas at all —
		// the block stays the {} from content_block_start; the real API sends
		// nothing (round-9 R9-6: a "null" delta made SDK accumulation set the
		// snapshot's input to None).
		if len(tc.Arguments) > 0 {
			argsJSON, _ := json.Marshal(tc.Arguments)
			argChunks := chunkString(string(argsJSON), 20)
			for _, argChunk := range argChunks {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := sleepCtx(ctx, time.Duration(delayMs)*time.Millisecond); err != nil {
					return err
				}
				if err := sse.WriteEvent("content_block_delta", anthropicContentBlockDelta{
					Type: "content_block_delta", Index: blockIndex,
					Delta: anthropicInputJSONDelta{Type: "input_json_delta", PartialJSON: argChunk},
				}); err != nil {
					return err
				}
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
	if resp.Refusal != "" {
		stopReason = "refusal"
	}
	if len(resp.ToolCalls) > 0 {
		stopReason = "tool_use"
	}
	// Scenario-forced stop reason (e.g. "length" -> max_tokens) wins (FB-03).
	if resp.FinishReason != "" {
		stopReason = AnthropicStopReason(resp.FinishReason)
	}
	outputTokens := len(resp.Content)/4 + 1
	if err := sse.WriteEvent("message_delta", anthropicMessageDelta{
		Type: "message_delta",
		Delta: struct {
			StopReason string `json:"stop_reason"`
		}{StopReason: stopReason},
		Usage: anthropicDeltaUsage{OutputTokens: outputTokens},
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

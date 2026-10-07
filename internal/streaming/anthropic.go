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

type anthropicThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
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

type anthropicThinkingDelta struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type anthropicSignatureDelta struct {
	Type      string `json:"type"`
	Signature string `json:"signature"`
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
	OutputTokens             int  `json:"output_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens,omitempty"`
}

type anthropicMessageStop struct {
	Type string `json:"type"`
}

// AnthropicStreamOptions carries adapter-computed Anthropic response metadata.
// Omitted, StreamAnthropic keeps deterministic defaults for direct callers.
type AnthropicStreamOptions struct {
	InputTokens              int
	OutputTokens             int
	Thinking                 string
	ThinkingSignature        string
	CacheCreationInputTokens *int
	CacheReadInputTokens     *int
}

// StreamAnthropic writes an engine Response as an Anthropic-format SSE
// stream. Optional metadata carries adapter-computed usage and thinking data.
// Omitted, deterministic defaults apply for direct callers.
func StreamAnthropic(
	ctx context.Context,
	w http.ResponseWriter,
	resp *engine.Response,
	streamCfg *types.StreamingConfig,
	metadata ...AnthropicStreamOptions,
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
		if streamCfg.ChunkDelayMs != nil {
			delayMs = *streamCfg.ChunkDelayMs
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
	opts := AnthropicStreamOptions{
		InputTokens:  25,
		OutputTokens: len(resp.Content)/4 + 1,
	}
	if len(metadata) > 0 {
		opts = metadata[0]
	}
	if err := sse.WriteEvent("message_start", anthropicMessageStart{
		Type: "message_start",
		Message: anthropicMessageHeader{
			ID: msgID, Type: "message", Role: "assistant",
			Content: []any{}, Model: resp.Model,
			Usage: anthropicUsage{InputTokens: opts.InputTokens, OutputTokens: 1},
		},
	}); err != nil {
		return err
	}

	blockIndex := 0
	if opts.Thinking != "" {
		if err := sse.WriteEvent("content_block_start", anthropicContentBlockStart{
			Type: "content_block_start", Index: blockIndex,
			ContentBlock: anthropicThinkingBlock{Type: "thinking", Thinking: "", Signature: ""},
		}); err != nil {
			return err
		}
		if err := sse.WriteEvent("content_block_delta", anthropicContentBlockDelta{
			Type: "content_block_delta", Index: blockIndex,
			Delta: anthropicThinkingDelta{Type: "thinking_delta", Thinking: opts.Thinking},
		}); err != nil {
			return err
		}
		if err := sse.WriteEvent("content_block_delta", anthropicContentBlockDelta{
			Type: "content_block_delta", Index: blockIndex,
			Delta: anthropicSignatureDelta{Type: "signature_delta", Signature: opts.ThinkingSignature},
		}); err != nil {
			return err
		}
		if err := sse.WriteEvent("content_block_stop", anthropicContentBlockStop{
			Type: "content_block_stop", Index: blockIndex,
		}); err != nil {
			return err
		}
		blockIndex++
	}

	// 2. Text content blocks (with stream-timing physics + fault injection).
	// The content and the refusal (FB-03) each stream as their own text
	// block, matching the non-streaming response, which carries both. Only
	// the content used to stream when both were set, silently dropping the
	// refusal (audit L-14).
	var textBodies []string
	for _, body := range []string{resp.Content, resp.Refusal} {
		if body != "" {
			textBodies = append(textBodies, body)
		}
	}
	chunkIdx := 0
	for _, textBody := range textBodies {
		// content_block_start
		if err := sse.WriteEvent("content_block_start", anthropicContentBlockStart{
			Type: "content_block_start", Index: blockIndex,
			ContentBlock: anthropicTextBlock{Type: "text", Text: ""},
		}); err != nil {
			return err
		}

		// content_block_delta(s)
		for _, chunk := range NewChunker(chunkSize).Chunk(textBody) {
			if err := ctx.Err(); err != nil {
				return err
			}
			truncate, err := pacer.beforeChunk(ctx, chunkIdx, tokenLen(chunk))
			chunkIdx++
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
	if err := sse.WriteEvent("message_delta", anthropicMessageDelta{
		Type: "message_delta",
		Delta: struct {
			StopReason string `json:"stop_reason"`
		}{StopReason: stopReason},
		Usage: anthropicDeltaUsage{
			OutputTokens:             opts.OutputTokens,
			CacheCreationInputTokens: opts.CacheCreationInputTokens,
			CacheReadInputTokens:     opts.CacheReadInputTokens,
		},
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

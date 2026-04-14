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

// OpenAI SSE chunk types matching the OpenAI API format.

// ChatCompletionChunk is a single SSE chunk in OpenAI streaming format.
type ChatCompletionChunk struct {
	ID      string                `json:"id"`
	Object  string                `json:"object"`
	Created int64                 `json:"created"`
	Model   string                `json:"model"`
	Choices []ChunkChoice         `json:"choices"`
}

// ChunkChoice represents a single choice in a streaming chunk.
type ChunkChoice struct {
	Index        int          `json:"index"`
	Delta        ChunkDelta   `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
}

// ChunkDelta represents incremental content changes.
type ChunkDelta struct {
	Role      string              `json:"role,omitempty"`
	Content   string              `json:"content,omitempty"`
	ToolCalls []ChunkToolCall     `json:"tool_calls,omitempty"`
}

// ChunkToolCall represents a tool call in a streaming chunk.
type ChunkToolCall struct {
	Index    int                  `json:"index"`
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function ChunkFunction        `json:"function"`
}

// ChunkFunction represents a function call chunk.
type ChunkFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// StreamOpenAI writes an engine Response as an OpenAI-format SSE stream.
func StreamOpenAI(
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

	id := fmt.Sprintf("chatcmpl-%s", generateStreamID())
	created := time.Now().Unix()

	// 1. Role chunk.
	if err := sse.WriteData(ChatCompletionChunk{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: resp.Model,
		Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant"}}},
	}); err != nil {
		return err
	}

	// 2. Content chunks.
	if resp.Content != "" {
		chunker := NewChunker(chunkSize)
		chunks := chunker.Chunk(resp.Content)
		for _, chunk := range chunks {
			if err := ctx.Err(); err != nil {
				return err
			}
			if delayMs > 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
			if err := sse.WriteData(ChatCompletionChunk{
				ID: id, Object: "chat.completion.chunk", Created: created, Model: resp.Model,
				Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: chunk}}},
			}); err != nil {
				return err
			}
		}
	}

	// 3. Tool call chunks (if any).
	if len(resp.ToolCalls) > 0 {
		if err := streamOpenAIToolCalls(ctx, sse, id, created, resp, delayMs); err != nil {
			return err
		}
	}

	// 4. Finish chunk.
	finishReason := "stop"
	if len(resp.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}
	if err := sse.WriteData(ChatCompletionChunk{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: resp.Model,
		Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &finishReason}},
	}); err != nil {
		return err
	}

	// 5. [DONE] sentinel.
	return sse.WriteRaw("[DONE]")
}

func streamOpenAIToolCalls(
	ctx context.Context,
	sse *SSEWriter,
	id string,
	created int64,
	resp *engine.Response,
	delayMs int,
) error {
	for i, tc := range resp.ToolCalls {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Resolve tool call ID from results if available.
		callID := fmt.Sprintf("call_%d", i)
		if i < len(resp.ToolResults) {
			callID = resp.ToolResults[i].ID
		}

		// Marshal arguments.
		argsJSON, _ := json.Marshal(tc.Arguments)
		argsStr := string(argsJSON)

		if delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}

		// First chunk: function name.
		if err := sse.WriteData(ChatCompletionChunk{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: resp.Model,
			Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{
				ToolCalls: []ChunkToolCall{{
					Index: i, ID: callID, Type: "function",
					Function: ChunkFunction{Name: tc.Name},
				}},
			}}},
		}); err != nil {
			return err
		}

		// Argument chunks — split JSON into pieces.
		argChunks := chunkString(argsStr, 20)
		for _, argChunk := range argChunks {
			if err := ctx.Err(); err != nil {
				return err
			}
			if delayMs > 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
			if err := sse.WriteData(ChatCompletionChunk{
				ID: id, Object: "chat.completion.chunk", Created: created, Model: resp.Model,
				Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{
					ToolCalls: []ChunkToolCall{{
						Index:    i,
						Function: ChunkFunction{Arguments: argChunk},
					}},
				}}},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// chunkString splits a string into pieces of at most size characters.
func chunkString(s string, size int) []string {
	if size <= 0 || len(s) == 0 {
		return []string{s}
	}
	var chunks []string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

func generateStreamID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

package streaming

import (
	"context"
	"net/http"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// Gemini SSE chunk types matching the streamGenerateContent (alt=sse) format.
// Each SSE event is a full GenerateContentResponse carrying a partial set of
// parts; the final event additionally carries finishReason. Unlike OpenAI,
// Gemini does not terminate the stream with a [DONE] sentinel.

type geminiStreamResponse struct {
	Candidates    []geminiStreamCandidate `json:"candidates"`
	UsageMetadata *geminiStreamUsage      `json:"usageMetadata,omitempty"`
}

type geminiStreamUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiStreamCandidate struct {
	Content      geminiStreamContent `json:"content"`
	FinishReason string              `json:"finishReason,omitempty"`
	Index        int                 `json:"index"`
}

type geminiStreamContent struct {
	Role  string             `json:"role"`
	Parts []geminiStreamPart `json:"parts"`
}

type geminiStreamPart struct {
	Text         string                    `json:"text,omitempty"`
	FunctionCall *geminiStreamFunctionCall `json:"functionCall,omitempty"`
}

type geminiStreamFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// StreamGemini writes an engine Response as a Gemini-format SSE stream
// (streamGenerateContent?alt=sse). Content is emitted in chunks sized by the
// agent's streaming config, followed by any function-call parts, then a final
// event carrying finishReason: "STOP".
func StreamGemini(
	ctx context.Context,
	w http.ResponseWriter,
	resp *engine.Response,
	streamCfg *types.StreamingConfig,
	promptTokens, candidateTokens int,
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

	pacer := newPacer(streamCfg)

	// 1. Content chunks (with stream-timing physics + fault injection).
	if resp.Content != "" {
		if err := pacer.firstByte(ctx); err != nil {
			return err
		}
		chunks := NewChunker(chunkSize).Chunk(resp.Content)
		for i, chunk := range chunks {
			if err := ctx.Err(); err != nil {
				return err
			}
			truncate, err := pacer.beforeChunk(ctx, i, tokenLen(chunk))
			if err != nil {
				return err
			}
			if truncate {
				return pacer.writeStop(sse) // truncated/malformed: no final event
			}
			if err := sse.WriteData(geminiStreamResponse{
				Candidates: []geminiStreamCandidate{{
					Content: geminiStreamContent{Role: "model", Parts: []geminiStreamPart{{Text: chunk}}},
					Index:   0,
				}},
			}); err != nil {
				return err
			}
		}
	}

	if pacer.malformed {
		return pacer.writeStop(sse)
	}

	// 2. Function-call parts (if any).
	for _, tc := range resp.ToolCalls {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sleepCtx(ctx, time.Duration(delayMs)*time.Millisecond); err != nil {
			return err
		}
		if err := sse.WriteData(geminiStreamResponse{
			Candidates: []geminiStreamCandidate{{
				Content: geminiStreamContent{Role: "model", Parts: []geminiStreamPart{{
					FunctionCall: &geminiStreamFunctionCall{Name: tc.Name, Args: tc.Arguments},
				}}},
				Index: 0,
			}},
		}); err != nil {
			return err
		}
	}

	// 3. Final event with finishReason + usage (no [DONE] sentinel in Gemini).
	// The real streamGenerateContent emits usageMetadata, in practice on the
	// terminal chunk.
	return sse.WriteData(geminiStreamResponse{
		Candidates: []geminiStreamCandidate{{
			Content:      geminiStreamContent{Role: "model", Parts: []geminiStreamPart{}},
			FinishReason: "STOP",
			Index:        0,
		}},
		UsageMetadata: &geminiStreamUsage{
			PromptTokenCount:     promptTokens,
			CandidatesTokenCount: candidateTokens,
			TotalTokenCount:      promptTokens + candidateTokens,
		},
	})
}

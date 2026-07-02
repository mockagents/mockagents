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
	Args map[string]any `json:"args,omitzero"` // no-arg calls render "args": {} (R9-6)
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

	// Time-to-first-token: sleep before the FIRST emitted frame so a client's
	// first-byte timing reflects the configured TTFT (FB-05).
	if err := pacer.firstByte(ctx); err != nil {
		return err
	}

	// finishReason + usage ride the LAST content-bearing chunk (round-9
	// R9-13) — the real API attaches them there; a separate empty-parts
	// terminal chunk broke clients reading parts[0] off the finish chunk.
	finishReason := "STOP"
	if resp.FinishReason != "" {
		finishReason = GeminiFinishReason(resp.FinishReason) // FB-03: e.g. "length" -> MAX_TOKENS
	} else if resp.Refusal != "" {
		finishReason = "SAFETY"
	}
	usage := &geminiStreamUsage{
		PromptTokenCount:     promptTokens,
		CandidatesTokenCount: candidateTokens,
		TotalTokenCount:      promptTokens + candidateTokens,
	}
	textBody := resp.Content
	if textBody == "" {
		textBody = resp.Refusal // refusal-only responses stream the refusal text (FB-03)
	}
	var texts []string
	if textBody != "" {
		texts = NewChunker(chunkSize).Chunk(textBody)
	}
	totalParts := len(texts) + len(resp.ToolCalls)
	emitted := 0
	write := func(part geminiStreamPart) error {
		emitted++
		out := geminiStreamResponse{Candidates: []geminiStreamCandidate{{
			Content: geminiStreamContent{Role: "model", Parts: []geminiStreamPart{part}},
			Index:   0,
		}}}
		// A malformed-fault stream ends with the injected bad frame instead of
		// a finish — never decorate its last chunk.
		if emitted == totalParts && !pacer.malformed {
			out.Candidates[0].FinishReason = finishReason
			out.UsageMetadata = usage
		}
		return sse.WriteData(out)
	}

	// 1. Content chunks (with stream-timing physics + fault injection).
	for i, chunk := range texts {
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
		if err := write(geminiStreamPart{Text: chunk}); err != nil {
			return err
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
		if err := write(geminiStreamPart{
			FunctionCall: &geminiStreamFunctionCall{Name: tc.Name, Args: tc.ArgumentsObject()},
		}); err != nil {
			return err
		}
	}

	// Degenerate case (no content, no tool calls): a lone finish chunk is
	// unavoidable — but its parts stay empty only because there is nothing
	// to attach the finish to.
	if totalParts == 0 {
		return sse.WriteData(geminiStreamResponse{
			Candidates: []geminiStreamCandidate{{
				Content:      geminiStreamContent{Role: "model", Parts: []geminiStreamPart{}},
				FinishReason: finishReason,
				Index:        0,
			}},
			UsageMetadata: usage,
		})
	}
	return nil
}

package adapter

import (
	"context"
	"net/http"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/types"
)

// streamResponses emits an engine result as a Responses-API SSE stream. The
// Responses surface uses *named* events (event: response.<...>) carrying a
// monotonic sequence_number, unlike Chat Completions' data-only chunk stream.
// The event ladder mirrors the real API so an Agents-SDK client's streaming
// state machine advances exactly as it would against OpenAI:
//
//	response.created → response.in_progress
//	  per output item:
//	    response.output_item.added
//	    (text)  content_part.added → output_text.delta* → output_text.done → content_part.done
//	    (tool)  function_call_arguments.delta* → function_call_arguments.done
//	    response.output_item.done
//	response.completed (or response.incomplete)
//
// `out` is the fully-built completed response; this function replays it
// incrementally. On a client disconnect (ctx cancel / write error) it returns
// the error and the caller abandons the half-written stream.
func streamResponses(
	ctx context.Context,
	w http.ResponseWriter,
	out *ResponsesResponse,
	resp *engine.Response,
	streamCfg *types.StreamingConfig,
) error {
	sse, err := streaming.NewSSEWriter(w)
	if err != nil {
		return err
	}

	chunkSize := streaming.DefaultChunkSize
	delayMs := streaming.DefaultChunkDelayMs
	if streamCfg != nil {
		if streamCfg.ChunkSize > 0 {
			chunkSize = streamCfg.ChunkSize
		}
		if streamCfg.ChunkDelayMs >= 0 {
			delayMs = streamCfg.ChunkDelayMs
		}
	}
	delay := time.Duration(delayMs) * time.Millisecond

	seq := 0
	emit := func(eventType string, payload map[string]any) error {
		payload["type"] = eventType
		payload["sequence_number"] = seq
		seq++
		return sse.WriteEvent(eventType, payload)
	}

	// response.created + response.in_progress carry a snapshot with no output
	// yet and status in_progress.
	pending := responsesSnapshot(out, "in_progress", []any{})
	if err := emit("response.created", map[string]any{"response": pending}); err != nil {
		return err
	}
	if err := emit("response.in_progress", map[string]any{"response": pending}); err != nil {
		return err
	}

	for idx, item := range out.Output {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch it := item.(type) {
		case responseMessageItem:
			if err := streamMessageItem(ctx, emit, idx, it, chunkSize, delay); err != nil {
				return err
			}
		case responseFunctionCallItem:
			if err := streamFunctionCallItem(ctx, emit, idx, it, delay); err != nil {
				return err
			}
		}
	}

	finalEvent := "response.completed"
	if out.Status == "incomplete" {
		finalEvent = "response.incomplete"
	}
	return emit(finalEvent, map[string]any{"response": out})
}

// streamMessageItem replays a "message" output item: it opens the item and its
// single content part, streams the text (or refusal) in deltas, then closes the
// part and the item.
func streamMessageItem(
	ctx context.Context,
	emit func(string, map[string]any) error,
	idx int,
	item responseMessageItem,
	chunkSize int,
	delay time.Duration,
) error {
	// The opening item snapshot has an empty content slice + in_progress status.
	opening := responseMessageItem{
		Type: "message", ID: item.ID, Status: "in_progress",
		Role: item.Role, Content: []any{},
	}
	if err := emit("response.output_item.added", map[string]any{
		"output_index": idx, "item": opening,
	}); err != nil {
		return err
	}

	switch part := firstContentPart(item).(type) {
	case responseOutputText:
		if err := emit("response.content_part.added", map[string]any{
			"item_id": item.ID, "output_index": idx, "content_index": 0,
			"part": responseOutputText{Type: "output_text", Text: "", Annotations: []any{}},
		}); err != nil {
			return err
		}
		for _, chunk := range streaming.NewChunker(chunkSize).Chunk(part.Text) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := sleepResponses(ctx, delay); err != nil {
				return err
			}
			if err := emit("response.output_text.delta", map[string]any{
				"item_id": item.ID, "output_index": idx, "content_index": 0,
				"delta": chunk, "logprobs": []any{},
			}); err != nil {
				return err
			}
		}
		if err := emit("response.output_text.done", map[string]any{
			"item_id": item.ID, "output_index": idx, "content_index": 0,
			"text": part.Text, "logprobs": []any{},
		}); err != nil {
			return err
		}
		if err := emit("response.content_part.done", map[string]any{
			"item_id": item.ID, "output_index": idx, "content_index": 0, "part": part,
		}); err != nil {
			return err
		}
	case responseRefusalPart:
		if err := emit("response.content_part.added", map[string]any{
			"item_id": item.ID, "output_index": idx, "content_index": 0,
			"part": responseRefusalPart{Type: "refusal", Refusal: ""},
		}); err != nil {
			return err
		}
		if err := emit("response.refusal.delta", map[string]any{
			"item_id": item.ID, "output_index": idx, "content_index": 0, "delta": part.Refusal,
		}); err != nil {
			return err
		}
		if err := emit("response.refusal.done", map[string]any{
			"item_id": item.ID, "output_index": idx, "content_index": 0, "refusal": part.Refusal,
		}); err != nil {
			return err
		}
		if err := emit("response.content_part.done", map[string]any{
			"item_id": item.ID, "output_index": idx, "content_index": 0, "part": part,
		}); err != nil {
			return err
		}
	}

	return emit("response.output_item.done", map[string]any{"output_index": idx, "item": item})
}

// streamFunctionCallItem replays a "function_call" output item: it opens the
// item with empty arguments, streams the argument JSON in deltas, then closes.
func streamFunctionCallItem(
	ctx context.Context,
	emit func(string, map[string]any) error,
	idx int,
	item responseFunctionCallItem,
	delay time.Duration,
) error {
	opening := responseFunctionCallItem{
		Type: "function_call", ID: item.ID, CallID: item.CallID,
		Name: item.Name, Arguments: "", Status: "in_progress",
	}
	if err := emit("response.output_item.added", map[string]any{
		"output_index": idx, "item": opening,
	}); err != nil {
		return err
	}

	for _, chunk := range chunkArguments(item.Arguments, 20) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sleepResponses(ctx, delay); err != nil {
			return err
		}
		if err := emit("response.function_call_arguments.delta", map[string]any{
			"item_id": item.ID, "output_index": idx, "delta": chunk,
		}); err != nil {
			return err
		}
	}
	if err := emit("response.function_call_arguments.done", map[string]any{
		"item_id": item.ID, "output_index": idx, "arguments": item.Arguments,
	}); err != nil {
		return err
	}

	return emit("response.output_item.done", map[string]any{"output_index": idx, "item": item})
}

// chunkArguments splits a function-call argument JSON string into byte-bounded
// pieces so the arguments arrive as a sequence of deltas, the way a real
// streamed tool call does. An empty string yields a single empty chunk so the
// delta/done pair is still emitted.
func chunkArguments(s string, size int) []string {
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

// firstContentPart returns the message item's single content part (the engine
// produces exactly one: an output_text or a refusal), or nil if absent.
func firstContentPart(item responseMessageItem) any {
	if len(item.Content) == 0 {
		return nil
	}
	return item.Content[0]
}

// responsesSnapshot shallow-copies the response with an overridden status and
// output — used for the in_progress snapshots that precede the real output.
func responsesSnapshot(out *ResponsesResponse, status string, output []any) *ResponsesResponse {
	cp := *out
	cp.Status = status
	cp.Output = output
	return &cp
}

// sleepResponses sleeps for d, returning early if the client disconnects.
func sleepResponses(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

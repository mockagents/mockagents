// Round-8 fidelity regression tests (2026-07-02 eval — first live-SDK round):
// VAD auto-commit vs the client-commit floor, tool-loop convergence, and the
// behavior/shape batches.
package realtime

import (
	"context"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// R8-2 (S2, live-SDK proven): the tool loop must converge — a follow-up right
// after a function_call_output must not re-issue the identical function_call
// (an SDK agent loop answering every call would spin forever). A DIFFERENT
// call (deliberate multi-step chain) still goes out, and a fresh user turn
// re-arms the original call.
func TestToolLoopConverges(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r82", "", fakeGenTool("Checking the weather.",
		types.ToolCallSpec{Name: "get_weather", Arguments: map[string]any{"location": "NYC"}}))

	// Turn 1: user asks → function_call ladder.
	s.Handle(ctx, mkUserItem("item_q1", "What is the weather in NYC?"))
	evs := s.Handle(ctx, &ClientEvent{Type: "response.create"})
	fcDone := firstEvent(evs, "response.function_call_arguments.done")
	if fcDone == nil {
		t.Fatalf("first response = %v, want the function_call ladder", typesOf(evs))
	}

	// The client answers the call → the follow-up must NOT re-issue it.
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.create",
		Item: []byte(`{"type":"function_call_output","call_id":"` + fcDone["call_id"].(string) + `","output":"{\"temp\":72}"}`)})
	evs = s.Handle(ctx, &ClientEvent{Type: "response.create"})
	if firstEvent(evs, "response.function_call_arguments.done") != nil {
		t.Fatalf("follow-up re-issued the identical function_call: %v", typesOf(evs))
	}
	if firstEvent(evs, "response.done") == nil {
		t.Fatalf("follow-up must complete as a message response: %v", typesOf(evs))
	}

	// A fresh user turn re-arms the tool call.
	s.Handle(ctx, mkUserItem("item_q2", "and the weather tomorrow?"))
	evs = s.Handle(ctx, &ClientEvent{Type: "response.create"})
	if firstEvent(evs, "response.function_call_arguments.done") == nil {
		t.Errorf("a new user turn must re-arm the tool call: %v", typesOf(evs))
	}
}

// R8-1 (S2, two-lens proven): the server's own VAD end-of-turn commit must
// never trip the 100ms CLIENT-commit floor — a short turn previously emitted
// a client-shaped error, answered with an empty context, and re-fired the
// turn on every subsequent silent append (two responses for one utterance).
func TestVADAutoCommitBypassesClientFloor(t *testing.T) {
	ctx := context.Background()
	var histories [][]engine.RequestMessage
	gen := func(_ context.Context, _, _ string, history []engine.RequestMessage) (*engine.Response, error) {
		histories = append(histories, append([]engine.RequestMessage(nil), history...))
		return &engine.Response{Content: "short answer"}, nil
	}
	s := NewSession("r81", "", gen)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","silence_duration_ms":50,"prefix_padding_ms":0}}}}`)

	// 30ms speech + 60ms silence → an 80-90ms turn window.
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(30, speechAmp)})
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(60, quietAmp)})
	tps := typesOf(evs)
	if contains(tps, "error") {
		t.Fatalf("VAD end-of-turn emitted a client-shaped error: %v", tps)
	}
	if !contains(tps, "input_audio_buffer.committed") || !contains(tps, "response.done") {
		t.Fatalf("short turn = %v, want commit + auto-response", tps)
	}
	if len(histories) != 1 || len(histories[0]) == 0 {
		t.Fatalf("engine histories = %v, want one generation WITH the committed turn", histories)
	}

	// Further silence re-fires nothing — the cycle closed with the commit.
	if evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(60, quietAmp)}); len(evs) != 0 {
		t.Errorf("post-turn silence emitted %v, want nothing", typesOf(evs))
	}
	if len(histories) != 1 {
		t.Errorf("generations = %d, want exactly 1 (no duplicate response)", len(histories))
	}

	// The floor still applies to CLIENT commits.
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.clear"})
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(50, quietAmp)})
	got := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})
	if got[0]["type"] != "error" || got[0]["error"].(map[string]any)["code"] != "input_audio_buffer_commit_empty" {
		t.Errorf("client sub-100ms commit = %v, want the buffer-too-small error", got[0])
	}
}

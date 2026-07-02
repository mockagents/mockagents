// Round-8 fidelity regression tests (2026-07-02 eval — first live-SDK round):
// VAD auto-commit vs the client-commit floor, tool-loop convergence, and the
// behavior/shape batches.
package realtime

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// R8-7/R8-9 (S3): parallel_tool_calls is a beta leftover — absent from the
// session object and rejected in strict mode; audio-modality output bills as
// audio tokens.
func TestShapeBatch(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r87", "", fakeGen("spoken words"))

	if _, ok := s.Greeting()[0]["session"].(map[string]any)["parallel_tool_calls"]; ok {
		t.Error("session object must not carry parallel_tool_calls")
	}
	s.SetStrict(true)
	evs := s.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(`{"parallel_tool_calls":false}`)})
	if e := evs[0]["error"].(map[string]any); evs[0]["type"] != "error" || e["code"] != "unknown_parameter" {
		t.Errorf("strict parallel_tool_calls = %v, want unknown_parameter", evs[0])
	}
	s.SetStrict(false)

	// Audio response → output billed as audio tokens.
	evs = s.Handle(ctx, &ClientEvent{Type: "response.create"})
	usage := firstEvent(evs, "response.done")["response"].(map[string]any)["usage"].(map[string]any)
	details := usage["output_token_details"].(map[string]any)
	if details["audio_tokens"] != 2 || details["text_tokens"] != 0 {
		t.Errorf("audio output details = %v, want audio_tokens=2 text_tokens=0", details)
	}
	// Text-only response → text tokens.
	evs = s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"output_modalities":["text"]}`)})
	details = firstEvent(evs, "response.done")["response"].(map[string]any)["usage"].(map[string]any)["output_token_details"].(map[string]any)
	if details["text_tokens"] != 2 || details["audio_tokens"] != 0 {
		t.Errorf("text output details = %v, want text_tokens=2 audio_tokens=0", details)
	}
}

// R8-8 (S3): an invalid mint payload must not silently seed a broken config —
// ValidateSessionPayload rejects it (the adapter 400s), and SeedConfig keeps
// the working turn_detection if a bad one slips through anyway.
func TestMintValidationAndSeedFallback(t *testing.T) {
	if param, _, ok := ValidateSessionPayload([]byte(`{"audio":{"input":{"turn_detection":{"type":"server-vad"}}}}`)); ok ||
		param != "session.audio.input.turn_detection.type" {
		t.Errorf("typo'd turn_detection: param=%q ok=%v, want rejection naming the type", param, ok)
	}
	if _, _, ok := ValidateSessionPayload([]byte(`{"audio":{"output":{"voice":"verse"}}}`)); !ok {
		t.Error("a clean payload must validate")
	}

	s := NewSession("r88", "", fakeGen("ok"))
	s.SeedConfig([]byte(`{"instructions":"be brief","audio":{"input":{"turn_detection":{"type":"server-vad"}}}}`))
	if s.vad == nil {
		t.Error("an invalid seeded turn_detection must keep the working default, not disable VAD")
	}
	if s.cfg.instructions != "be brief" {
		t.Error("the valid parts of the seeded config must still apply")
	}
}

// R8-3 (S3): truncating the streaming message must not drop the
// function_call items' history entries (the round-7 truncate override
// replaced the whole appendHistory closure).
func TestTruncateKeepsFunctionCallHistory(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGenTool("alpha beta", types.ToolCallSpec{Name: "lookup"}), serverVAD)
	endVADTurn(t, s)

	var msgID string
	for range 4 {
		for _, ev := range s.Tick(ctx, fc.advance(10*time.Millisecond)) {
			if ev["type"] == "conversation.item.added" {
				msgID = ev["item"].(map[string]any)["id"].(string)
			}
		}
	}
	if msgID == "" {
		t.Fatal("setup: message item not announced")
	}
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.truncate", ItemID: msgID, AudioEndMs: 10})
	drain(t, s, fc, 200)

	assistants := 0
	for _, m := range s.history {
		if m.Role == "assistant" {
			assistants++
		}
	}
	if assistants != 2 {
		t.Errorf("assistant history entries = %d, want 2 (truncated message + function_call)", assistants)
	}
}

// R8-4 (S3): the VAD window must not shrink the CLIENT's buffer — a manual
// commit of everything appended (530ms) passes the floor and bills/stores the
// full buffer; the VAD's own commit still slices the window.
func TestManualCommitSeesFullClientBuffer(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r84", "", fakeGen("ok"))
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","prefix_padding_ms":0,"create_response":false},"transcription":{"model":"whisper-1"}}}}`)

	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(500, quietAmp)})
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(30, speechAmp)})
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})
	committed := firstEvent(evs, "input_audio_buffer.committed")
	if committed == nil {
		t.Fatalf("manual commit of a 530ms buffer = %v, want success", typesOf(evs))
	}
	// Billed duration covers the WHOLE client buffer.
	usage := firstEvent(evs, "conversation.item.input_audio_transcription.completed")["usage"].(map[string]any)
	if secs := usage["seconds"].(float64); secs < 0.52 || secs > 0.54 {
		t.Errorf("billed seconds = %v, want ~0.53 (the appended buffer)", secs)
	}
	// Stored audio covers the whole buffer too.
	got := firstEvent(s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve",
		ItemID: committed["item_id"].(string)}), "conversation.item.retrieved")
	audio, _ := got["item"].(map[string]any)["content"].([]any)[0].(map[string]any)["audio"].(string)
	raw, _ := base64.StdEncoding.DecodeString(audio)
	if len(raw) != 530*48 {
		t.Errorf("stored audio = %dms, want the full 530ms client buffer", len(raw)/48)
	}
}

// R8-5 (S3): a FAILED response re-arms the idle timeout like a successful one
// — a generation failure must not strand silence detection.
func TestFailedResponseRearmsIdle(t *testing.T) {
	gen := func(context.Context, string, string, []engine.RequestMessage) (*engine.Response, error) {
		return nil, fmt.Errorf("engine down")
	}
	s := NewSession("r85", "", gen)
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)

	evs := endVADTurn(t, s)
	if st := firstEvent(evs, "response.done")["response"].(map[string]any)["status"]; st != "failed" {
		t.Fatalf("setup: response status = %v, want failed", st)
	}
	if _, ok := s.NextDeadline(); !ok {
		t.Error("idle timeout not re-armed after a failed response")
	}
}

// R8-6 (S3): SeedConfig cannot flip a pinned session type — first-set-wins
// covers the mint path, not just session.update.
func TestSeedConfigRespectsPinnedSessionType(t *testing.T) {
	s := NewSession("r86", "", fakeGen("ok"))
	s.SetSessionType("transcription") // ?intent=transcription
	s.SeedConfig([]byte(`{"type":"realtime","instructions":"be brief"}`))
	if !s.isTranscription() {
		t.Error("a minted realtime payload flipped a session pinned to transcription")
	}
	if s.cfg.instructions != "be brief" {
		t.Error("the rest of the seeded config must still apply")
	}
}

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

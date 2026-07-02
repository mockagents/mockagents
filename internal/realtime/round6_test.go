// Round-6 fidelity regression tests (2026-07-02 eval): idle-timer teardown,
// emission-time previous_item_id, delete tombstones, cancel semantics, and
// the new response.create input / transcription-session surfaces.
package realtime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// R6-2 (S2): a paced item's conversation.item.added must chain off the
// conversation tail AT EMISSION, not the build-time cursor — a client item
// created mid-pace previously produced two items claiming the same
// predecessor (a forked chain).
func TestPaced_ChainPrevRewrittenAtEmission(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("paced reply"), serverVAD)
	evs := endVADTurn(t, s)
	userItem := firstEvent(evs, "input_audio_buffer.committed")["item_id"].(string)

	// Emit only response.output_item.added (the mirror .added is still queued).
	s.Tick(ctx, fc.advance(10*time.Millisecond))

	// A client item lands mid-pace, chaining off the user item (current tail).
	added := firstEvent(s.Handle(ctx, mkUserItem("cli_mid", "interleaved")), "conversation.item.added")
	if added["previous_item_id"] != userItem {
		t.Fatalf("setup: client item chains off %v, want %v", added["previous_item_id"], userItem)
	}

	// The response item's mirror pair must now chain off cli_mid — not repeat
	// the build-time cursor (the user item).
	rest := drain(t, s, fc, 100)
	msgAdded := firstEvent(rest, "conversation.item.added")
	if msgAdded["previous_item_id"] != "cli_mid" {
		t.Errorf("paced item .added previous_item_id = %v, want cli_mid (emission-time tail)", msgAdded["previous_item_id"])
	}
	msgDone := firstEvent(rest, "conversation.item.done")
	if msgDone["previous_item_id"] != "cli_mid" {
		t.Errorf(".done previous_item_id = %v, must match its .added", msgDone["previous_item_id"])
	}
}

// R6-2b (S2): deleting the tail mid-pace must not leave the next announced
// item chaining off the deleted id.
func TestPaced_ChainPrevSurvivesMidPaceDelete(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("paced reply"), serverVAD)
	evs := endVADTurn(t, s)
	userItem := firstEvent(evs, "input_audio_buffer.committed")["item_id"].(string)

	s.Tick(ctx, fc.advance(10*time.Millisecond)) // output_item.added only
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: userItem})

	rest := drain(t, s, fc, 100)
	msgAdded := firstEvent(rest, "conversation.item.added")
	if msgAdded["previous_item_id"] != nil {
		t.Errorf("announced item chains off %v, want null (its predecessor was deleted)", msgAdded["previous_item_id"])
	}
}

// R6-3 (S2): deleting an ANNOUNCED in-flight item must stick — its queued
// conversation.item.done previously re-indexed it (retrievable as completed
// yet absent from the chain).
func TestPaced_DeletedInflightItemStaysDeleted(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("short reply"), serverVAD)
	evs := endVADTurn(t, s)
	userItem := firstEvent(evs, "input_audio_buffer.committed")["item_id"].(string)

	// Emit through conversation.item.added so the assistant item is announced.
	var msgID string
	for range 3 {
		for _, ev := range s.Tick(ctx, fc.advance(10*time.Millisecond)) {
			if ev["type"] == "conversation.item.added" {
				msgID = ev["item"].(map[string]any)["id"].(string)
			}
		}
	}
	if msgID == "" {
		t.Fatal("setup: assistant item was not announced in 3 ticks")
	}

	if del := s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: msgID}); del[0]["type"] != "conversation.item.deleted" {
		t.Fatalf("delete = %v", typesOf(del))
	}
	drain(t, s, fc, 100) // the ladder still streams and completes

	got := s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve", ItemID: msgID})
	if got[0]["type"] != "error" || got[0]["error"].(map[string]any)["code"] != "item_not_found" {
		t.Errorf("deleted in-flight item was resurrected: retrieve = %v", got[0])
	}
	// The chain excludes it: the next item chains off the user item.
	added := firstEvent(s.Handle(ctx, mkUserItem("cli_after", "next")), "conversation.item.added")
	if added["previous_item_id"] != userItem {
		t.Errorf("next item chains off %v, want %v", added["previous_item_id"], userItem)
	}
}

// R6-6 (S2): a client response.cancel targets only the in-flight response —
// the pending auto-response of an already-committed turn survives and fires
// right after the close-out (previously the committed turn was never
// answered, the T-F4 failure resurfacing via the cancel path).
func TestPaced_ClientCancelKeepsPendingAutoResponse(t *testing.T) {
	ctx := context.Background()
	s, _ := pacedSession(t, fakeGen("manual then auto"), serverVAD)

	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	s.Handle(ctx, &ClientEvent{Type: "response.create"}) // inflight A
	// Turn ends mid-flight: committed, auto-response queued.
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
	if firstEvent(evs, "input_audio_buffer.committed") == nil {
		t.Fatalf("setup: turn did not commit: %v", typesOf(evs))
	}

	out := s.Handle(ctx, &ClientEvent{Type: "response.cancel"})
	tps := typesOf(out)
	cancelled := firstEvent(out, "response.done")
	if cancelled == nil || cancelled["response"].(map[string]any)["status"] != "cancelled" {
		t.Fatalf("cancel = %v, want the cancelled response.done", tps)
	}
	if firstEvent(out, "response.created") == nil {
		t.Errorf("the committed turn's auto-response must fire after the cancel; got %v", tps)
	}
	// Barge-in still supersedes pending (the new turn answers instead) —
	// covered by TestPaced_BargeIn asserting no stacked response.
}

// R6-7 (S3): errors from timer-initiated flows (idle timeout) carry
// error.event_id null — not the id of an unrelated earlier client event.
func TestTickErrorsCarryNoStaleEventID(t *testing.T) {
	ctx := context.Background()
	calls := 0
	gen := func(context.Context, string, string, []engine.RequestMessage) (*engine.Response, error) {
		calls++
		if calls > 1 {
			return nil, fmt.Errorf("engine down")
		}
		return &engine.Response{Content: "first answer"}, nil
	}
	s := NewSession("r67", "", gen)
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)

	// A stamped client event drives the successful turn; the idle timeout then
	// fires a FAILING response with no causing client event.
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", EventID: "evt_client_1", Audio: pcmChunk(200, speechAmp)})
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", EventID: "evt_client_2", Audio: pcmChunk(600, quietAmp)})

	evs := s.Tick(ctx, fc.advance(5*time.Second))
	errEv := firstEvent(evs, "error")
	if errEv == nil {
		t.Fatalf("idle Tick with a failing engine = %v, want an error event", typesOf(evs))
	}
	if got := errEv["error"].(map[string]any)["event_id"]; got != nil {
		t.Errorf("timer-initiated error.event_id = %v, want null (no causing client event)", got)
	}
}

// R6-8 (S3): cancelling before any function_call_arguments.delta emitted must
// not fabricate an arguments.done for the never-started stream.
func TestPaced_CancelBeforeArgsStreamSkipsArgumentsDone(t *testing.T) {
	ctx := context.Background()
	// Content "" + a tool call → a tool-only ladder (no message item).
	s, fc := pacedSession(t, fakeGenTool("", types.ToolCallSpec{Name: "lookup", Arguments: map[string]any{"q": "x"}}), serverVAD)
	endVADTurn(t, s)

	// Announce the function_call item, stop before its argument deltas.
	s.Tick(ctx, fc.advance(10*time.Millisecond))
	s.Tick(ctx, fc.advance(10*time.Millisecond))

	evs := s.Handle(ctx, &ClientEvent{Type: "response.cancel"})
	if firstEvent(evs, "response.function_call_arguments.done") != nil {
		t.Errorf("fabricated function_call_arguments.done for a never-started stream; got %v", typesOf(evs))
	}
	itemDone := firstEvent(evs, "response.output_item.done")
	if itemDone == nil {
		t.Fatalf("announced function_call item must still close out; got %v", typesOf(evs))
	}
	item := itemDone["item"].(map[string]any)
	if item["status"] != "incomplete" || item["arguments"] != "" {
		t.Errorf("item = status %v arguments %q, want incomplete with empty arguments", item["status"], item["arguments"])
	}
}

// R6-9 (S3): a semantically identical turn_detection re-sent with different
// key order must not wipe a mid-speech cycle (the byte-compare skip alone
// resurfaced T-F2 for non-byte-stable serializers).
func TestSessionUpdateReorderedVADConfigKeepsTurn(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r69", "", fakeGen("still here"))
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","threshold":0.5}}}}`)

	started := firstEvent(s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)}),
		"input_audio_buffer.speech_started")
	if started == nil {
		t.Fatal("setup: speech not detected")
	}
	pending := started["item_id"].(string)

	// Same config, keys reordered → bytes differ, semantics identical.
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"threshold":0.5,"type":"server_vad"}}}}`)

	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
	committed := firstEvent(evs, "input_audio_buffer.committed")
	if committed == nil {
		t.Fatalf("turn dropped after reordered-key config resend: %v", typesOf(evs))
	}
	if committed["item_id"] != pending {
		t.Errorf("committed item_id = %v, want the pre-announced %q", committed["item_id"], pending)
	}
}

// R6-10 (S3): a message item that fully streamed before a late cancel is a
// completed conversation item — its transcript must join the engine history
// even though the response ends status "cancelled".
func TestPaced_LateCancelKeepsCompletedTranscriptInHistory(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGenTool("alpha beta", types.ToolCallSpec{Name: "lookup"}), serverVAD)
	endVADTurn(t, s)

	// Drain until the MESSAGE item completes (its conversation.item.done with
	// status completed), leaving the function_call item still streaming.
	msgDone := false
	for i := 0; i < 100 && !msgDone; i++ {
		for _, ev := range s.Tick(ctx, fc.advance(10*time.Millisecond)) {
			if ev["type"] == "conversation.item.done" {
				if item := ev["item"].(map[string]any); item["type"] == "message" && item["status"] == "completed" {
					msgDone = true
				}
			}
		}
	}
	if !msgDone {
		t.Fatal("setup: message item never completed")
	}

	s.Handle(ctx, &ClientEvent{Type: "response.cancel"})
	found := false
	for _, m := range s.history {
		if m.Role == "assistant" && m.Content == "alpha beta" {
			found = true
		}
	}
	if !found {
		t.Errorf("completed transcript missing from history after late cancel: %+v", s.history)
	}
}

// R6-1 (S1): disabling turn_detection must disarm the idle timeout — a Tick
// firing afterwards nil-dereferenced the VAD state and killed the session.
func TestIdleTimerDisarmedWhenVADDisabled(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r61", "", fakeGen("ok"))
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)

	endVADTurn(t, s) // burst turn completes → idle deadline armed
	if _, ok := s.NextDeadline(); !ok {
		t.Fatal("setup: idle deadline should be armed")
	}

	enableVAD(t, s, `{"audio":{"input":{"turn_detection":null}}}`)
	if dl, ok := s.NextDeadline(); ok {
		t.Fatalf("idle deadline %v survived disabling VAD", dl)
	}
	// Even if a stale timer fires, Tick must not panic and must stay silent.
	if evs := s.Tick(ctx, fc.advance(10*time.Second)); len(evs) != 0 {
		t.Errorf("Tick after VAD disable emitted %v, want nothing", typesOf(evs))
	}
}

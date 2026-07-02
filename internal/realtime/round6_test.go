// Round-6 fidelity regression tests (2026-07-02 eval): idle-timer teardown,
// emission-time previous_item_id, delete tombstones, cancel semantics, and
// the new response.create input / transcription-session surfaces.
package realtime

import (
	"context"
	"testing"
	"time"
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

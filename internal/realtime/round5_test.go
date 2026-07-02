// Round-5 fidelity regression tests (2026-07-02 eval): phantom items on early
// cancel, chain repair on delete, out-of-band item scoping, session.update
// mid-speech, voice locking, and assorted wire-shape corrections.
package realtime

import (
	"context"
	"testing"
	"time"
)

func mkUserItem(id, text string) *ClientEvent {
	return &ClientEvent{Type: "conversation.item.create",
		Item: []byte(`{"id":"` + id + `","type":"message","role":"user","content":[{"type":"input_text","text":"` + text + `"}]}`)}
}

// T-F3: deleting the chain-tail item must repair lastItemID — the next item
// chains off the new tail, not off an id the server itself would reject.
func TestDeleteTailRepairsChain(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sd", "", fakeGen("ok"))
	s.Handle(ctx, mkUserItem("item_a", "one"))
	s.Handle(ctx, mkUserItem("item_b", "two"))

	if evs := s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: "item_b"}); evs[0]["type"] != "conversation.item.deleted" {
		t.Fatalf("delete = %v", typesOf(evs))
	}
	added := firstEvent(s.Handle(ctx, mkUserItem("item_c", "three")), "conversation.item.added")
	if added["previous_item_id"] != "item_a" {
		t.Errorf("after tail delete, previous_item_id = %v, want item_a", added["previous_item_id"])
	}

	// Deleting everything empties the chain: the next item is first (prev null).
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: "item_a"})
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: "item_c"})
	added = firstEvent(s.Handle(ctx, mkUserItem("item_d", "four")), "conversation.item.added")
	if added["previous_item_id"] != nil {
		t.Errorf("after deleting all items, previous_item_id = %v, want null", added["previous_item_id"])
	}
}

// T-F2 (S1): a session.update that does not change turn_detection (voice,
// instructions, tools — routine mid-call, e.g. Agents-SDK handoffs) must not
// wipe an in-progress speech cycle: the turn still commits with the
// pre-announced item id and gets its auto-response.
func TestSessionUpdateMidSpeechKeepsTurn(t *testing.T) {
	ctx := context.Background()
	s := NewSession("su", "gpt-realtime", fakeGen("still with you"))
	enableVAD(t, s, serverVAD)

	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	started := firstEvent(evs, "input_audio_buffer.speech_started")
	if started == nil {
		t.Fatalf("speech append = %v, want speech_started", typesOf(evs))
	}
	pending := started["item_id"].(string)

	// Mid-speech instructions update — turn_detection untouched.
	upd := s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"instructions":"be brief"}`)})
	if upd[0]["type"] != "session.updated" {
		t.Fatalf("session.update = %v", typesOf(upd))
	}

	// Silence ends the turn: it must still commit with the pre-announced id and
	// auto-respond — under the old code the speech cycle was wiped and the
	// silence appends returned nothing (turn dropped).
	evs = s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
	committed := firstEvent(evs, "input_audio_buffer.committed")
	if committed == nil {
		t.Fatalf("turn-end after mid-speech update = %v, want the commit ladder", typesOf(evs))
	}
	if committed["item_id"] != pending {
		t.Errorf("committed item_id = %v, want the pre-announced %q", committed["item_id"], pending)
	}
	if firstEvent(evs, "response.done") == nil {
		t.Errorf("turn-end must auto-respond; got %v", typesOf(evs))
	}

	// Changing turn_detection itself still rebuilds the state machine.
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"audio":{"input":{"turn_detection":{"type":"server_vad","silence_duration_ms":900}}}}`)})
	if s.vad == nil || s.vad.cfg.SilenceDurationMs != 900 {
		t.Errorf("changed turn_detection not applied: %+v", s.vad)
	}
	if s.vad.speechActive {
		t.Error("a changed turn_detection config rebuilds the speech cycle")
	}
}

// LC-1: after a response has produced assistant audio the voice is locked —
// a differing voice rejects the WHOLE session.update with the verbatim GA
// error; re-sending the current voice stays accepted.
func TestVoiceLockedAfterAudio(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv5", "", fakeGen("spoken answer"))

	// Before any audio: voice changes freely.
	if evs := s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"audio":{"output":{"voice":"marin"}}}`)}); evs[0]["type"] != "session.updated" {
		t.Fatalf("pre-audio voice change = %v", typesOf(evs))
	}

	s.Handle(ctx, &ClientEvent{Type: "response.create"}) // default modalities → audio

	evs := s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"audio":{"output":{"voice":"cedar"}}}`)})
	if evs[0]["type"] != "error" {
		t.Fatalf("post-audio voice change = %v, want error", typesOf(evs))
	}
	errObj := evs[0]["error"].(map[string]any)
	if errObj["code"] != "cannot_update_voice" || errObj["type"] != "invalid_request_error" {
		t.Errorf("error = %v, want invalid_request_error/cannot_update_voice", errObj)
	}
	if errObj["message"] != "Cannot update a conversation's voice if assistant audio is present." {
		t.Errorf("message = %q, want the verbatim GA capture", errObj["message"])
	}
	if errObj["param"] != nil {
		t.Errorf("param = %v, want null", errObj["param"])
	}
	if contains(typesOf(evs), "session.updated") {
		t.Error("a rejected update must not emit session.updated")
	}
	if s.effectiveVoice() != "marin" {
		t.Errorf("voice = %q, want the locked marin", s.effectiveVoice())
	}

	// Re-sending the SAME voice (plus other fields) is not a change.
	evs = s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"instructions":"shorter","audio":{"output":{"voice":"marin"}}}`)})
	if evs[0]["type"] != "session.updated" {
		t.Errorf("same-voice update = %v, want session.updated", typesOf(evs))
	}

	// A text-only session never locks.
	s2 := NewSession("sv6", "", fakeGen("typed answer"))
	s2.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(`{"output_modalities":["text"]}`)})
	s2.Handle(ctx, &ClientEvent{Type: "response.create"})
	if evs := s2.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"audio":{"output":{"voice":"cedar"}}}`)}); evs[0]["type"] != "session.updated" {
		t.Errorf("text-only session locked the voice: %v", typesOf(evs))
	}
}

// T-F4: a response.create while VAD speech is active occupies the inflight
// slot; when the turn then ends, the commit must not forward-reference an
// unannounced item and the auto-response is QUEUED (not eaten) — it runs when
// the inflight completes.
func TestPaced_ResponseCreateMidSpeechQueuesAutoResponse(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("manual then auto"), serverVAD)

	// The user starts speaking; the client fires a manual response mid-speech.
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	if evs := s.Handle(ctx, &ClientEvent{Type: "response.create"}); firstEvent(evs, "response.created") == nil {
		t.Fatalf("mid-speech response.create = %v", typesOf(evs))
	}

	// The turn ends while that response is in flight.
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
	committed := firstEvent(evs, "input_audio_buffer.committed")
	if committed == nil {
		t.Fatalf("turn end = %v, want the commit ladder", typesOf(evs))
	}
	if committed["previous_item_id"] != nil {
		t.Errorf("commit forward-references an unannounced item: %v", committed["previous_item_id"])
	}
	if contains(typesOf(evs), "response.created") {
		t.Fatal("a second response must not stack while one is in flight")
	}

	// Draining completes the manual response AND then runs the queued
	// auto-response for the committed turn.
	rest := drain(t, s, fc, 300)
	dones := 0
	for _, ev := range rest {
		if ev["type"] == "response.done" {
			dones++
		}
	}
	if dones != 2 {
		t.Fatalf("drained response.done count = %d, want 2 (inflight + queued auto-response); got %v", dones, typesOf(rest))
	}
}

// T-F5: cancellation close-out honors the delta-concatenation invariant — the
// .done events, the stored item, and usage carry exactly the deltas the client
// received, never the full never-streamed transcript.
func TestPaced_CancelCloseOutCarriesEmittedPrefix(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("alpha beta gamma delta"), serverVAD)
	endVADTurn(t, s)

	// One event per tick: output_item.added, conversation.item.added,
	// content_part.added, transcript.delta("alpha "), audio.delta.
	var streamed string
	for range 5 {
		for _, ev := range s.Tick(ctx, fc.advance(10*time.Millisecond)) {
			if ev["type"] == "response.output_audio_transcript.delta" {
				streamed += ev["delta"].(string)
			}
		}
	}
	if streamed != "alpha " {
		t.Fatalf("setup: streamed %q, want %q", streamed, "alpha ")
	}

	evs := s.Handle(ctx, &ClientEvent{Type: "response.cancel"})
	if td := firstEvent(evs, "response.output_audio_transcript.done"); td == nil || td["transcript"] != streamed {
		t.Errorf("transcript.done = %v, want the emitted prefix %q", td, streamed)
	}
	if pd := firstEvent(evs, "response.content_part.done"); pd == nil || pd["part"].(map[string]any)["transcript"] != streamed {
		t.Errorf("content_part.done = %v, want part transcript %q", pd, streamed)
	}
	item := firstEvent(evs, "response.output_item.done")["item"].(map[string]any)
	if got := item["content"].([]any)[0].(map[string]any)["transcript"]; got != streamed {
		t.Errorf("item transcript = %v, want %q", got, streamed)
	}
	usage := firstEvent(evs, "response.done")["response"].(map[string]any)["usage"].(map[string]any)
	if usage["output_tokens"] != 1 {
		t.Errorf("usage output_tokens = %v, want 1 (only %q streamed)", usage["output_tokens"], streamed)
	}
}

// T-F5: a content part whose content_part.added never emitted must not be
// fabricated at close-out; the announced item still closes with empty content,
// and a head-cancel bills zero output tokens.
func TestPaced_CancelBeforePartOpenSkipsPartEvents(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("never streamed"), serverVAD)
	endVADTurn(t, s)

	// Announce the item (output_item.added, conversation.item.added) but stop
	// before content_part.added.
	s.Tick(ctx, fc.advance(10*time.Millisecond))
	s.Tick(ctx, fc.advance(10*time.Millisecond))

	evs := s.Handle(ctx, &ClientEvent{Type: "response.cancel"})
	for _, typ := range []string{"response.content_part.added", "response.content_part.done",
		"response.output_audio_transcript.done", "response.output_audio.done"} {
		if firstEvent(evs, typ) != nil {
			t.Errorf("fabricated %s for a part that never opened; got %v", typ, typesOf(evs))
		}
	}
	itemDone := firstEvent(evs, "response.output_item.done")
	if itemDone == nil {
		t.Fatalf("the announced item must still close out; got %v", typesOf(evs))
	}
	item := itemDone["item"].(map[string]any)
	if item["status"] != "incomplete" {
		t.Errorf("item status = %v, want incomplete", item["status"])
	}
	if content := item["content"].([]any); len(content) != 0 {
		t.Errorf("item content = %v, want empty (nothing streamed)", content)
	}
	usage := firstEvent(evs, "response.done")["response"].(map[string]any)["usage"].(map[string]any)
	if usage["output_tokens"] != 0 {
		t.Errorf("usage output_tokens = %v, want 0 (nothing streamed)", usage["output_tokens"])
	}
}

// R4-2: an out-of-band completion is not the end of the user's turn — it must
// not arm the idle timeout; and cancellation clears any idle deadline.
func TestIdleTimer_OOBAndCancelHygiene(t *testing.T) {
	ctx := context.Background()
	idleVAD := `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`

	s := NewSession("soi", "", fakeGen("side"))
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, idleVAD)
	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none"}`)})
	if dl, ok := s.NextDeadline(); ok {
		t.Errorf("OOB completion armed the idle timeout (deadline %v)", dl)
	}

	s2, fc2 := pacedSession(t, fakeGen("cancel me"), idleVAD)
	endVADTurn(t, s2)
	s2.Tick(ctx, fc2.advance(10*time.Millisecond))
	s2.Handle(ctx, &ClientEvent{Type: "response.cancel"})
	if dl, ok := s2.NextDeadline(); ok {
		t.Errorf("deadline %v survived response.cancel (would fire a spurious [silence] idle turn)", dl)
	}
}

// T-F6: an out-of-band response's items belong to no conversation — they are
// listed in ITS response.done output but must not be retrievable or anchor the
// conversation chain.
func TestOutOfBandItemsNotRetrievable(t *testing.T) {
	ctx := context.Background()
	s := NewSession("so", "", fakeGen("side answer"))
	s.Handle(ctx, mkUserItem("item_u", "main topic"))

	evs := s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none"}`)})
	output := firstEvent(evs, "response.done")["response"].(map[string]any)["output"].([]any)
	if len(output) == 0 {
		t.Fatal("OOB response must still list its output items")
	}
	oobID := output[0].(map[string]any)["id"].(string)

	got := s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve", ItemID: oobID})
	if got[0]["type"] != "error" || got[0]["error"].(map[string]any)["code"] != "item_not_found" {
		t.Errorf("OOB item retrieve = %v, want item_not_found (it joined no conversation)", got[0])
	}
	// The chain tail is untouched by the OOB response.
	added := firstEvent(s.Handle(ctx, mkUserItem("item_v", "next")), "conversation.item.added")
	if added["previous_item_id"] != "item_u" {
		t.Errorf("after OOB response, previous_item_id = %v, want item_u", added["previous_item_id"])
	}
}

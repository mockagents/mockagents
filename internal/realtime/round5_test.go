// Round-5 fidelity regression tests (2026-07-02 eval): phantom items on early
// cancel, chain repair on delete, out-of-band item scoping, session.update
// mid-speech, voice locking, and assorted wire-shape corrections.
package realtime

import (
	"context"
	"testing"
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

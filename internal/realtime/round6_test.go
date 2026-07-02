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

// R6-4 (S2): response.create `input` replaces the default conversation as the
// model context for that response — the documented out-of-band classification
// pattern. An explicit [] clears the context; item_reference entries resolve
// stored items; the conversation itself is untouched.
func TestResponseCreateInputCustomContext(t *testing.T) {
	ctx := context.Background()
	var seen []engine.RequestMessage
	gen := func(_ context.Context, _, _ string, history []engine.RequestMessage) (*engine.Response, error) {
		seen = history
		return &engine.Response{Content: "ok"}, nil
	}
	s := NewSession("r64", "", gen)
	s.Handle(ctx, mkUserItem("item_main", "main topic"))

	// Custom context: the supplied items are the WHOLE context.
	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(
		`{"conversation":"none","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"classify this"}]}]}`)})
	if len(seen) != 1 || seen[0].Content != "classify this" {
		t.Errorf("custom-context engine history = %+v, want exactly the supplied item", seen)
	}

	// Explicit [] clears the context entirely.
	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none","input":[]}`)})
	if len(seen) != 0 {
		t.Errorf("input:[] engine history = %+v, want empty", seen)
	}

	// item_reference resolves a stored item into the context.
	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(
		`{"conversation":"none","input":[{"type":"item_reference","id":"item_main"}]}`)})
	if len(seen) != 1 || seen[0].Content != "main topic" {
		t.Errorf("item_reference engine history = %+v, want the referenced turn", seen)
	}

	// Absent input still uses the conversation.
	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none"}`)})
	if len(seen) == 0 || seen[len(seen)-1].Content != "main topic" {
		t.Errorf("default engine history = %+v, want the conversation", seen)
	}
}

// R6-5 (S2): session.type "transcription" is the other arm of the GA session
// union: a transcription-shaped session object, and NO responses — a detected
// turn produces the commit + transcription ladder only.
func TestTranscriptionSession(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r65", "", fakeGen("must never be spoken"))
	fc := newFakeClock()
	s.SetClock(fc.now)

	evs := s.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(
		`{"type":"transcription","audio":{"input":{"transcription":{"model":"gpt-4o-transcribe"},"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)})
	if evs[0]["type"] != "session.updated" {
		t.Fatalf("transcription session.update = %v", typesOf(evs))
	}
	sess := evs[0]["session"].(map[string]any)
	if sess["type"] != "transcription" || sess["object"] != "realtime.transcription_session" {
		t.Errorf("session type/object = %v/%v, want transcription/realtime.transcription_session", sess["type"], sess["object"])
	}
	for _, absent := range []string{"output_modalities", "tools", "voice", "instructions", "max_output_tokens"} {
		if _, ok := sess[absent]; ok {
			t.Errorf("transcription session must not carry %q", absent)
		}
	}
	if _, ok := sess["audio"].(map[string]any)["output"]; ok {
		t.Error("transcription session must not carry audio.output")
	}

	// A detected turn transcribes but NEVER responds; the idle timer never arms.
	evs = endVADTurn(t, s)
	tps := typesOf(evs)
	if !contains(tps, "input_audio_buffer.committed") ||
		!contains(tps, "conversation.item.input_audio_transcription.completed") {
		t.Fatalf("transcription turn = %v, want the commit + transcription ladder", tps)
	}
	for _, banned := range []string{"response.created", "response.done", "rate_limits.updated"} {
		if contains(tps, banned) {
			t.Errorf("transcription session emitted %s — it must never respond; got %v", banned, tps)
		}
	}
	if dl, ok := s.NextDeadline(); ok {
		t.Errorf("idle deadline %v armed on a transcription session (would self-prompt)", dl)
	}
}

// R6-11/R6-12 (S3): voice-lock scoping — out-of-band audio does not lock the
// conversation's voice, and a locked conversation rejects a per-response
// voice override the same way it rejects a session.update.
func TestVoiceLockScoping(t *testing.T) {
	ctx := context.Background()

	// OOB audio does NOT lock (it joins no conversation).
	s := NewSession("r611", "", fakeGen("side audio"))
	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none"}`)})
	if evs := s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"audio":{"output":{"voice":"cedar"}}}`)}); evs[0]["type"] != "session.updated" {
		t.Errorf("OOB audio locked the voice: %v", typesOf(evs))
	}

	// In-conversation audio DOES lock, and the lock covers response.create
	// overrides (a per-response voice change is two voices in one conversation).
	s2 := NewSession("r612", "", fakeGen("spoken"))
	s2.Handle(ctx, &ClientEvent{Type: "response.create"})
	evs := s2.Handle(ctx, &ClientEvent{Type: "response.create",
		Response: []byte(`{"audio":{"output":{"voice":"cedar"}}}`)})
	if evs[0]["type"] != "error" || evs[0]["error"].(map[string]any)["code"] != "cannot_update_voice" {
		t.Errorf("per-response voice override bypassed the lock: %v", evs[0])
	}
	// An OOB override on the same locked session is still fine.
	evs = s2.Handle(ctx, &ClientEvent{Type: "response.create",
		Response: []byte(`{"conversation":"none","audio":{"output":{"voice":"cedar"}}}`)})
	if firstEvent(evs, "response.done") == nil {
		t.Errorf("OOB voice override rejected: %v", typesOf(evs))
	}
}

// R6-13 (S3): a still-streaming assistant audio item (announced, content not
// yet streamed) is a model-output-audio message — truncate must not reject it
// with "Only model output audio messages can be truncated".
func TestTruncateInProgressAudioItem(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("long spoken answer"), serverVAD)
	endVADTurn(t, s)

	// Announce the item (output_item.added + conversation.item.added).
	var msgID string
	for range 2 {
		for _, ev := range s.Tick(ctx, fc.advance(10*time.Millisecond)) {
			if ev["type"] == "conversation.item.added" {
				msgID = ev["item"].(map[string]any)["id"].(string)
			}
		}
	}
	if msgID == "" {
		t.Fatal("setup: item not announced")
	}
	evs := s.Handle(ctx, &ClientEvent{Type: "conversation.item.truncate",
		ItemID: msgID, ContentIndex: 0, AudioEndMs: 50})
	if evs[0]["type"] != "conversation.item.truncated" {
		t.Errorf("truncate of in-progress audio item = %v, want conversation.item.truncated", evs[0])
	}
}

// R6-15 (S3): the gpt-4o-transcribe* family reports TOKEN usage on
// transcription.completed; duration stays whisper-1's shape.
func TestTranscriptionUsageVariant(t *testing.T) {
	ctx := context.Background()
	usageFor := func(model string) map[string]any {
		s := NewSession("r615", "", fakeGen("ok"))
		s.Handle(ctx, &ClientEvent{Type: "session.update",
			Session: []byte(`{"audio":{"input":{"transcription":{"model":"` + model + `"}}}}`)})
		s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(500, speechAmp)})
		evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})
		done := firstEvent(evs, "conversation.item.input_audio_transcription.completed")
		if done == nil {
			t.Fatalf("no transcription.completed for %s: %v", model, typesOf(evs))
		}
		return done["usage"].(map[string]any)
	}
	if u := usageFor("whisper-1"); u["type"] != "duration" {
		t.Errorf("whisper-1 usage = %v, want the duration variant", u)
	}
	u := usageFor("gpt-4o-transcribe")
	if u["type"] != "tokens" {
		t.Fatalf("gpt-4o-transcribe usage = %v, want the tokens variant", u)
	}
	if u["total_tokens"] != u["input_tokens"].(int)+u["output_tokens"].(int) {
		t.Errorf("token usage does not add up: %v", u)
	}
}

// R6-16 (S3): retrieve returns the committed user audio (its documented
// purpose); the item .added/.done events keep excluding it.
func TestRetrieveReturnsCommittedAudio(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r616", "", fakeGen("ok"))
	chunk := pcmChunk(100, speechAmp)
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: chunk})
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})

	added := firstEvent(evs, "conversation.item.added")
	itemID := added["item"].(map[string]any)["id"].(string)
	if _, has := added["item"].(map[string]any)["content"].([]any)[0].(map[string]any)["audio"]; has {
		t.Error("item events must exclude audio data")
	}

	got := firstEvent(s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve", ItemID: itemID}), "conversation.item.retrieved")
	audio, _ := got["item"].(map[string]any)["content"].([]any)[0].(map[string]any)["audio"].(string)
	if audio != chunk {
		t.Errorf("retrieved audio length %d, want the appended payload (%d chars)", len(audio), len(chunk))
	}
}

// R6-17 (S3): session.include item.input_audio_transcription.logprobs attaches
// logprobs to the transcription delta and completed events.
func TestTranscriptionLogprobsInclude(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r617", "", fakeGen("ok"))
	s.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(
		`{"include":["item.input_audio_transcription.logprobs"],"audio":{"input":{"transcription":{"model":"gpt-4o-transcribe"}}}}`)})
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(100, speechAmp)})
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})

	delta := firstEvent(evs, "conversation.item.input_audio_transcription.delta")
	lps, ok := delta["logprobs"].([]any)
	if !ok || len(lps) == 0 {
		t.Fatalf("delta missing logprobs: %v", delta)
	}
	lp := lps[0].(map[string]any)
	if lp["token"] == "" || lp["logprob"] == nil {
		t.Errorf("logprob entry = %v, want token+logprob", lp)
	}
	done := firstEvent(evs, "conversation.item.input_audio_transcription.completed")
	if _, ok := done["logprobs"].([]any); !ok {
		t.Errorf("completed missing logprobs: %v", done)
	}
	// Without the include flag, no logprobs appear.
	s2 := NewSession("r617b", "", fakeGen("ok"))
	s2.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(
		`{"audio":{"input":{"transcription":{"model":"gpt-4o-transcribe"}}}}`)})
	s2.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(100, speechAmp)})
	evs = s2.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})
	if _, ok := firstEvent(evs, "conversation.item.input_audio_transcription.delta")["logprobs"]; ok {
		t.Error("logprobs emitted without the include opt-in")
	}
}

// R6-18 (S3): an input_audio content part's transcript is matchable text —
// conversation-restore flows re-create prior audio turns that way.
func TestInputAudioTranscriptJoinsHistory(t *testing.T) {
	ctx := context.Background()
	var lastUser string
	gen := func(_ context.Context, _, _ string, history []engine.RequestMessage) (*engine.Response, error) {
		for _, m := range history {
			if m.Role == "user" {
				lastUser = m.Content
			}
		}
		return &engine.Response{Content: "ok"}, nil
	}
	s := NewSession("r618", "", gen)
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.create", Item: []byte(
		`{"type":"message","role":"user","content":[{"type":"input_audio","transcript":"restored audio turn"}]}`)})
	s.Handle(ctx, &ClientEvent{Type: "response.create"})
	if lastUser != "restored audio turn" {
		t.Errorf("engine saw last user turn %q, want the input_audio transcript", lastUser)
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

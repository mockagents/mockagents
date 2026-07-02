// Round-7 fidelity regression tests (2026-07-02 eval): transcription-session
// response gating, input:null semantics, idle-disarm predicate, GA VAD
// defaults, commit minimum, ephemeral-key seeding, and the error-shape batch.
package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// R7-1 (S2, three-lens convergence): a transcription session has no response
// generation — a manual response.create must be rejected, not answered with a
// full assistant audio ladder.
func TestTranscriptionSessionRejectsResponseCreate(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r71", "", fakeGen("must never be spoken"))
	s.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(`{"type":"transcription"}`)})

	evs := s.Handle(ctx, &ClientEvent{Type: "response.create"})
	if evs[0]["type"] != "error" {
		t.Fatalf("response.create on a transcription session = %v, want error", typesOf(evs))
	}
	errObj := evs[0]["error"].(map[string]any)
	if errObj["type"] != "invalid_request_error" || errObj["code"] != nil {
		t.Errorf("error = %v, want invalid_request_error with code null", errObj)
	}
	if contains(typesOf(evs), "response.created") {
		t.Error("a transcription session must never open a response ladder")
	}
}

// R7-2 (S2): `input: null` (an SDK's unset Optional) means ABSENT — it must
// not clear the context; only an explicit [] does. A malformed non-array is
// treated as absent too.
func TestResponseCreateInputNullMeansAbsent(t *testing.T) {
	ctx := context.Background()
	var seen int
	gen := func(_ context.Context, _, _ string, history []engine.RequestMessage) (*engine.Response, error) {
		seen = len(history)
		return &engine.Response{Content: "ok"}, nil
	}
	s := NewSession("r72", "", gen)
	s.Handle(ctx, mkUserItem("item_ctx", "main topic"))

	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none","input":null}`)})
	if seen == 0 {
		t.Error("input:null cleared the context — it must mean absent")
	}
	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none","input":"garbage"}`)})
	if seen == 0 {
		t.Error("malformed input cleared the context — it must be treated as absent")
	}
	// The explicit empty array still clears.
	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none","input":[]}`)})
	if seen != 0 {
		t.Errorf("input:[] context size = %d, want 0", seen)
	}
}

// R7-11/R7-12/R7-13 (S3): dangling input references error, response-generated
// function calls join history, and the session type is immutable once set.
func TestInputRefsHistoryAndSessionType(t *testing.T) {
	ctx := context.Background()

	// R7-11: a dangling item_reference fails the create — no response opens.
	s := NewSession("r711", "", fakeGen("ok"))
	evs := s.Handle(ctx, &ClientEvent{Type: "response.create",
		Response: []byte(`{"conversation":"none","input":[{"type":"item_reference","id":"item_ghost"}]}`)})
	e := evs[0]["error"].(map[string]any)
	if evs[0]["type"] != "error" || e["code"] != "item_not_found" || e["param"] != "response.input" {
		t.Errorf("dangling reference = %v, want item_not_found on response.input", evs[0])
	}
	if contains(typesOf(evs), "response.created") {
		t.Error("a rejected input must not open a response")
	}

	// R7-12: the response's function_call turn joins engine history (mirrors
	// the conversation.item.create {type:"function_call"} mapping).
	s2 := NewSession("r712", "", fakeGenTool("checking", types.ToolCallSpec{Name: "lookup"}))
	s2.Handle(ctx, mkUserItem("item_q", "question"))
	s2.Handle(ctx, &ClientEvent{Type: "response.create"})
	assistant := 0
	for _, m := range s2.history {
		if m.Role == "assistant" {
			assistant++
		}
	}
	if assistant != 2 {
		t.Errorf("assistant history entries = %d, want 2 (message + function_call)", assistant)
	}

	// R7-13: the session type is immutable once set.
	s3 := NewSession("r713", "", fakeGen("ok"))
	s3.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(`{"type":"transcription"}`)})
	evs = s3.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(`{"type":"realtime"}`)})
	e = evs[0]["error"].(map[string]any)
	if evs[0]["type"] != "error" || e["param"] != "session.type" {
		t.Errorf("type flip = %v, want invalid_value on session.type", evs[0])
	}
	if !s3.isTranscription() {
		t.Error("a rejected type flip must leave the session type unchanged")
	}
	// Re-sending the SAME type stays accepted.
	if evs := s3.Handle(ctx, &ClientEvent{Type: "session.update", Session: []byte(`{"type":"transcription"}`)}); evs[0]["type"] != "session.updated" {
		t.Errorf("same-type update = %v, want session.updated", typesOf(evs))
	}
}

// R7-15/R7-18/R7-19 (S3): error-shape batch — missing type is invalid_event
// (not unknown_event), message role/part-type combinations are validated, and
// item_not_found names its param.
func TestErrorShapeBatch(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r715", "", fakeGen("ok"))

	// Missing type → invalid_event; unrecognized type → unknown_event.
	if e := s.Handle(ctx, &ClientEvent{})[0]["error"].(map[string]any); e["code"] != "invalid_event" {
		t.Errorf("missing type code = %v, want invalid_event", e["code"])
	}
	if e := s.Handle(ctx, &ClientEvent{Type: "bogus.event"})[0]["error"].(map[string]any); e["code"] != "unknown_event" {
		t.Errorf("unrecognized type code = %v, want unknown_event", e["code"])
	}

	// Invalid role → invalid_value on item.role.
	evs := s.Handle(ctx, &ClientEvent{Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"banana","content":[{"type":"input_text","text":"x"}]}`)})
	if e := evs[0]["error"].(map[string]any); evs[0]["type"] != "error" || e["param"] != "item.role" {
		t.Errorf("invalid role = %v, want invalid_value on item.role", evs[0])
	}
	// Cross-role content part → invalid_value naming the part path.
	evs = s.Handle(ctx, &ClientEvent{Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"output_audio","transcript":"x"}]}`)})
	if e := evs[0]["error"].(map[string]any); evs[0]["type"] != "error" || e["param"] != "item.content[0].type" {
		t.Errorf("cross-role part = %v, want invalid_value on item.content[0].type", evs[0])
	}

	// item_not_found carries param item_id on retrieve/delete/truncate.
	for _, typ := range []string{"conversation.item.retrieve", "conversation.item.delete", "conversation.item.truncate"} {
		evs := s.Handle(ctx, &ClientEvent{Type: typ, ItemID: "item_ghost"})
		e := evs[0]["error"].(map[string]any)
		if e["code"] != "item_not_found" || e["param"] != "item_id" {
			t.Errorf("%s unknown id = %v, want item_not_found with param item_id", typ, e)
		}
	}
}

// R7-7 (S3): the committed turn's audio window is [speech_start −
// prefix_padding_ms, end] — leading silence buffered before the turn must not
// end up in the stored audio or the billed duration.
func TestStoredAudioExcludesLeadingSilence(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r77", "", fakeGen("ok"))
	// Default server_vad (prefix 300ms). 1000ms silence, 200ms speech, 600ms
	// decision silence → window = 300 + 200 + 600 = 1100ms.
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(1000, quietAmp)})
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
	committed := firstEvent(evs, "input_audio_buffer.committed")
	if committed == nil {
		t.Fatalf("turn did not commit: %v", typesOf(evs))
	}

	got := firstEvent(s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve",
		ItemID: committed["item_id"].(string)}), "conversation.item.retrieved")
	audio, _ := got["item"].(map[string]any)["content"].([]any)[0].(map[string]any)["audio"].(string)
	raw, err := base64.StdEncoding.DecodeString(audio)
	if err != nil {
		t.Fatalf("stored audio not base64: %v", err)
	}
	if wantBytes := 1100 * 48; len(raw) != wantBytes {
		t.Errorf("stored audio = %dms, want 1100ms (prefix+speech+decision window)", len(raw)/48)
	}
}

// R7-8 (S3): truncating the item the response is still streaming CUTS the
// stream — no further deltas, close-outs/retrieve/usage/history all carry the
// emitted prefix, not the full transcript.
func TestTruncateInflightItemCutsTheStream(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("alpha beta gamma delta"), serverVAD)
	endVADTurn(t, s)

	// Emit through the first transcript delta ("alpha ").
	var msgID, streamed string
	for range 4 {
		for _, ev := range s.Tick(ctx, fc.advance(10*time.Millisecond)) {
			switch ev["type"] {
			case "conversation.item.added":
				msgID = ev["item"].(map[string]any)["id"].(string)
			case "response.output_audio_transcript.delta":
				streamed += ev["delta"].(string)
			}
		}
	}
	if msgID == "" || streamed != "alpha " {
		t.Fatalf("setup: msgID=%q streamed=%q", msgID, streamed)
	}

	if evs := s.Handle(ctx, &ClientEvent{Type: "conversation.item.truncate",
		ItemID: msgID, ContentIndex: 0, AudioEndMs: 20}); evs[0]["type"] != "conversation.item.truncated" {
		t.Fatalf("truncate = %v", typesOf(evs))
	}

	rest := drain(t, s, fc, 100)
	for _, ev := range rest {
		if ev["type"] == "response.output_audio_transcript.delta" {
			t.Fatalf("delta streamed past the truncation: %v", ev)
		}
	}
	if td := firstEvent(rest, "response.output_audio_transcript.done"); td == nil || td["transcript"] != streamed {
		t.Errorf("transcript.done = %v, want the emitted prefix %q", td, streamed)
	}
	usage := firstEvent(rest, "response.done")["response"].(map[string]any)["usage"].(map[string]any)
	if usage["output_tokens"] != 1 {
		t.Errorf("usage output_tokens = %v, want 1 (truncated to %q)", usage["output_tokens"], streamed)
	}
	got := firstEvent(s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve", ItemID: msgID}), "conversation.item.retrieved")
	if tr := got["item"].(map[string]any)["content"].([]any)[0].(map[string]any)["transcript"]; tr != streamed {
		t.Errorf("retrieved transcript = %v, want %q", tr, streamed)
	}
	for _, m := range s.history {
		if m.Role == "assistant" && m.Content != streamed {
			t.Errorf("history got %q, want the truncated %q", m.Content, streamed)
		}
	}
}

// R7-9 (S3): an out-of-band side-response is not the user's turn — it must
// not DISARM an armed idle timeout (round 5 pinned only that it must not arm
// one).
func TestOOBDoesNotDisarmIdle(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r79", "", fakeGen("ok"))
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)
	endVADTurn(t, s)
	if _, ok := s.NextDeadline(); !ok {
		t.Fatal("setup: idle deadline should be armed")
	}

	s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none"}`)})
	if _, ok := s.NextDeadline(); !ok {
		t.Error("an OOB response disarmed the armed idle timeout")
	}
}

// R7-10 (S3): a deleted (tombstoned) in-flight item gets no conversation
// mirror close-out — the client was told it is deleted; only the
// response-scoped output_item.done still emits.
func TestDeletedInflightItemGetsNoMirrorDone(t *testing.T) {
	ctx := context.Background()
	s, fc := pacedSession(t, fakeGen("short reply"), serverVAD)
	endVADTurn(t, s)

	var msgID string
	for range 3 {
		for _, ev := range s.Tick(ctx, fc.advance(10*time.Millisecond)) {
			if ev["type"] == "conversation.item.added" {
				msgID = ev["item"].(map[string]any)["id"].(string)
			}
		}
	}
	if msgID == "" {
		t.Fatal("setup: item not announced")
	}
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: msgID})

	rest := drain(t, s, fc, 100)
	if firstEvent(rest, "conversation.item.done") != nil {
		t.Errorf("deleted item received a conversation mirror .done: %v", typesOf(rest))
	}
	if firstEvent(rest, "response.output_item.done") == nil {
		t.Errorf("the response-scoped output_item.done must still emit: %v", typesOf(rest))
	}

	// Same via the CANCEL drain.
	s2, fc2 := pacedSession(t, fakeGen("short reply"), serverVAD)
	endVADTurn(t, s2)
	msgID = ""
	for range 3 {
		for _, ev := range s2.Tick(ctx, fc2.advance(10*time.Millisecond)) {
			if ev["type"] == "conversation.item.added" {
				msgID = ev["item"].(map[string]any)["id"].(string)
			}
		}
	}
	s2.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: msgID})
	cancel := s2.Handle(ctx, &ClientEvent{Type: "response.cancel"})
	if firstEvent(cancel, "conversation.item.done") != nil {
		t.Errorf("cancel drain emitted a mirror .done for the deleted item: %v", typesOf(cancel))
	}
}

// R7-4 (S2): server VAD is the GA DEFAULT — a fresh session detects turns
// without any session.update, session.created reports the server_vad object
// (not null), and turn_detection:null is the explicit opt-out.
func TestServerVADOnByDefault(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r74", "gpt-realtime", fakeGen("default detected"))

	td := s.Greeting()[0]["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)["turn_detection"]
	raw, _ := td.(json.RawMessage)
	if !strings.Contains(string(raw), `"server_vad"`) {
		t.Errorf("default session turn_detection = %s, want the GA server_vad object", raw)
	}

	// Mic audio produces a detected turn with NO prior session.update.
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	if firstEvent(evs, "input_audio_buffer.speech_started") == nil {
		t.Fatalf("default session did not detect speech: %v", typesOf(evs))
	}
	evs = s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
	if firstEvent(evs, "input_audio_buffer.committed") == nil || firstEvent(evs, "response.done") == nil {
		t.Errorf("default session turn-end = %v, want commit + auto-response", typesOf(evs))
	}

	// null is the explicit opt-out.
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":null}}}`)
	if evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)}); len(evs) != 0 {
		t.Errorf("append after opt-out emitted %v, want nothing", typesOf(evs))
	}
}

// R7-3 (S2): a pending idle deadline must be disarmed by ANY reconfiguration
// that removes the idle timeout — not just turn_detection:null. A stale
// deadline fired phantom [silence] turns under semantic_vad or a server_vad
// config without idle_timeout_ms.
func TestIdleDisarmedWhenNoLongerConfigured(t *testing.T) {
	ctx := context.Background()
	arm := func(name string) *Session {
		s := NewSession(name, "", fakeGen("ok"))
		fc := newFakeClock()
		s.SetClock(fc.now)
		enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)
		endVADTurn(t, s)
		if _, ok := s.NextDeadline(); !ok {
			t.Fatal("setup: idle deadline should be armed")
		}
		return s
	}

	// Switching detector types drops the idle timeout (semantic_vad has none).
	s := arm("r73a")
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"semantic_vad"}}}}`)
	if dl, ok := s.NextDeadline(); ok {
		t.Errorf("idle deadline %v survived the switch to semantic_vad", dl)
	}

	// Re-sending server_vad WITHOUT idle_timeout_ms turns the feature off.
	s = arm("r73b")
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad"}}}}`)
	if dl, ok := s.NextDeadline(); ok {
		t.Errorf("idle deadline %v survived removing idle_timeout_ms", dl)
	}
	if evs := s.Tick(ctx, time.Unix(99999, 0)); len(evs) != 0 {
		t.Errorf("stale idle fired %v under a config without idle_timeout_ms", typesOf(evs))
	}

	// Still configured → the armed deadline survives a same-config tweak.
	s = arm("r73c")
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000,"threshold":0.6}}}}`)
	if _, ok := s.NextDeadline(); !ok {
		t.Error("idle deadline dropped although idle_timeout_ms is still configured")
	}
}

// Round-7 fidelity regression tests (2026-07-02 eval): transcription-session
// response gating, input:null semantics, idle-disarm predicate, GA VAD
// defaults, commit minimum, ephemeral-key seeding, and the error-shape batch.
package realtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
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

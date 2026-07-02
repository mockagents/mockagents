package realtime

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

// pcmChunk builds ms milliseconds of constant-amplitude PCM16LE @ 24 kHz,
// base64-encoded. amplitude 0 = silence; ≥ 16384 crosses the default 0.5
// energy threshold.
func pcmChunk(ms int, amplitude int16) string {
	samples := ms * 24
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(amplitude))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

const (
	speechAmp  = 20000 // energy ≈ 0.61
	quietAmp   = 0
	serverVAD  = `{"audio":{"input":{"turn_detection":{"type":"server_vad"}}}}`
	vadOffJSON = `{"audio":{"input":{"turn_detection":null}}}`
)

func enableVAD(t *testing.T, s *Session, sessionJSON string) {
	t.Helper()
	evs := s.Handle(context.Background(), &ClientEvent{Type: "session.update", Session: []byte(sessionJSON)})
	if evs[0]["type"] != "session.updated" {
		t.Fatalf("session.update events = %v", typesOf(evs))
	}
}

// The canonical VAD voice loop: speech → speech_started; enough silence →
// speech_stopped + auto-commit (same ladder as a manual commit, with the
// pre-announced item id) + auto-response.
func TestVAD_FullTurn(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv", "gpt-realtime", fakeGen("Hi there!"))
	enableVAD(t, s, serverVAD)

	// 400 ms of speech → speech_started, audio_start_ms clamped to 0 (the
	// default 300 ms prefix padding reaches before the session's first audio).
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(400, speechAmp)})
	started := firstEvent(evs, "input_audio_buffer.speech_started")
	if started == nil {
		t.Fatalf("speech append events = %v, want speech_started", typesOf(evs))
	}
	if started["audio_start_ms"] != 0 {
		t.Errorf("audio_start_ms = %v, want 0 (clamped)", started["audio_start_ms"])
	}
	itemID, _ := started["item_id"].(string)
	if itemID == "" {
		t.Fatal("speech_started must pre-announce the item id")
	}

	// 300 ms of silence: under the 500 ms window — nothing yet.
	if evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(300, quietAmp)}); len(evs) != 0 {
		t.Fatalf("sub-window silence emitted %v", typesOf(evs))
	}
	// 300 more ms (600 total ≥ 500): the turn ends.
	evs = s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(300, quietAmp)})
	tps := typesOf(evs)

	stopped := firstEvent(evs, "input_audio_buffer.speech_stopped")
	if stopped == nil {
		t.Fatalf("turn-end events = %v, want speech_stopped first", tps)
	}
	// audio_end_ms is the stop-decision point and INCLUDES the silence window
	// (400ms speech + 600ms silence when the 500ms window tripped) per GA.
	if stopped["audio_end_ms"] != 1000 {
		t.Errorf("audio_end_ms = %v, want 1000 (includes min_silence_duration_ms)", stopped["audio_end_ms"])
	}
	if stopped["item_id"] != itemID {
		t.Errorf("speech_stopped item_id = %v, want %q", stopped["item_id"], itemID)
	}

	// Auto-commit ladder with the pre-announced id, then the auto-response.
	committed := firstEvent(evs, "input_audio_buffer.committed")
	if committed == nil || committed["item_id"] != itemID {
		t.Errorf("committed = %v, want item_id %q", committed, itemID)
	}
	added := firstEvent(evs, "conversation.item.added")
	if added == nil || added["item"].(map[string]any)["id"] != itemID {
		t.Errorf("conversation.item.added must carry the pre-announced item")
	}
	for _, want := range []string{"response.created", "response.done"} {
		if !contains(tps, want) {
			t.Errorf("auto-response missing %q; got %v", want, tps)
		}
	}
}

// create_response:false ends the turn with the commit ladder but no response.
func TestVAD_CreateResponseFalse(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv2", "", fakeGen("ok"))
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","create_response":false}}}}`)

	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
	tps := typesOf(evs)
	if !contains(tps, "input_audio_buffer.committed") {
		t.Errorf("turn must auto-commit; got %v", tps)
	}
	if contains(tps, "response.created") {
		t.Errorf("create_response:false must not auto-respond; got %v", tps)
	}
}

// threshold and prefix_padding_ms are honored on the GA threshold scale
// (0..1 activation, mapped to mean-abs amplitude via vadAmplitudeScale so
// realistic mic levels work): quiet audio below a raised threshold never
// starts a turn, and audio_start_ms subtracts the padding.
func TestVAD_ThresholdAndPrefixPadding(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv3", "", fakeGen("ok"))
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","threshold":0.8,"prefix_padding_ms":100}}}}`)

	// Threshold 0.8 → effective mean-abs 0.08. Amplitude 1600 ≈ 0.049 — quiet
	// speech that a raised threshold must ignore.
	if evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(500, 1600)}); len(evs) != 0 {
		t.Fatalf("sub-threshold audio started a turn: %v", typesOf(evs))
	}
	// Loud speech (5000 ≈ 0.153 > 0.08) after 500 ms of prior audio → start at 500-100=400.
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, 5000)})
	started := firstEvent(evs, "input_audio_buffer.speech_started")
	if started == nil {
		t.Fatalf("loud audio did not start a turn: %v", typesOf(evs))
	}
	if started["audio_start_ms"] != 400 {
		t.Errorf("audio_start_ms = %v, want 400 (500ms in, minus 100ms padding)", started["audio_start_ms"])
	}
}

// Realistic microphone levels trigger the DEFAULT threshold (the GA 0.5 scale
// is not linear amplitude — real speech averages ~0.02–0.15 of full scale).
func TestVAD_RealisticMicLevels(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv8", "", fakeGen("ok"))
	enableVAD(t, s, serverVAD) // default threshold 0.5 → effective 0.05

	// −18 dBFS-ish speech: amplitude 2600 ≈ 0.079 mean-abs. Must trigger.
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, 2600)})
	if firstEvent(evs, "input_audio_buffer.speech_started") == nil {
		t.Fatalf("realistic speech level did not trigger the default threshold: %v", typesOf(evs))
	}
	// Room noise: amplitude 300 ≈ 0.009. Must count as silence and end the turn.
	evs = s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, 300)})
	if firstEvent(evs, "input_audio_buffer.speech_stopped") == nil {
		t.Errorf("room-noise level did not read as silence: %v", typesOf(evs))
	}
}

// idle_timeout_ms is server_vad-only (the GA SemanticVad config has no such
// field) — a semantic_vad session must not arm the idle deadline.
func TestIdleTimeout_ServerVADOnly(t *testing.T) {
	s := NewSession("sv9", "", fakeGen("ok"))
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"semantic_vad","idle_timeout_ms":5000}}}}`)

	ctx := context.Background()
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(4100, quietAmp)}) // ends the turn (auto window 4000)
	if dl, ok := s.NextDeadline(); ok {
		t.Errorf("semantic_vad armed an idle deadline (%v); idle_timeout_ms is server_vad-only", dl)
	}
}

// semantic_vad maps eagerness to the silence window (high → 2000 ms).
func TestVAD_SemanticEagerness(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv4", "", fakeGen("ok"))
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"semantic_vad","eagerness":"high"}}}}`)

	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	// 1900 ms silence: under the 2000 ms high-eagerness window.
	if evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(1900, quietAmp)}); len(evs) != 0 {
		t.Fatalf("silence under the eagerness window ended the turn: %v", typesOf(evs))
	}
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, quietAmp)})
	if firstEvent(evs, "input_audio_buffer.speech_stopped") == nil {
		t.Errorf("2100 ms of silence must end a high-eagerness turn; got %v", typesOf(evs))
	}
}

// A speech pause shorter than the window does not end the turn — and the
// silence counter resets when speech resumes.
func TestVAD_PauseResumesTurn(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv5", "", fakeGen("ok"))
	enableVAD(t, s, serverVAD)

	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(400, quietAmp)}) // pause < 500
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)}) // resume resets
	if evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(400, quietAmp)}); len(evs) != 0 {
		t.Fatalf("silence counter did not reset on resumed speech: %v", typesOf(evs))
	}
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, quietAmp)})
	if firstEvent(evs, "input_audio_buffer.speech_stopped") == nil {
		t.Errorf("600 ms of post-resume silence must end the turn; got %v", typesOf(evs))
	}
}

// Non-PCM payloads count as speech: a mock must not punish synthetic audio.
func TestVAD_NonPCMCountsAsSpeech(t *testing.T) {
	s := NewSession("sv6", "", fakeGen("ok"))
	enableVAD(t, s, serverVAD)
	evs := s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: "!!!not-base64!!!"})
	if firstEvent(evs, "input_audio_buffer.speech_started") == nil {
		t.Errorf("non-decodable audio should start a turn; got %v", typesOf(evs))
	}
}

// Disabling turn detection (null) restores manual behavior, and clear abandons
// an in-progress speech cycle.
func TestVAD_DisableAndClear(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv7", "", fakeGen("ok"))
	enableVAD(t, s, serverVAD)

	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.clear"})
	// After clear, fresh silence does nothing and fresh speech starts a NEW turn.
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	if firstEvent(evs, "input_audio_buffer.speech_started") == nil {
		t.Errorf("post-clear speech should start a new turn; got %v", typesOf(evs))
	}

	enableVAD(t, s, vadOffJSON)
	if evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)}); len(evs) != 0 {
		t.Errorf("with VAD off, append must be silent; got %v", typesOf(evs))
	}
}

// An invalid turn_detection config rejects the whole session.update with a GA
// error — code invalid_value, param naming the offending field — and the
// session config stays untouched.
func TestSessionUpdate_TurnDetectionValidation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, td, wantParam string
	}{
		{"unknown type", `{"type":"bogus_vad"}`, "session.audio.input.turn_detection.type"},
		{"missing type", `{"threshold":0.5}`, "session.audio.input.turn_detection.type"},
		{"threshold too high", `{"type":"server_vad","threshold":1.5}`, "session.audio.input.turn_detection.threshold"},
		{"threshold negative", `{"type":"server_vad","threshold":-0.1}`, "session.audio.input.turn_detection.threshold"},
		{"negative prefix", `{"type":"server_vad","prefix_padding_ms":-1}`, "session.audio.input.turn_detection.prefix_padding_ms"},
		{"negative silence", `{"type":"server_vad","silence_duration_ms":-1}`, "session.audio.input.turn_detection.silence_duration_ms"},
		{"negative idle", `{"type":"server_vad","idle_timeout_ms":-5}`, "session.audio.input.turn_detection.idle_timeout_ms"},
		{"bad eagerness", `{"type":"semantic_vad","eagerness":"frantic"}`, "session.audio.input.turn_detection.eagerness"},
		{"not an object", `"server_vad"`, "session.audio.input.turn_detection"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSession("svv", "", fakeGen("ok"))
			evs := s.Handle(ctx, &ClientEvent{Type: "session.update", EventID: "evt_v",
				Session: []byte(`{"audio":{"input":{"turn_detection":` + tc.td + `}}}`)})
			if len(evs) != 1 || evs[0]["type"] != "error" {
				t.Fatalf("events = %v, want one error", typesOf(evs))
			}
			e := evs[0]["error"].(map[string]any)
			if e["code"] != "invalid_value" {
				t.Errorf("code = %v, want invalid_value", e["code"])
			}
			if e["param"] != tc.wantParam {
				t.Errorf("param = %v, want %q", e["param"], tc.wantParam)
			}
			if e["event_id"] != "evt_v" {
				t.Errorf("event_id = %v, want the client event id", e["event_id"])
			}
			// The update was rejected wholesale: VAD stays off.
			if s.vad != nil {
				t.Error("rejected update must not enable VAD")
			}
		})
	}

	// A valid config (and an explicit null) still applies.
	s := NewSession("svv2", "", fakeGen("ok"))
	if evs := s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(serverVAD)}); evs[0]["type"] != "session.updated" {
		t.Fatalf("valid config rejected: %v", typesOf(evs))
	}
	if evs := s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(vadOffJSON)}); evs[0]["type"] != "session.updated" {
		t.Fatalf("explicit null rejected: %v", typesOf(evs))
	}
}

// With input transcription enabled, a commit streams the GA transcription
// ladder: delta chunks reassembling the transcript, then completed carrying the
// REQUIRED usage field (duration variant, from the decoded audio length).
func TestTranscription_DeltaAndUsage(t *testing.T) {
	ctx := context.Background()
	s := NewSession("str", "", fakeGen("ok"))
	s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"audio":{"input":{"transcription":{"model":"whisper-1"}}}}`)})

	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(480, speechAmp)})
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})

	var assembled string
	for _, e := range evs {
		if e["type"] == "conversation.item.input_audio_transcription.delta" {
			assembled += e["delta"].(string)
		}
	}
	if assembled == "" {
		t.Fatalf("no transcription deltas; events = %v", typesOf(evs))
	}
	completed := firstEvent(evs, "conversation.item.input_audio_transcription.completed")
	if completed == nil {
		t.Fatal("missing transcription completed event")
	}
	if strings.TrimSpace(assembled) != completed["transcript"] {
		t.Errorf("deltas %q do not reassemble the transcript %q", assembled, completed["transcript"])
	}
	usage, _ := completed["usage"].(map[string]any)
	if usage == nil || usage["type"] != "duration" {
		t.Fatalf("completed usage = %v, want the duration variant", completed["usage"])
	}
	if usage["seconds"] != 0.48 {
		t.Errorf("usage.seconds = %v, want 0.48 (480ms of committed audio)", usage["seconds"])
	}
}

// F17: the beta-flat aliases work — top-level turn_detection enables (and is
// validated like) the GA nested form, and beta format strings translate to GA
// format objects.
func TestSessionUpdate_BetaAliases(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sba", "", fakeGen("ok"))

	// Beta-flat turn_detection enables VAD.
	evs := s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"turn_detection":{"type":"server_vad"},"input_audio_format":"g711_ulaw"}`)})
	if evs[0]["type"] != "session.updated" {
		t.Fatalf("beta-flat update rejected: %v", typesOf(evs))
	}
	if got := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)}); firstEvent(got, "input_audio_buffer.speech_started") == nil {
		t.Errorf("beta-flat turn_detection did not enable VAD; got %v", typesOf(got))
	}
	// The beta format string was translated to the GA object.
	sess := evs[0]["session"].(map[string]any)
	format, _ := json.Marshal(sess["audio"].(map[string]any)["input"].(map[string]any)["format"])
	if string(format) != `{"type":"audio/pcmu"}` {
		t.Errorf("input format = %s, want the translated GA object", format)
	}

	// Beta-flat turn_detection is validated too.
	bad := s.Handle(ctx, &ClientEvent{Type: "session.update",
		Session: []byte(`{"turn_detection":{"type":"server_vad","threshold":2}}`)})
	if bad[0]["type"] != "error" || bad[0]["error"].(map[string]any)["code"] != "invalid_value" {
		t.Errorf("invalid beta-flat turn_detection accepted: %v", bad[0])
	}
}

func TestAudioEnergy(t *testing.T) {
	ms, energy := audioEnergy(pcmChunk(100, 0))
	if ms != 100 || energy != 0 {
		t.Errorf("silence chunk = (%v ms, %v), want (100, 0)", ms, energy)
	}
	ms, energy = audioEnergy(pcmChunk(50, 16384))
	if ms != 50 || energy < 0.49 || energy > 0.51 {
		t.Errorf("half-amplitude chunk = (%v ms, %v), want (50, ~0.5)", ms, energy)
	}
	if _, energy := audioEnergy("not base64 at all"); energy != 1.0 {
		t.Errorf("undecodable payload energy = %v, want 1.0", energy)
	}
}

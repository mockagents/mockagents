package realtime

import (
	"context"
	"encoding/base64"
	"encoding/binary"
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
	if stopped["audio_end_ms"] != 400 {
		t.Errorf("audio_end_ms = %v, want 400 (speech ended where silence began)", stopped["audio_end_ms"])
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

// threshold and prefix_padding_ms are honored: quiet speech below a raised
// threshold never starts a turn, and audio_start_ms subtracts the padding.
func TestVAD_ThresholdAndPrefixPadding(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sv3", "", fakeGen("ok"))
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","threshold":0.8,"prefix_padding_ms":100}}}}`)

	// amplitude 20000 ≈ 0.61 < 0.8 → still "silence" under the raised threshold.
	if evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(500, speechAmp)}); len(evs) != 0 {
		t.Fatalf("sub-threshold audio started a turn: %v", typesOf(evs))
	}
	// Loud speech (≈0.91) after 500 ms of prior audio → start at 500-100=400.
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, 30000)})
	started := firstEvent(evs, "input_audio_buffer.speech_started")
	if started == nil {
		t.Fatalf("loud audio did not start a turn: %v", typesOf(evs))
	}
	if started["audio_start_ms"] != 400 {
		t.Errorf("audio_start_ms = %v, want 400 (500ms in, minus 100ms padding)", started["audio_start_ms"])
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

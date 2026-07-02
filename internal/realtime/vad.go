// Server VAD / turn detection (Phase 1 of docs/design/realtime-server-vad.md).
//
// Real Realtime clients stream continuous mic audio — silence arrives as
// low-energy PCM frames, not as an absence of frames — so the two core VAD
// transitions (speech start, speech stop) are decidable synchronously inside
// Handle("input_audio_buffer.append") with a plain energy detector, the same
// detector family server_vad itself describes (threshold, "louder audio to
// activate"). No timers, no goroutines: wall-clock behavior (idle_timeout_ms,
// interrupting an in-flight response) is Phase 2.
package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// vadConfig is the decoded turn_detection session config. Defaults follow the
// GA reference: threshold 0.5, prefix_padding_ms 300, silence_duration_ms 500,
// create_response true, interrupt_response true.
type vadConfig struct {
	Type              string  `json:"type"` // "server_vad" | "semantic_vad"
	Threshold         float64 `json:"threshold"`
	PrefixPaddingMs   int     `json:"prefix_padding_ms"`
	SilenceDurationMs int     `json:"silence_duration_ms"`
	CreateResponse    *bool   `json:"create_response"`    // nil → true
	InterruptResponse *bool   `json:"interrupt_response"` // nil → true (Phase 2: cancels an in-flight paced response on speech start)
	IdleTimeoutMs     int     `json:"idle_timeout_ms"`    // Phase 2: 0 = off
	Eagerness         string  `json:"eagerness"`          // semantic_vad: low|medium|high|auto
}

// interrupts reports whether a VAD speech start should cancel an in-flight
// response (GA default: yes).
func (c *vadConfig) interrupts() bool {
	return c.InterruptResponse == nil || *c.InterruptResponse
}

// vadState is the per-session turn-detection state machine. All durations are
// derived from decoded audio length (PCM16 mono @ 24 kHz = 48 bytes/ms), so the
// machine is fully deterministic.
type vadState struct {
	cfg vadConfig
	// totalMs is all audio ever appended this session — the GA audio_start_ms /
	// audio_end_ms fields are "milliseconds from the start of all audio written
	// to the buffer during the session".
	totalMs       float64
	speechActive  bool
	speechStartMs float64
	silenceMs     float64 // low-energy audio accumulated while speech is active
	// pendingItemID is minted at speech start: the GA speech_started event
	// pre-announces "the ID of the user message item that will be created when
	// speech stops", so the eventual commit must use this exact id.
	pendingItemID string
}

// validateTurnDetection checks a session.update's turn_detection payload
// against the GA constraints (Phase 3). It returns the error event to reject
// the update with — code invalid_value and param naming the offending field —
// or nil when the payload is acceptable. Fields the mock doesn't act on stay
// lenient; only values that would silently corrupt the VAD state machine are
// rejected.
func (s *Session) validateTurnDetection(sessionRaw json.RawMessage) []Event {
	var upd struct {
		TurnDetection json.RawMessage `json:"turn_detection"` // beta-flat alias
		Audio         *struct {
			Input *struct {
				TurnDetection json.RawMessage `json:"turn_detection"`
			} `json:"input"`
		} `json:"audio"`
	}
	if len(sessionRaw) == 0 || json.Unmarshal(sessionRaw, &upd) != nil {
		return nil // no turn_detection in this update
	}
	// GA nested wins over the beta-flat alias, mirroring applyConfig.
	td := upd.TurnDetection
	if upd.Audio != nil && upd.Audio.Input != nil && len(upd.Audio.Input.TurnDetection) > 0 {
		td = upd.Audio.Input.TurnDetection
	}
	raw := strings.TrimSpace(string(td))
	if raw == "" || raw == "null" {
		return nil // absent, or explicitly turning VAD off
	}

	const p = "session.audio.input.turn_detection"
	fail := func(field, msg string) []Event {
		return []Event{s.errorEventParam("invalid_value", msg, p+"."+field)}
	}
	var cfg vadConfig
	if err := json.Unmarshal(td, &cfg); err != nil {
		return []Event{s.errorEventParam("invalid_value", "turn_detection must be an object or null", p)}
	}
	switch cfg.Type {
	case "server_vad", "semantic_vad":
	default:
		return fail("type", fmt.Sprintf("invalid turn_detection type %q: expected server_vad or semantic_vad", cfg.Type))
	}
	if cfg.Threshold < 0 || cfg.Threshold > 1 {
		return fail("threshold", "threshold must be between 0.0 and 1.0")
	}
	if cfg.PrefixPaddingMs < 0 {
		return fail("prefix_padding_ms", "prefix_padding_ms must not be negative")
	}
	if cfg.SilenceDurationMs < 0 {
		return fail("silence_duration_ms", "silence_duration_ms must not be negative")
	}
	if cfg.IdleTimeoutMs < 0 {
		return fail("idle_timeout_ms", "idle_timeout_ms must not be negative")
	}
	if cfg.Type == "semantic_vad" {
		switch cfg.Eagerness {
		case "", "auto", "low", "medium", "high":
		default:
			return fail("eagerness", fmt.Sprintf("invalid eagerness %q: expected low, medium, high, or auto", cfg.Eagerness))
		}
	}
	return nil
}

// refreshVAD re-derives the VAD state machine from the session's turn_detection
// config (called when a session.update actually changes it). A null/absent/
// unknown config disables VAD; cumulative audio time survives reconfiguration,
// and an in-progress speech cycle survives a same-type reconfiguration.
// Disabling VAD also disarms the idle timeout — idleTimeout dereferences the
// VAD state, so a deadline left armed after `turn_detection: null` would panic
// when the transport's timer fires.
func (s *Session) refreshVAD() {
	defer func() {
		if s.vad == nil {
			s.idleAt, s.idleFired = time.Time{}, false
		}
	}()
	raw := strings.TrimSpace(string(s.cfg.turnDetection))
	if raw == "" || raw == "null" {
		s.vad = nil
		return
	}
	cfg := vadConfig{Threshold: 0.5, PrefixPaddingMs: 300, SilenceDurationMs: 500}
	if err := json.Unmarshal(s.cfg.turnDetection, &cfg); err != nil {
		s.vad = nil
		return
	}
	switch cfg.Type {
	case "server_vad":
		// defaults above apply
	case "semantic_vad":
		// A mock has no semantic model; eagerness maps to the silence window via
		// the documented max response timeouts (high 2s, auto/medium 4s, low 8s).
		switch cfg.Eagerness {
		case "high":
			cfg.SilenceDurationMs = 2000
		case "low":
			cfg.SilenceDurationMs = 8000
		default: // "auto", "medium", ""
			cfg.SilenceDurationMs = 4000
		}
	default:
		s.vad = nil
		return
	}
	nv := &vadState{cfg: cfg}
	if s.vad != nil {
		nv.totalMs = s.vad.totalMs
		// A same-type reconfiguration (tuned thresholds/timeouts — or a
		// semantically identical config re-serialized with different key
		// order, which defeats the byte-compare skip in the session.update
		// handler) must not drop an in-progress turn: the live speech cycle
		// carries over. Switching detector types starts a fresh cycle.
		if s.vad.cfg.Type == cfg.Type {
			nv.speechActive = s.vad.speechActive
			nv.speechStartMs = s.vad.speechStartMs
			nv.silenceMs = s.vad.silenceMs
			nv.pendingItemID = s.vad.pendingItemID
		}
	}
	s.vad = nv
}

// vadAmplitudeScale maps the GA `threshold` scale onto mean-absolute PCM16
// amplitude. GA's 0..1 threshold is a detector-activation scale, NOT linear
// amplitude: real microphone speech averages only ~0.02–0.15 of full scale
// (−18 dBFS ≈ 0.08), so comparing the raw mean against a GA-typical 0.5 would
// never trigger on real audio. Scaling by 0.1 puts the GA default (0.5 →
// 0.045 mean-abs) below typical speech and above room noise, and preserves
// "a higher threshold requires louder audio".
const vadAmplitudeScale = 0.1

// audioEnergy decodes a base64 PCM16LE append payload and returns its duration
// (ms at 24 kHz mono: 48 bytes/ms) and normalized mean-absolute energy (0..1).
// Payloads that don't decode as base64 count as full-energy speech of an
// estimated duration — a mock shouldn't punish synthetic test audio, and only
// near-zero PCM16 should read as silence.
func audioEnergy(b64 string) (ms, energy float64) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return float64(len(b64)) * 0.75 / 48, 1.0
	}
	ms = float64(len(raw)) / 48
	n := len(raw) / 2
	if n == 0 {
		return ms, 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		sample := int16(uint16(raw[2*i]) | uint16(raw[2*i+1])<<8)
		if sample < 0 {
			// Avoid the int16(-32768) negation overflow.
			sum -= float64(sample)
		} else {
			sum += float64(sample)
		}
	}
	return ms, sum / float64(n) / 32768
}

// vadAppend advances the turn-detection state machine with one append payload
// (already decoded by the caller: duration + energy), returning any server
// events the transition produces: speech_started at speech onset; at end of
// turn (silence_duration_ms of accumulated low-energy audio) speech_stopped
// followed by the full auto-commit — and, unless create_response:false,
// auto-response — ladders.
func (s *Session) vadAppend(ctx context.Context, ms, energy float64) []Event {
	v := s.vad
	startMs := v.totalMs
	v.totalMs += ms
	// Strictly-positive energy is required: with threshold 0 the comparison
	// alone would classify digital silence (energy 0) as speech forever.
	speech := energy > 0 && energy >= v.cfg.Threshold*vadAmplitudeScale

	if speech {
		// The user is talking — reset the idle timeout and re-allow it to fire.
		s.idleAt = time.Time{}
		s.idleFired = false
	}

	if !v.speechActive {
		if !speech {
			return nil // leading / inter-turn silence
		}
		v.speechActive = true
		v.silenceMs = 0
		v.speechStartMs = startMs
		v.pendingItemID = s.nextID("item")
		audioStart := v.speechStartMs - float64(v.cfg.PrefixPaddingMs)
		if audioStart < 0 {
			audioStart = 0
		}
		out := []Event{{"type": "input_audio_buffer.speech_started",
			"audio_start_ms": int(audioStart), "item_id": v.pendingItemID}}
		// Barge-in: a VAD start event interrupts an in-flight (paced) response
		// unless the client set interrupt_response:false.
		if s.inflight != nil && v.cfg.interrupts() {
			out = append(out, s.cancelInflight("turn_detected")...)
		}
		return out
	}

	if speech {
		v.silenceMs = 0 // a short pause was just a pause
		return nil
	}
	v.silenceMs += ms
	if v.silenceMs < float64(v.cfg.SilenceDurationMs) {
		return nil
	}
	return s.vadEndOfTurn(ctx)
}

// vadEndOfTurn closes the detected turn: speech_stopped, then the same commit
// ladder a manual input_audio_buffer.commit produces (byte-identical — it IS
// that handler, which adopts pendingItemID), then the auto-response.
func (s *Session) vadEndOfTurn(ctx context.Context) []Event {
	v := s.vad
	// audio_end_ms "corresponds to the end of audio sent to the model, and thus
	// includes the min_silence_duration_ms" (GA speech_stopped) — the stop
	// DECISION point, not where the silence began.
	endMs := v.totalMs
	out := []Event{{"type": "input_audio_buffer.speech_stopped",
		"audio_end_ms": int(endMs), "item_id": v.pendingItemID}}
	out = append(out, s.handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})...)
	// A transcription-only session ends here: commit + transcription ladder,
	// never a model response.
	if s.isTranscription() {
		return out
	}
	// Auto-respond unless disabled. With a response still in flight (a
	// mid-speech response.create, or interrupt_response:false letting one
	// survive the barge-in) the auto-response is QUEUED, not dropped — Tick
	// runs it when the inflight completes.
	if v.cfg.CreateResponse == nil || *v.cfg.CreateResponse {
		if s.inflight == nil {
			out = append(out, s.createResponse(ctx, &ClientEvent{})...)
		} else {
			s.pendingResponse = true
		}
	}
	return out
}

// vadCommitItemID lets the commit path adopt the item id pre-announced by
// speech_started (and resets the speech cycle); with no pending id it reports
// none and the caller mints one.
func (s *Session) vadCommitItemID() (string, bool) {
	if s.vad == nil || s.vad.pendingItemID == "" {
		return "", false
	}
	id := s.vad.pendingItemID
	s.vad.pendingItemID = ""
	s.vad.speechActive = false
	s.vad.silenceMs = 0
	return id, true
}

// vadReset abandons any in-progress speech cycle (input_audio_buffer.clear).
// Cumulative audio time is kept — audio_start_ms counts all session audio.
func (s *Session) vadReset() {
	if s.vad == nil {
		return
	}
	s.vad.speechActive = false
	s.vad.silenceMs = 0
	s.vad.pendingItemID = ""
}

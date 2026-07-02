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
	"strings"
)

// vadConfig is the decoded turn_detection session config. Defaults follow the
// GA reference: threshold 0.5, prefix_padding_ms 300, silence_duration_ms 500,
// create_response true. interrupt_response and idle_timeout_ms are accepted but
// inert in Phase 1 (they need the deadline-driven async model).
type vadConfig struct {
	Type              string  `json:"type"` // "server_vad" | "semantic_vad"
	Threshold         float64 `json:"threshold"`
	PrefixPaddingMs   int     `json:"prefix_padding_ms"`
	SilenceDurationMs int     `json:"silence_duration_ms"`
	CreateResponse    *bool   `json:"create_response"` // nil → true
	Eagerness         string  `json:"eagerness"`       // semantic_vad: low|medium|high|auto
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

// refreshVAD re-derives the VAD state machine from the session's turn_detection
// config (called after every session.update). A null/absent/unknown config
// disables VAD; cumulative audio time survives reconfiguration.
func (s *Session) refreshVAD() {
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
	}
	s.vad = nv
}

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

// vadAppend advances the turn-detection state machine with one append payload,
// returning any server events the transition produces: speech_started at speech
// onset; at end of turn (silence_duration_ms of accumulated low-energy audio)
// speech_stopped followed by the full auto-commit — and, unless
// create_response:false, auto-response — ladders.
func (s *Session) vadAppend(ctx context.Context, audioB64 string) []Event {
	v := s.vad
	ms, energy := audioEnergy(audioB64)
	startMs := v.totalMs
	v.totalMs += ms
	speech := energy >= v.cfg.Threshold

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
		return []Event{{"type": "input_audio_buffer.speech_started",
			"audio_start_ms": int(audioStart), "item_id": v.pendingItemID}}
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
	endMs := v.totalMs - v.silenceMs // speech ended where the silence began
	out := []Event{{"type": "input_audio_buffer.speech_stopped",
		"audio_end_ms": int(endMs), "item_id": v.pendingItemID}}
	out = append(out, s.handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})...)
	if v.cfg.CreateResponse == nil || *v.cfg.CreateResponse {
		out = append(out, s.createResponse(ctx, &ClientEvent{})...)
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

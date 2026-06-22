// Package realtime implements the deterministic core of a mock OpenAI Realtime
// API session (NF-01). The Realtime API is a bidirectional, event-driven
// protocol normally carried over a WebSocket: the client streams audio/text in
// and the server streams an audio + transcript response back. This package is
// transport-agnostic — a Session turns one inbound ClientEvent into the ordered
// outbound Events a real server would emit — so the WebSocket plumbing
// (internal/adapter/realtime.go) stays thin and the protocol logic is unit
// testable without a socket. Audio is synthesized deterministically (a mock has
// no TTS); the transcript is whatever the agent's scenario engine produces.
package realtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// DefaultModel is the model id reported when a client connects without one.
const DefaultModel = "gpt-realtime"

// audioInputPlaceholder is the transcript used for a committed audio buffer: a
// mock has no speech-to-text, so committed audio becomes a fixed user turn (the
// agent's scenarios then drive the response). Real text input via
// conversation.item.create is the precise path.
const audioInputPlaceholder = "[audio input]"

// Generator produces an engine response for the accumulated conversation. The
// adapter supplies one wrapping engine.ProcessRequestContext; tests supply a
// fake. Keeping the dependency a function (not the whole *engine.Engine) makes
// the Session trivially testable.
type Generator func(ctx context.Context, model, sessionID string, history []engine.RequestMessage) (*engine.Response, error)

// ClientEvent is an inbound Realtime event — the subset the mock handles.
type ClientEvent struct {
	Type    string          `json:"type"`
	EventID string          `json:"event_id,omitempty"`
	Session json.RawMessage `json:"session,omitempty"` // session.update
	Item    json.RawMessage `json:"item,omitempty"`    // conversation.item.create
	Audio   string          `json:"audio,omitempty"`   // input_audio_buffer.append (base64)
}

// Event is an outbound server event. The Realtime protocol has dozens of event
// shapes; a map keeps the emitter readable without a struct per shape.
type Event map[string]any

// sessionConfig is the mutable session.* configuration a client can set. It
// accepts both the GA field names (output_modalities) and the beta aliases
// (modalities) so either generation of SDK round-trips its settings.
type sessionConfig struct {
	Model            string   `json:"model,omitempty"`
	Voice            string   `json:"voice,omitempty"`
	Instructions     string   `json:"instructions,omitempty"`
	Modalities       []string `json:"modalities,omitempty"`        // beta alias
	OutputModalities []string `json:"output_modalities,omitempty"` // GA
	// InputAudioTranscription mirrors session.input_audio_transcription: when set
	// (and not null) the server emits a transcription event for committed audio.
	InputAudioTranscription json.RawMessage `json:"input_audio_transcription,omitempty"`
}

// Session is one Realtime connection's state machine. It is NOT safe for
// concurrent use — drive it from a single read loop.
type Session struct {
	id           string
	initialModel string
	cfg          sessionConfig
	generate     Generator
	history      []engine.RequestMessage
	audioBuffer  bool
	counter      int
}

// NewSession builds a session with the given id (minted by the caller) and the
// model from the connection (may be empty → DefaultModel).
func NewSession(id, model string, gen Generator) *Session {
	return &Session{id: id, initialModel: model, generate: gen}
}

// Greeting returns the events to emit immediately on connect (session.created).
func (s *Session) Greeting() []Event {
	return s.stamp([]Event{{"type": "session.created", "session": s.sessionObject()}})
}

// Handle processes one inbound client event and returns the ordered server
// events to write back. An unknown event yields a single error event. Every
// emitted event is stamped with a unique event_id (a required field on all
// Realtime server events).
func (s *Session) Handle(ctx context.Context, ce *ClientEvent) []Event {
	return s.stamp(s.handle(ctx, ce))
}

// stamp assigns a unique event_id to every event that lacks one. The Realtime
// protocol requires event_id on all server events; a single choke point keeps
// every emit path covered.
func (s *Session) stamp(evs []Event) []Event {
	for _, e := range evs {
		if _, ok := e["event_id"]; !ok {
			e["event_id"] = s.nextID("event")
		}
	}
	return evs
}

func (s *Session) handle(ctx context.Context, ce *ClientEvent) []Event {
	switch ce.Type {
	case "session.update":
		s.applyConfig(ce.Session)
		return []Event{{"type": "session.updated", "session": s.sessionObject()}}

	case "input_audio_buffer.append":
		// A mock does not decode audio; just note the buffer is non-empty.
		s.audioBuffer = true
		return nil

	case "input_audio_buffer.clear":
		s.audioBuffer = false
		return []Event{{"type": "input_audio_buffer.cleared"}}

	case "input_audio_buffer.commit":
		if !s.audioBuffer {
			return []Event{s.errorEvent("input_audio_buffer_commit_empty", "cannot commit an empty input audio buffer")}
		}
		s.audioBuffer = false
		itemID := s.nextID("item")
		s.history = append(s.history, engine.RequestMessage{Role: "user", Content: audioInputPlaceholder})
		// The committed item only carries a transcript when the client enabled
		// input_audio_transcription; otherwise it is null (a mock has no STT, so
		// the transcript is a deterministic placeholder when it is requested).
		var transcript any
		if s.transcriptionEnabled() {
			transcript = audioInputPlaceholder
		}
		out := []Event{
			{"type": "input_audio_buffer.committed", "item_id": itemID},
			{"type": "conversation.item.created", "item": map[string]any{
				"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
				"role": "user", "content": []any{map[string]any{"type": "input_audio", "transcript": transcript}},
			}},
		}
		if s.transcriptionEnabled() {
			out = append(out, Event{
				"type":    "conversation.item.input_audio_transcription.completed",
				"item_id": itemID, "content_index": 0, "transcript": audioInputPlaceholder,
			})
		}
		return out

	case "conversation.item.create":
		role, text := parseItem(ce.Item)
		itemID := s.nextID("item")
		if text != "" {
			s.history = append(s.history, engine.RequestMessage{Role: role, Content: text})
		}
		return []Event{{"type": "conversation.item.created", "item": map[string]any{
			"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
			"role": role, "content": []any{map[string]any{"type": "input_text", "text": text}},
		}}}

	case "response.create":
		return s.createResponse(ctx)

	default:
		return []Event{s.errorEvent("unknown_event", fmt.Sprintf("unknown or unsupported event type %q", ce.Type))}
	}
}

// createResponse runs the engine on the accumulated history and emits the full
// response event ladder. A response can carry several output items: an assistant
// message (audio or, in text-only mode, text) and one function_call item per
// tool call the scenario emitted — so a voice agent that calls tools produces
// the response.function_call_arguments.* events a real client drives its tool
// loop from. The ladder opens with response.created and ends with response.done
// (whose output lists every item).
func (s *Session) createResponse(ctx context.Context) []Event {
	respID := s.nextID("resp")

	resp, err := s.generate(ctx, s.model(), s.id, s.engineHistory())
	if err != nil {
		return []Event{s.errorEvent("response_generation_failed", err.Error())}
	}
	if resp == nil {
		return []Event{s.errorEvent("response_generation_failed", "engine returned no response")}
	}

	inputTokens := countTokens(s.history)

	transcript := resp.Content
	hasTools := len(resp.ToolCalls) > 0
	// Emit a message item when there is content, or when there are no tool calls
	// at all (so the ladder is never empty — a bare tool-call turn skips it).
	emitMessage := transcript != "" || !hasTools
	if emitMessage && transcript == "" {
		transcript = "(no content)"
	}
	if emitMessage {
		s.history = append(s.history, engine.RequestMessage{Role: "assistant", Content: transcript})
	}
	outputTokens := wordCount(transcript)

	out := []Event{
		{"type": "response.created", "response": map[string]any{
			"id": respID, "object": "realtime.response", "status": "in_progress", "output": []any{}}},
		// rate_limits.updated is emitted at the start of a response (tokens are
		// reserved on creation); synthesized deterministically.
		{"type": "rate_limits.updated", "rate_limits": []any{
			map[string]any{"name": "requests", "limit": 10000, "remaining": 9999, "reset_seconds": 0.06},
			map[string]any{"name": "tokens", "limit": 1000000, "remaining": 1000000 - inputTokens - outputTokens, "reset_seconds": 0.0},
		}},
	}

	var items []any
	outputIndex := 0
	if emitMessage {
		items = append(items, s.appendMessageLadder(&out, respID, transcript, outputIndex))
		outputIndex++
	}
	for _, tc := range resp.ToolCalls {
		items = append(items, s.appendFunctionCallLadder(&out, respID, tc, outputIndex))
		outputIndex++
	}

	out = append(out, Event{"type": "response.done", "response": map[string]any{
		"id": respID, "object": "realtime.response", "status": "completed",
		"output": items,
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"total_tokens":  inputTokens + outputTokens,
		}}})
	return out
}

// appendMessageLadder emits the assistant-message item events (item.added →
// content_part.added → deltas → *.done) and returns the completed item. In
// text-only mode (output_modalities without "audio") it streams
// response.output_text.delta and an output_text content part; otherwise it
// streams the GA audio ladder (output_audio + output_audio_transcript).
func (s *Session) appendMessageLadder(out *[]Event, respID, transcript string, outputIndex int) map[string]any {
	itemID := s.nextID("msg")

	if s.textOnly() {
		final := map[string]any{
			"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
			"role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": transcript}},
		}
		*out = append(*out,
			Event{"type": "response.output_item.added", "response_id": respID, "output_index": outputIndex, "item": map[string]any{
				"id": itemID, "object": "realtime.item", "type": "message", "status": "in_progress",
				"role": "assistant", "content": []any{}}},
			Event{"type": "response.content_part.added", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "part": map[string]any{"type": "output_text", "text": ""}},
		)
		for _, chunk := range chunkText(transcript) {
			*out = append(*out, Event{"type": "response.output_text.delta", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "delta": chunk})
		}
		*out = append(*out,
			Event{"type": "response.output_text.done", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "text": transcript},
			Event{"type": "response.content_part.done", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "part": map[string]any{"type": "output_text", "text": transcript}},
			Event{"type": "response.output_item.done", "response_id": respID, "output_index": outputIndex, "item": final},
		)
		return final
	}

	// GA audio ladder: assistant audio is the "output_audio" content part, and the
	// streamed events are response.output_audio*.delta/.done (not the beta
	// response.audio*.delta names).
	final := map[string]any{
		"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
		"role": "assistant", "content": []any{map[string]any{"type": "output_audio", "transcript": transcript}},
	}
	*out = append(*out,
		Event{"type": "response.output_item.added", "response_id": respID, "output_index": outputIndex, "item": map[string]any{
			"id": itemID, "object": "realtime.item", "type": "message", "status": "in_progress",
			"role": "assistant", "content": []any{}}},
		Event{"type": "response.content_part.added", "response_id": respID, "item_id": itemID,
			"output_index": outputIndex, "content_index": 0, "part": map[string]any{"type": "output_audio", "transcript": ""}},
	)
	for _, chunk := range chunkText(transcript) {
		*out = append(*out,
			Event{"type": "response.output_audio_transcript.delta", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "delta": chunk},
			Event{"type": "response.output_audio.delta", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "delta": synthAudioChunk(chunk)},
		)
	}
	*out = append(*out,
		Event{"type": "response.output_audio.done", "response_id": respID, "item_id": itemID, "output_index": outputIndex, "content_index": 0},
		Event{"type": "response.output_audio_transcript.done", "response_id": respID, "item_id": itemID,
			"output_index": outputIndex, "content_index": 0, "transcript": transcript},
		Event{"type": "response.content_part.done", "response_id": respID, "item_id": itemID,
			"output_index": outputIndex, "content_index": 0, "part": map[string]any{"type": "output_audio", "transcript": transcript}},
		Event{"type": "response.output_item.done", "response_id": respID, "output_index": outputIndex, "item": final},
	)
	return final
}

// appendFunctionCallLadder emits a function_call output item and its streamed
// argument events (response.function_call_arguments.delta/.done), returning the
// completed item. This is what a Realtime client's tool loop consumes: it reads
// call_id + name + assembled arguments, runs the tool, and sends the result back
// as a conversation.item.create of type function_call_output.
func (s *Session) appendFunctionCallLadder(out *[]Event, respID string, tc types.ToolCallSpec, outputIndex int) map[string]any {
	itemID := s.nextID("fc")
	callID := s.nextID("call")
	args := marshalArgs(tc.Arguments)

	final := map[string]any{
		"id": itemID, "object": "realtime.item", "type": "function_call", "status": "completed",
		"name": tc.Name, "call_id": callID, "arguments": args,
	}
	*out = append(*out,
		Event{"type": "response.output_item.added", "response_id": respID, "output_index": outputIndex, "item": map[string]any{
			"id": itemID, "object": "realtime.item", "type": "function_call", "status": "in_progress",
			"name": tc.Name, "call_id": callID, "arguments": ""}},
	)
	for _, chunk := range chunkArgs(args) {
		*out = append(*out, Event{"type": "response.function_call_arguments.delta", "response_id": respID, "item_id": itemID,
			"output_index": outputIndex, "call_id": callID, "delta": chunk})
	}
	*out = append(*out,
		Event{"type": "response.function_call_arguments.done", "response_id": respID, "item_id": itemID,
			"output_index": outputIndex, "call_id": callID, "arguments": args},
		Event{"type": "response.output_item.done", "response_id": respID, "output_index": outputIndex, "item": final},
	)
	return final
}

// engineHistory is the conversation handed to the engine, prepended with the
// session instructions as a system message when one is set.
func (s *Session) engineHistory() []engine.RequestMessage {
	if s.cfg.Instructions == "" {
		return s.history
	}
	out := make([]engine.RequestMessage, 0, len(s.history)+1)
	out = append(out, engine.RequestMessage{Role: "system", Content: s.cfg.Instructions})
	return append(out, s.history...)
}

func (s *Session) model() string {
	if s.cfg.Model != "" {
		return s.cfg.Model
	}
	if s.initialModel != "" {
		return s.initialModel
	}
	return DefaultModel
}

func (s *Session) sessionObject() map[string]any {
	mods := s.outputModalities()
	voice := s.cfg.Voice
	if voice == "" {
		voice = "alloy"
	}
	return map[string]any{
		// GA session objects carry type:"realtime" and output_modalities; the
		// beta "modalities" alias is mirrored so an older SDK still reads it.
		"id": s.id, "object": "realtime.session", "type": "realtime", "model": s.model(),
		"output_modalities": mods, "modalities": mods,
		"voice": voice, "instructions": s.cfg.Instructions,
	}
}

// outputModalities resolves the effective response modalities, preferring the GA
// output_modalities field over the beta modalities alias, defaulting to both.
func (s *Session) outputModalities() []string {
	if len(s.cfg.OutputModalities) > 0 {
		return s.cfg.OutputModalities
	}
	if len(s.cfg.Modalities) > 0 {
		return s.cfg.Modalities
	}
	return []string{"audio", "text"}
}

// textOnly reports whether the client asked for a text-only response (modalities
// were set and do not include "audio").
func (s *Session) textOnly() bool {
	mods := s.outputModalities()
	for _, m := range mods {
		if m == "audio" {
			return false
		}
	}
	return len(mods) > 0
}

// transcriptionEnabled reports whether the client configured
// input_audio_transcription (a non-null value).
func (s *Session) transcriptionEnabled() bool {
	raw := strings.TrimSpace(string(s.cfg.InputAudioTranscription))
	return raw != "" && raw != "null"
}

func (s *Session) applyConfig(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var c sessionConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return
	}
	if c.Model != "" {
		s.cfg.Model = c.Model
	}
	if c.Voice != "" {
		s.cfg.Voice = c.Voice
	}
	if c.Instructions != "" {
		s.cfg.Instructions = c.Instructions
	}
	if len(c.Modalities) > 0 {
		s.cfg.Modalities = c.Modalities
	}
	if len(c.OutputModalities) > 0 {
		s.cfg.OutputModalities = c.OutputModalities
	}
	if len(c.InputAudioTranscription) > 0 {
		s.cfg.InputAudioTranscription = c.InputAudioTranscription
	}
}

func (s *Session) errorEvent(code, msg string) Event {
	return Event{"type": "error", "error": map[string]any{
		"type": "invalid_request_error", "code": code, "message": msg}}
}

func (s *Session) nextID(prefix string) string {
	s.counter++
	return fmt.Sprintf("%s_%s_%d", prefix, s.id, s.counter)
}

// --- helpers ---

// parseItem extracts the role and text from a conversation.item.create item. It
// accepts the Realtime content array ([{type:input_text|text, text:"..."}]).
func parseItem(raw json.RawMessage) (role, text string) {
	if len(raw) == 0 {
		return "user", ""
	}
	var item struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return "user", ""
	}
	role = item.Role
	if role == "" {
		role = "user"
	}
	var parts []string
	for _, c := range item.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return role, strings.Join(parts, " ")
}

// chunkText splits a transcript into word chunks (each keeping its trailing
// space), so the audio_transcript.delta stream looks like incremental tokens.
func chunkText(s string) []string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return []string{s}
	}
	out := make([]string, len(fields))
	for i, f := range fields {
		if i < len(fields)-1 {
			out[i] = f + " "
		} else {
			out[i] = f
		}
	}
	return out
}

// marshalArgs renders a tool call's arguments as the JSON string the Realtime
// protocol carries in function_call items, defaulting to "{}" for none.
func marshalArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// chunkArgs splits a function-call arguments JSON string into fixed-size,
// rune-safe pieces so the function_call_arguments.delta stream looks incremental
// (a client concatenates the deltas back into the full string).
func chunkArgs(s string) []string {
	if s == "" {
		return []string{""}
	}
	const size = 20
	var out []string
	runes := []rune(s)
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

// synthAudioChunk returns a deterministic base64 PCM16 chunk for a transcript
// fragment. A mock has no TTS; a sha256-derived sample block is stable across
// runs and non-empty, which is all a client needs to exercise audio handling.
func synthAudioChunk(s string) string {
	h := sha256.Sum256([]byte(s))
	return base64.StdEncoding.EncodeToString(h[:]) // 32 bytes = 16 PCM16 samples
}

func wordCount(s string) int { return len(strings.Fields(s)) }

func countTokens(msgs []engine.RequestMessage) int {
	n := 0
	for _, m := range msgs {
		n += wordCount(m.Content)
	}
	return n
}

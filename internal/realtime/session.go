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
	"time"

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
	Session  json.RawMessage `json:"session,omitempty"`  // session.update
	Item     json.RawMessage `json:"item,omitempty"`     // conversation.item.create
	Audio    string          `json:"audio,omitempty"`    // input_audio_buffer.append (base64)
	Response json.RawMessage `json:"response,omitempty"` // response.create inline overrides
	// conversation.item.retrieve / delete / truncate
	ItemID       string `json:"item_id,omitempty"`
	ContentIndex int    `json:"content_index,omitempty"` // truncate
	AudioEndMs   int    `json:"audio_end_ms,omitempty"`  // truncate
	// conversation.item.create: insert after this item ("" = append at the
	// end; "root" = insert at the beginning).
	PreviousItemID string `json:"previous_item_id,omitempty"`
}

// Event is an outbound server event. The Realtime protocol has dozens of event
// shapes; a map keeps the emitter readable without a struct per shape.
type Event map[string]any

// sessionConfig is the effective, mutable session state echoed back in the GA
// session object. applyConfig merges incoming session.update payloads (GA-nested
// or beta-flat) into it; raw json.RawMessage fields are stored verbatim so they
// round-trip exactly. A zero value yields the GA defaults (see sessionObject).
type sessionConfig struct {
	model            string
	voice            string
	instructions     string
	outputModalities []string
	tools            json.RawMessage // session.tools (default [])
	toolChoice       json.RawMessage // session.tool_choice (default "auto")
	maxOutputTokens  json.RawMessage // session.max_output_tokens (default "inf")
	transcription    json.RawMessage // audio.input.transcription (nil = off)
	turnDetection    json.RawMessage // audio.input.turn_detection (nil = off)
	inputFormat      json.RawMessage // audio.input.format
	outputFormat     json.RawMessage // audio.output.format
	speed            float64         // audio.output.speed (default 1.0)
	// Round-tripped verbatim (the mock stores + echoes them; only tools/
	// turn_detection/transcription change behavior):
	tracing           json.RawMessage // session.tracing (default null)
	truncation        json.RawMessage // session.truncation (default "auto")
	prompt            json.RawMessage // session.prompt (default null)
	include           json.RawMessage // session.include (default null)
	parallelToolCalls json.RawMessage // session.parallel_tool_calls (default true)
	noiseReduction    json.RawMessage // audio.input.noise_reduction (default null)
}

// sessionUpdate is the inbound session.update payload. It accepts the GA shape
// (audio.input/output, output_modalities) AND the beta aliases (top-level voice,
// modalities, input_audio_transcription) so either SDK generation round-trips.
type sessionUpdate struct {
	Model                   string          `json:"model"`
	Voice                   string          `json:"voice"` // beta top-level alias
	Instructions            string          `json:"instructions"`
	Modalities              []string        `json:"modalities"`        // beta alias
	OutputModalities        []string        `json:"output_modalities"` // GA
	InputAudioTranscription json.RawMessage `json:"input_audio_transcription"`
	Tools                   json.RawMessage `json:"tools"`
	ToolChoice              json.RawMessage `json:"tool_choice"`
	MaxOutputTokens         json.RawMessage `json:"max_output_tokens"`
	Tracing                 json.RawMessage `json:"tracing"`
	Truncation              json.RawMessage `json:"truncation"`
	Prompt                  json.RawMessage `json:"prompt"`
	Include                 json.RawMessage `json:"include"`
	ParallelToolCalls       json.RawMessage `json:"parallel_tool_calls"`
	TurnDetection           json.RawMessage `json:"turn_detection"`      // beta top-level alias
	InputAudioFormat        string          `json:"input_audio_format"`  // beta alias ("pcm16", "g711_ulaw", "g711_alaw")
	OutputAudioFormat       string          `json:"output_audio_format"` // beta alias
	Audio                   *struct {
		Input *struct {
			Transcription  json.RawMessage `json:"transcription"`
			TurnDetection  json.RawMessage `json:"turn_detection"`
			Format         json.RawMessage `json:"format"`
			NoiseReduction json.RawMessage `json:"noise_reduction"`
		} `json:"input"`
		Output *struct {
			Voice  string          `json:"voice"`
			Format json.RawMessage `json:"format"`
			Speed  *float64        `json:"speed"`
		} `json:"output"`
	} `json:"audio"`
}

// betaAudioFormat translates a beta-flat format string into the GA format
// object, so a beta-generation client's setting round-trips in GA shape.
func betaAudioFormat(name string) json.RawMessage {
	switch name {
	case "pcm16":
		return json.RawMessage(defaultAudioFormat)
	case "g711_ulaw":
		return json.RawMessage(`{"type":"audio/pcmu"}`)
	case "g711_alaw":
		return json.RawMessage(`{"type":"audio/pcma"}`)
	default:
		return nil // unknown names are dropped, matching the mock's leniency
	}
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
	expiresAt    int64  // unix seconds; emitted in the session object when > 0
	lastItemID   string // id of the most recently created conversation item ("" → previous_item_id is null)
	// lastClientEventID is the event_id of the client event currently being
	// handled; error events echo it as error.event_id (the GA correlation handle).
	lastClientEventID string
	// items indexes every completed conversation item by id so
	// conversation.item.retrieve / delete / truncate can address them.
	items map[string]map[string]any
	// vad is the server turn-detection state machine (vad.go); nil when the
	// client has not enabled turn_detection.
	vad *vadState
	// Phase-2 deadline state (pace.go): the injected clock (nil = time.Now),
	// the transport's paced-emission interval (0 = burst), the paced response
	// currently mid-emission, and the armed idle-timeout deadline (zero = none).
	now          func() time.Time
	paceInterval time.Duration
	inflight     *inflightResponse
	idleAt       time.Time
	// idleFired guards the idle timeout to once per stretch of user inactivity
	// (a deliberate mock safety: a silent connection must not self-prompt
	// forever); cleared by user activity.
	idleFired bool
	// bufferedMs is the decoded duration of un-committed appended audio — the
	// transcription usage ("duration" variant) reported on commit.
	bufferedMs float64
}

// rememberItem indexes a completed conversation item for later retrieve /
// delete / truncate, and returns it for inline use.
func (s *Session) rememberItem(item map[string]any) map[string]any {
	if id, _ := item["id"].(string); id != "" {
		s.items[id] = item
	}
	return item
}

// previousItemID returns the value for a server event's previous_item_id field:
// the id of the item created just before the current one, or nil (JSON null)
// when this is the first item in the conversation.
func (s *Session) previousItemID() any {
	if s.lastItemID == "" {
		return nil
	}
	return s.lastItemID
}

// newConversationItem does the bookkeeping every conversation-item emitter must
// do: capture the previous_item_id value, mint the item id, and record it as the
// most recent conversation item so the next item chains off it.
func (s *Session) newConversationItem() (prev any, id string) {
	prev = s.previousItemID()
	id = s.nextID("item")
	s.lastItemID = id
	return prev, id
}

// conversationItemEvents returns the GA server-event pair announcing a new,
// already-complete conversation item: conversation.item.added then
// conversation.item.done (GA retired the beta conversation.item.created). Both
// carry previous_item_id and the full item.
func conversationItemEvents(prev any, item map[string]any) []Event {
	return []Event{
		{"type": "conversation.item.added", "previous_item_id": prev, "item": item},
		{"type": "conversation.item.done", "previous_item_id": prev, "item": item},
	}
}

// NewSession builds a session with the given id (minted by the caller) and the
// model from the connection (may be empty → DefaultModel).
func NewSession(id, model string, gen Generator) *Session {
	return &Session{id: id, initialModel: model, generate: gen, items: make(map[string]map[string]any)}
}

// SetExpiry sets the session's expiry (unix seconds), reported as expires_at in
// the GA session object so a client can schedule a reconnect. The transport sets
// it from the wall clock; left unset (0) it is omitted, keeping Session
// deterministic for tests.
func (s *Session) SetExpiry(unix int64) { s.expiresAt = unix }

// Greeting returns the events to emit immediately on connect (session.created).
func (s *Session) Greeting() []Event {
	return s.stamp([]Event{{"type": "session.created", "session": s.sessionObject()}})
}

// Handle processes one inbound client event and returns the ordered server
// events to write back. An unknown event yields a single error event. Every
// emitted event is stamped with a unique event_id (a required field on all
// Realtime server events).
func (s *Session) Handle(ctx context.Context, ce *ClientEvent) []Event {
	s.lastClientEventID = ce.EventID
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
		// An invalid turn_detection config rejects the WHOLE update with a GA
		// error (code invalid_value, param naming the field) — accept-and-warp
		// would silently corrupt the VAD state machine.
		if errEvs := s.validateTurnDetection(ce.Session); errEvs != nil {
			return errEvs
		}
		s.applyConfig(ce.Session)
		s.refreshVAD()
		return []Event{{"type": "session.updated", "session": s.sessionObject()}}

	case "input_audio_buffer.append":
		s.audioBuffer = true
		ms, energy := audioEnergy(ce.Audio)
		s.bufferedMs += ms
		// With turn detection enabled the appended audio drives the VAD state
		// machine (which may auto-commit and auto-respond); otherwise a mock
		// just notes the buffer is non-empty.
		if s.vad != nil {
			return s.vadAppend(ctx, ms, energy)
		}
		return nil

	case "input_audio_buffer.clear":
		s.audioBuffer = false
		s.bufferedMs = 0
		s.vadReset()
		return []Event{{"type": "input_audio_buffer.cleared"}}

	case "input_audio_buffer.commit":
		if !s.audioBuffer {
			return []Event{s.errorEvent("input_audio_buffer_commit_empty", "cannot commit an empty input audio buffer")}
		}
		s.audioBuffer = false
		s.idleAt, s.idleFired = time.Time{}, false // user activity resets the idle timeout
		committedSeconds := s.bufferedMs / 1000
		s.bufferedMs = 0
		// A VAD-detected turn pre-announced its item id on speech_started; the
		// committed item must carry that exact id. Manual turns mint one here.
		var prevItem any
		var itemID string
		if id, ok := s.vadCommitItemID(); ok {
			prevItem, itemID = s.previousItemID(), id
			s.lastItemID = id
		} else {
			prevItem, itemID = s.newConversationItem()
		}
		s.history = append(s.history, engine.RequestMessage{Role: "user", Content: audioInputPlaceholder})
		// The committed item only carries a transcript when the client enabled
		// input_audio_transcription; otherwise it is null (a mock has no STT, so
		// the transcript is a deterministic placeholder when it is requested).
		var transcript any
		if s.transcriptionEnabled() {
			transcript = audioInputPlaceholder
		}
		out := append([]Event{
			{"type": "input_audio_buffer.committed", "previous_item_id": prevItem, "item_id": itemID},
		}, conversationItemEvents(prevItem, s.rememberItem(map[string]any{
			"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
			"role": "user", "content": []any{map[string]any{"type": "input_audio", "transcript": transcript}},
		}))...)
		if s.transcriptionEnabled() {
			// GA streams the transcription: delta chunks, then completed with the
			// full transcript and a REQUIRED usage field — the "duration" variant
			// is the honest one for a mock (derived from the decoded audio length,
			// deterministic; ASR-token billing does not apply here).
			for _, chunk := range chunkText(audioInputPlaceholder) {
				out = append(out, Event{"type": "conversation.item.input_audio_transcription.delta",
					"item_id": itemID, "content_index": 0, "delta": chunk})
			}
			out = append(out, Event{
				"type":    "conversation.item.input_audio_transcription.completed",
				"item_id": itemID, "content_index": 0, "transcript": audioInputPlaceholder,
				"usage": map[string]any{"type": "duration", "seconds": committedSeconds},
			})
		}
		return out

	case "conversation.item.create":
		s.idleAt, s.idleFired = time.Time{}, false // user activity resets the idle timeout
		it := parseItem(ce.Item)
		// Honor a client-supplied item id (clients pre-generate ids so they can
		// truncate/delete/retrieve their own items later); duplicates rejected.
		itemID := it.ID
		if itemID == "" {
			itemID = s.nextID("item")
		} else if _, exists := s.items[itemID]; exists {
			return []Event{s.errorEventParam("invalid_value",
				fmt.Sprintf("item with id %q already exists", itemID), "item.id")}
		}
		// previous_item_id places the item: "" appends, "root" inserts first, a
		// known id inserts after it. The new item becomes the chain tail only
		// when placed at the end. (Mock simplification, documented: engine
		// history stays append-order — insertion positions the event-log view,
		// not scenario-matching order.)
		var prevItem any
		tail := true
		switch ce.PreviousItemID {
		case "":
			prevItem = s.previousItemID()
		case "root":
			prevItem, tail = nil, s.lastItemID == ""
		default:
			if _, known := s.items[ce.PreviousItemID]; !known {
				return []Event{s.errorEventParam("item_not_found",
					fmt.Sprintf("previous_item_id %q not found", ce.PreviousItemID), "previous_item_id")}
			}
			prevItem, tail = ce.PreviousItemID, ce.PreviousItemID == s.lastItemID
		}
		if tail {
			s.lastItemID = itemID
		}
		switch it.Type {
		case "function_call_output":
			// The tool-loop reply. Same history mapping as the Responses
			// adapters (responsesItemToMessage): role "tool" joins the history
			// without becoming the matched user message, so a follow-up
			// response.create can scenario-match on the tool result.
			s.history = append(s.history, engine.RequestMessage{Role: "tool", Content: it.Output})
			return conversationItemEvents(prevItem, s.rememberItem(map[string]any{
				"id": itemID, "object": "realtime.item", "type": "function_call_output", "status": "completed",
				"call_id": it.CallID, "output": it.Output,
			}))
		case "function_call":
			// An echoed prior tool call (context replay): an assistant turn with
			// no matchable text, acked with the real function_call item shape.
			s.history = append(s.history, engine.RequestMessage{Role: "assistant", Content: ""})
			return conversationItemEvents(prevItem, s.rememberItem(map[string]any{
				"id": itemID, "object": "realtime.item", "type": "function_call", "status": "completed",
				"call_id": it.CallID, "name": it.Name, "arguments": it.Arguments,
			}))
		default:
			text := it.text()
			if text != "" {
				s.history = append(s.history, engine.RequestMessage{Role: it.Role, Content: text})
			}
			return conversationItemEvents(prevItem, s.rememberItem(map[string]any{
				"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
				"role": it.Role, "content": []any{map[string]any{"type": "input_text", "text": text}},
			}))
		}

	case "response.create":
		// A client-driven response is user activity (an idle-triggered one is
		// not — idleTimeout calls createResponse directly, not through here).
		s.idleFired = false
		return s.createResponse(ctx, ce)

	case "conversation.item.retrieve":
		if item, ok := s.items[ce.ItemID]; ok {
			return []Event{{"type": "conversation.item.retrieved", "item": item}}
		}
		return []Event{s.errorEvent("item_not_found", fmt.Sprintf("item %q not found", ce.ItemID))}

	case "conversation.item.delete":
		if _, ok := s.items[ce.ItemID]; !ok {
			return []Event{s.errorEvent("item_not_found", fmt.Sprintf("item %q not found", ce.ItemID))}
		}
		delete(s.items, ce.ItemID)
		// Mock simplification: the engine history is not rewritten — deletion
		// affects retrieval, not scenario matching on prior turns.
		return []Event{{"type": "conversation.item.deleted", "item_id": ce.ItemID}}

	case "conversation.item.truncate":
		// The barge-in primitive: a client truncates an assistant audio item at
		// the point playback stopped. A mock has no audio timeline, so the whole
		// synthesized transcript past the cut is dropped (real servers remove
		// the transcript for the truncated span) and the ack echoes the request.
		item, ok := s.items[ce.ItemID]
		if !ok {
			return []Event{s.errorEvent("item_not_found", fmt.Sprintf("item %q not found", ce.ItemID))}
		}
		if content, ok := item["content"].([]any); ok {
			for _, c := range content {
				if part, ok := c.(map[string]any); ok && part["type"] == "output_audio" {
					part["transcript"] = ""
				}
			}
		}
		return []Event{{"type": "conversation.item.truncated",
			"item_id": ce.ItemID, "content_index": ce.ContentIndex, "audio_end_ms": ce.AudioEndMs}}

	case "response.cancel":
		// A paced response can actually be in flight now — cancel it. Otherwise
		// (burst mode, or nothing running) the real API also errors, with this
		// cancel-specific code that SDKs recognize and suppress (unknown_event
		// they surface as a protocol failure).
		if s.inflight != nil {
			return s.cancelInflight("client_cancelled")
		}
		return []Event{s.errorEvent("response_cancel_not_active", "Cancellation failed: no active response found")}

	default:
		return []Event{s.errorEvent("unknown_event", fmt.Sprintf("unknown or unsupported event type %q", ce.Type))}
	}
}

// responseConfig is the inline `response` payload of response.create — the GA
// per-response overrides the mock honors: instructions, output_modalities (or
// the beta modalities alias), metadata (echoed on the response envelope), and
// conversation ("none" → out-of-band: the response joins no conversation).
// Per-response tools and custom `input` context are not supported (the engine's
// tools come from the agent definition).
type responseConfig struct {
	Instructions     string          `json:"instructions"`
	OutputModalities []string        `json:"output_modalities"`
	Modalities       []string        `json:"modalities"` // beta alias
	Metadata         json.RawMessage `json:"metadata"`
	Conversation     string          `json:"conversation"` // "auto" (default) | "none"
}

// responseCtx carries one response's effective settings through the ladder:
// session defaults overlaid with the response.create inline overrides.
type responseCtx struct {
	mods         []string
	outOfBand    bool            // conversation:"none" — no history, no conversation-item mirror, conversation_id null
	metadata     json.RawMessage // echoed verbatim on the response envelope (nil → null)
	instructions string
}

func (s *Session) newResponseCtx(raw json.RawMessage) *responseCtx {
	rc := &responseCtx{mods: s.outputModalities(), instructions: s.cfg.instructions}
	var cfg responseConfig
	if len(raw) > 0 && json.Unmarshal(raw, &cfg) == nil {
		if len(cfg.OutputModalities) > 0 {
			rc.mods = cfg.OutputModalities
		} else if len(cfg.Modalities) > 0 {
			rc.mods = cfg.Modalities
		}
		if cfg.Instructions != "" {
			rc.instructions = cfg.Instructions
		}
		rc.metadata = cfg.Metadata
		rc.outOfBand = cfg.Conversation == "none"
	}
	return rc
}

// textOnly reports whether this response streams text (mods set and omitting
// "audio") — mods is never empty (session default is ["audio"]).
func (rc *responseCtx) textOnly() bool {
	for _, m := range rc.mods {
		if m == "audio" {
			return false
		}
	}
	return true
}

// createResponse runs the engine on the accumulated history and emits the full
// response event ladder. A response can carry several output items: an assistant
// message (audio or, in text-only mode, text) and one function_call item per
// tool call the scenario emitted — so a voice agent that calls tools produces
// the response.function_call_arguments.* events a real client drives its tool
// loop from. The ladder opens with response.created and ends with response.done
// (whose output lists every item).
func (s *Session) createResponse(ctx context.Context, ce *ClientEvent) []Event {
	// One response at a time — the real API rejects a second response.create
	// while one is active on the conversation.
	if s.inflight != nil {
		return []Event{s.errorEvent("conversation_already_has_active_response",
			"Conversation already has an active response")}
	}
	s.idleAt = time.Time{} // user activity resets the idle timeout
	respID := s.nextID("resp")
	rc := s.newResponseCtx(ce.Response)

	resp, err := s.generate(ctx, s.model(), s.id, s.engineHistory(rc.instructions))
	if err != nil {
		return s.failedResponse(respID, err.Error(), rc)
	}
	if resp == nil {
		return s.failedResponse(respID, "engine returned no response", rc)
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
	// An out-of-band response never joins the default conversation, so its
	// transcript must not become context for later turns. For a paced response
	// the append is deferred to completion — a cancelled response leaves no
	// transcript behind.
	appendHistory := func() {
		if emitMessage && !rc.outOfBand {
			s.history = append(s.history, engine.RequestMessage{Role: "assistant", Content: transcript})
		}
	}
	outputTokens := wordCount(transcript)

	out := []Event{
		{"type": "response.created", "response": s.responseObject(respID, "in_progress", []any{}, rc)},
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
		items = append(items, s.appendMessageLadder(&out, respID, transcript, outputIndex, rc))
		outputIndex++
	}
	for _, tc := range resp.ToolCalls {
		items = append(items, s.appendFunctionCallLadder(&out, respID, tc, outputIndex, rc))
		outputIndex++
	}

	done := s.responseObject(respID, "completed", items, rc)
	done["usage"] = map[string]any{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"total_tokens":  inputTokens + outputTokens,
		// GA per-modality breakdown. A mock attributes everything to text (the
		// transcript drives output; synthesized audio carries no real tokens).
		"input_token_details": map[string]any{
			"text_tokens": inputTokens, "audio_tokens": 0, "image_tokens": 0, "cached_tokens": 0,
			"cached_tokens_details": map[string]any{"text_tokens": 0, "audio_tokens": 0, "image_tokens": 0},
		},
		"output_token_details": map[string]any{"text_tokens": outputTokens, "audio_tokens": 0},
	}
	out = append(out, Event{"type": "response.done", "response": done})

	// Paced sessions emit response.created + rate_limits now and the rest of
	// the ladder against deadlines (Tick) — the interruption window barge-in
	// and response.cancel need. Burst mode returns everything at once.
	if s.paced() {
		return s.beginPacedResponse(respID, rc, out, appendHistory)
	}
	appendHistory()
	s.armIdleTimer()
	return out
}

// failedResponse emits the GA failure ladder for a response that could not be
// generated. response.done is ALWAYS emitted no matter the final state — a bare
// error event alone would leave a client awaiting response.done hanging — so:
// response.created → error (type server_error, carrying the detail) →
// response.done (status "failed" + status_details.error).
func (s *Session) failedResponse(respID, msg string, rc *responseCtx) []Event {
	failed := s.responseObject(respID, "failed", []any{}, rc)
	failed["status_details"] = map[string]any{
		"type": "failed",
		// status_details.error carries only type+code (GA RealtimeResponseStatus);
		// the human-readable detail travels on the error event above it.
		"error": map[string]any{"type": "server_error", "code": "response_generation_failed"},
	}
	return []Event{
		{"type": "response.created", "response": s.responseObject(respID, "in_progress", []any{}, rc)},
		{"type": "error", "error": s.errorBody("server_error", "response_generation_failed", msg)},
		{"type": "response.done", "response": failed},
	}
}

// responseObject builds the GA `response` envelope shared by response.created and
// response.done: id/object/status plus the GA fields a client reads off the
// final event (output_modalities, conversation_id, status_details, metadata).
// An out-of-band response (conversation:"none") carries conversation_id null —
// the discriminator clients use to route side-responses, along with metadata.
func (s *Session) responseObject(respID, status string, output []any, rc *responseCtx) map[string]any {
	var convID any
	if !rc.outOfBand {
		convID = "conv_" + s.id
	}
	var metadata any
	if len(rc.metadata) > 0 {
		metadata = rc.metadata
	}
	return map[string]any{
		"id": respID, "object": "realtime.response", "status": status,
		"status_details":    nil,
		"output":            output,
		"output_modalities": rc.mods,
		"conversation_id":   convID,
		"metadata":          metadata,
		// GA fields a strict reader expects on every envelope: usage is null
		// until response.done overwrites it; audio echoes the effective output
		// config; max_output_tokens mirrors the session setting.
		"usage": nil,
		"audio": map[string]any{"output": map[string]any{
			"voice": s.effectiveVoice(), "format": rawOr(s.cfg.outputFormat, defaultAudioFormat)}},
		"max_output_tokens": rawOr(s.cfg.maxOutputTokens, `"inf"`),
	}
}

// appendMessageLadder emits the assistant-message item events (item.added →
// content_part.added → deltas → *.done) and returns the completed item. In
// text-only mode (output_modalities without "audio") it streams
// response.output_text.delta and an output_text content part; otherwise it
// streams the GA audio ladder (output_audio + output_audio_transcript).
func (s *Session) appendMessageLadder(out *[]Event, respID, transcript string, outputIndex int, rc *responseCtx) map[string]any {
	itemID := s.nextID("msg")
	// A response output item joins the conversation: capture what it chains off,
	// then record it so a later user turn's previous_item_id points at it.
	// Out-of-band responses (conversation:"none") join nothing — no chain update
	// and no conversation-item mirror below.
	prevItem := s.previousItemID()
	if !rc.outOfBand {
		s.lastItemID = itemID
	}
	// GA mirrors a response output item into the conversation: it announces
	// conversation.item.added when generation of the item starts (in_progress)
	// and conversation.item.done when it is finalized, alongside the
	// response.output_item.* events — emitted here directly after each
	// response.output_item counterpart.
	inProgress := map[string]any{
		"id": itemID, "object": "realtime.item", "type": "message", "status": "in_progress",
		"role": "assistant", "content": []any{}}

	if rc.textOnly() {
		final := s.rememberItem(map[string]any{
			"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
			"role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": transcript}},
		})
		// NB: the content_part events' part uses the SHORT type names ("text"/
		// "audio", per the GA Part type on ResponseContentPartAdded/DoneEvent) —
		// only ITEM content uses "output_text"/"output_audio". The GA API is
		// asymmetric here; don't "fix" one to match the other.
		*out = append(*out, Event{"type": "response.output_item.added", "response_id": respID, "output_index": outputIndex, "item": inProgress})
		if !rc.outOfBand {
			*out = append(*out, Event{"type": "conversation.item.added", "previous_item_id": prevItem, "item": inProgress})
		}
		*out = append(*out,
			Event{"type": "response.content_part.added", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "part": map[string]any{"type": "text", "text": ""}},
		)
		for _, chunk := range chunkText(transcript) {
			*out = append(*out, Event{"type": "response.output_text.delta", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "delta": chunk})
		}
		*out = append(*out,
			Event{"type": "response.output_text.done", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "text": transcript},
			Event{"type": "response.content_part.done", "response_id": respID, "item_id": itemID,
				"output_index": outputIndex, "content_index": 0, "part": map[string]any{"type": "text", "text": transcript}},
			Event{"type": "response.output_item.done", "response_id": respID, "output_index": outputIndex, "item": final},
		)
		if !rc.outOfBand {
			*out = append(*out, Event{"type": "conversation.item.done", "previous_item_id": prevItem, "item": final})
		}
		return final
	}

	// GA audio ladder: assistant audio is the "output_audio" content part, and the
	// streamed events are response.output_audio*.delta/.done (not the beta
	// response.audio*.delta names).
	final := s.rememberItem(map[string]any{
		"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
		"role": "assistant", "content": []any{map[string]any{"type": "output_audio", "transcript": transcript}},
	})
	*out = append(*out, Event{"type": "response.output_item.added", "response_id": respID, "output_index": outputIndex, "item": inProgress})
	if !rc.outOfBand {
		*out = append(*out, Event{"type": "conversation.item.added", "previous_item_id": prevItem, "item": inProgress})
	}
	*out = append(*out,
		// Short part type on content_part events ("audio"), output_audio only on
		// item content — see the note in the text branch.
		Event{"type": "response.content_part.added", "response_id": respID, "item_id": itemID,
			"output_index": outputIndex, "content_index": 0, "part": map[string]any{"type": "audio", "transcript": ""}},
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
			"output_index": outputIndex, "content_index": 0, "part": map[string]any{"type": "audio", "transcript": transcript}},
		Event{"type": "response.output_item.done", "response_id": respID, "output_index": outputIndex, "item": final},
	)
	if !rc.outOfBand {
		*out = append(*out, Event{"type": "conversation.item.done", "previous_item_id": prevItem, "item": final})
	}
	return final
}

// appendFunctionCallLadder emits a function_call output item and its streamed
// argument events (response.function_call_arguments.delta/.done), returning the
// completed item. This is what a Realtime client's tool loop consumes: it reads
// call_id + name + assembled arguments, runs the tool, and sends the result back
// as a conversation.item.create of type function_call_output.
func (s *Session) appendFunctionCallLadder(out *[]Event, respID string, tc types.ToolCallSpec, outputIndex int, rc *responseCtx) map[string]any {
	itemID := s.nextID("fc")
	callID := s.nextID("call")
	// A function_call output item joins the conversation: capture what it chains
	// off, then record it so a later user turn's previous_item_id points at it.
	// (Out-of-band responses join nothing — see appendMessageLadder.)
	prevItem := s.previousItemID()
	if !rc.outOfBand {
		s.lastItemID = itemID
	}
	// raw_arguments lets a scenario plant malformed/invalid JSON args verbatim
	// (FB-03) to exercise a client's tool-arg parser; otherwise marshal the
	// structured Arguments. Mirrors adapter/openai.go and the streaming paths.
	args := tc.RawArguments
	if args == "" {
		args = marshalArgs(tc.Arguments)
	}

	inProgress := map[string]any{
		"id": itemID, "object": "realtime.item", "type": "function_call", "status": "in_progress",
		"name": tc.Name, "call_id": callID, "arguments": ""}
	final := s.rememberItem(map[string]any{
		"id": itemID, "object": "realtime.item", "type": "function_call", "status": "completed",
		"name": tc.Name, "call_id": callID, "arguments": args,
	})
	// GA mirrors the item into the conversation (added at generation start, done
	// when finalized) alongside the response.output_item.* events.
	*out = append(*out, Event{"type": "response.output_item.added", "response_id": respID, "output_index": outputIndex, "item": inProgress})
	if !rc.outOfBand {
		*out = append(*out, Event{"type": "conversation.item.added", "previous_item_id": prevItem, "item": inProgress})
	}
	// NB: no content_index on the function_call_arguments events — a function_call
	// item has no content parts, and the GA SDK types
	// (ResponseFunctionCallArgumentsDelta/DoneEvent) carry only call_id/delta/
	// arguments/item_id/output_index/response_id.
	for _, chunk := range chunkArgs(args) {
		*out = append(*out, Event{"type": "response.function_call_arguments.delta", "response_id": respID, "item_id": itemID,
			"output_index": outputIndex, "call_id": callID, "delta": chunk})
	}
	*out = append(*out,
		Event{"type": "response.function_call_arguments.done", "response_id": respID, "item_id": itemID,
			"output_index": outputIndex, "call_id": callID, "arguments": args},
		Event{"type": "response.output_item.done", "response_id": respID, "output_index": outputIndex, "item": final},
	)
	if !rc.outOfBand {
		*out = append(*out, Event{"type": "conversation.item.done", "previous_item_id": prevItem, "item": final})
	}
	return final
}

// engineHistory is the conversation handed to the engine, prepended with the
// given instructions (the response override, or the session default the caller
// resolved) as a system message when non-empty.
func (s *Session) engineHistory(instructions string) []engine.RequestMessage {
	if instructions == "" {
		return s.history
	}
	out := make([]engine.RequestMessage, 0, len(s.history)+1)
	out = append(out, engine.RequestMessage{Role: "system", Content: instructions})
	return append(out, s.history...)
}

func (s *Session) model() string {
	if s.cfg.model != "" {
		return s.cfg.model
	}
	if s.initialModel != "" {
		return s.initialModel
	}
	return DefaultModel
}

// sessionObject builds the GA Realtime session object: top-level
// output_modalities / instructions / tools / tool_choice / max_output_tokens,
// and a nested `audio` object whose input/output carry the format,
// transcription, turn_detection, voice and speed. Voice lives at
// audio.output.voice (GA), NOT at the top level.
func (s *Session) sessionObject() map[string]any {
	speed := s.cfg.speed
	if speed == 0 {
		speed = 1.0
	}
	obj := map[string]any{
		"id": s.id, "object": "realtime.session", "type": "realtime", "model": s.model(),
		"output_modalities":   s.outputModalities(),
		"instructions":        s.cfg.instructions,
		"tools":               rawOr(s.cfg.tools, "[]"),
		"tool_choice":         rawOr(s.cfg.toolChoice, `"auto"`),
		"max_output_tokens":   rawOr(s.cfg.maxOutputTokens, `"inf"`),
		"tracing":             rawOr(s.cfg.tracing, "null"),
		"truncation":          rawOr(s.cfg.truncation, `"auto"`),
		"prompt":              rawOr(s.cfg.prompt, "null"),
		"include":             rawOr(s.cfg.include, "null"),
		"parallel_tool_calls": rawOr(s.cfg.parallelToolCalls, "true"),
		"audio": map[string]any{
			"input": map[string]any{
				"format":          rawOr(s.cfg.inputFormat, defaultAudioFormat),
				"transcription":   rawOr(s.cfg.transcription, "null"),
				"turn_detection":  rawOr(s.cfg.turnDetection, "null"),
				"noise_reduction": rawOr(s.cfg.noiseReduction, "null"),
			},
			"output": map[string]any{
				"format": rawOr(s.cfg.outputFormat, defaultAudioFormat),
				"voice":  s.effectiveVoice(),
				"speed":  speed,
			},
		},
	}
	if s.expiresAt > 0 {
		obj["expires_at"] = s.expiresAt
	}
	return obj
}

// effectiveVoice is the session voice with the GA default applied; shared by
// the session object and the response envelope's audio block.
func (s *Session) effectiveVoice() string {
	if s.cfg.voice != "" {
		return s.cfg.voice
	}
	return "alloy"
}

// outputModalities resolves the effective response modalities. The GA default
// is ["audio"] — per the GA types, output_modalities is only ever ["audio"] OR
// ["text"], never both (audio output always includes a text transcript).
// (applyConfig already folds the beta `modalities` alias into this field.)
func (s *Session) outputModalities() []string {
	if len(s.cfg.outputModalities) > 0 {
		return s.cfg.outputModalities
	}
	return []string{"audio"}
}

// transcriptionEnabled reports whether the client configured input audio
// transcription (a non-null value, from GA audio.input.transcription or the beta
// top-level input_audio_transcription).
func (s *Session) transcriptionEnabled() bool {
	raw := strings.TrimSpace(string(s.cfg.transcription))
	return raw != "" && raw != "null"
}

// applyConfig merges an inbound session.update payload into the effective
// session config, accepting both the GA nested (audio.*) and beta flat shapes.
func (s *Session) applyConfig(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var u sessionUpdate
	if err := json.Unmarshal(raw, &u); err != nil {
		return
	}
	if u.Model != "" {
		s.cfg.model = u.Model
	}
	if u.Instructions != "" {
		s.cfg.instructions = u.Instructions
	}
	if u.Voice != "" { // beta top-level
		s.cfg.voice = u.Voice
	}
	if len(u.OutputModalities) > 0 {
		s.cfg.outputModalities = u.OutputModalities
	} else if len(u.Modalities) > 0 { // beta alias
		s.cfg.outputModalities = u.Modalities
	}
	if len(u.InputAudioTranscription) > 0 { // beta top-level
		s.cfg.transcription = u.InputAudioTranscription
	}
	if len(u.Tools) > 0 {
		s.cfg.tools = u.Tools
	}
	if len(u.ToolChoice) > 0 {
		s.cfg.toolChoice = u.ToolChoice
	}
	if len(u.MaxOutputTokens) > 0 {
		s.cfg.maxOutputTokens = u.MaxOutputTokens
	}
	if len(u.Tracing) > 0 {
		s.cfg.tracing = u.Tracing
	}
	if len(u.Truncation) > 0 {
		s.cfg.truncation = u.Truncation
	}
	if len(u.Prompt) > 0 {
		s.cfg.prompt = u.Prompt
	}
	if len(u.Include) > 0 {
		s.cfg.include = u.Include
	}
	if len(u.ParallelToolCalls) > 0 {
		s.cfg.parallelToolCalls = u.ParallelToolCalls
	}
	// Beta-flat aliases (GA nested wins when both are present).
	if len(u.TurnDetection) > 0 && (u.Audio == nil || u.Audio.Input == nil || len(u.Audio.Input.TurnDetection) == 0) {
		s.cfg.turnDetection = u.TurnDetection
	}
	if f := betaAudioFormat(u.InputAudioFormat); f != nil && (u.Audio == nil || u.Audio.Input == nil || len(u.Audio.Input.Format) == 0) {
		s.cfg.inputFormat = f
	}
	if f := betaAudioFormat(u.OutputAudioFormat); f != nil && (u.Audio == nil || u.Audio.Output == nil || len(u.Audio.Output.Format) == 0) {
		s.cfg.outputFormat = f
	}
	if u.Audio != nil {
		if in := u.Audio.Input; in != nil {
			if len(in.Transcription) > 0 {
				s.cfg.transcription = in.Transcription
			}
			if len(in.TurnDetection) > 0 {
				s.cfg.turnDetection = in.TurnDetection
			}
			if len(in.Format) > 0 {
				s.cfg.inputFormat = in.Format
			}
			if len(in.NoiseReduction) > 0 {
				s.cfg.noiseReduction = in.NoiseReduction
			}
		}
		if o := u.Audio.Output; o != nil {
			if o.Voice != "" {
				s.cfg.voice = o.Voice
			}
			if len(o.Format) > 0 {
				s.cfg.outputFormat = o.Format
			}
			if o.Speed != nil {
				s.cfg.speed = *o.Speed
			}
		}
	}
}

// errorEvent builds a client-error event. The GA error object carries five
// fields: type, code, message, param (null unless a specific field is at
// fault), and event_id — the id of the CLIENT event that caused the error,
// which SDKs use to correlate errors to their requests.
func (s *Session) errorEvent(code, msg string) Event {
	return Event{"type": "error", "error": s.errorBody("invalid_request_error", code, msg)}
}

// errorEventParam is errorEvent with the GA `param` field naming the offending
// request field (config-validation rejections point at the exact path).
func (s *Session) errorEventParam(code, msg, param string) Event {
	body := s.errorBody("invalid_request_error", code, msg)
	body["param"] = param
	return Event{"type": "error", "error": body}
}

// errorBody builds the GA error object shared by errorEvent and the failed-
// response ladder (which uses type "server_error").
func (s *Session) errorBody(typ, code, msg string) map[string]any {
	var evID any
	if s.lastClientEventID != "" {
		evID = s.lastClientEventID
	}
	return map[string]any{"type": typ, "code": code, "message": msg, "param": nil, "event_id": evID}
}

func (s *Session) nextID(prefix string) string {
	s.counter++
	return fmt.Sprintf("%s_%s_%d", prefix, s.id, s.counter)
}

// --- helpers ---

// parseItem extracts the role and text from a conversation.item.create item. It
// accepts the Realtime content array ([{type:input_text|text, text:"..."}]).
// parsedItem is the decoded subset of a conversation.item.create payload the
// mock acts on, discriminated by Type the same way the Responses adapter's
// responsesItemToMessage is: "message" (role + content text), "function_call"
// (an echoed prior tool call), or "function_call_output" (the tool-loop reply).
type parsedItem struct {
	ID        string `json:"id"` // client-supplied item id ("" → the server mints one)
	Type      string `json:"type"`
	Role      string `json:"role"`
	CallID    string `json:"call_id"`
	Output    string `json:"output"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// text joins the item's content-part texts (the mock's matchable payload).
func (it *parsedItem) text() string {
	var parts []string
	for _, c := range it.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, " ")
}

func parseItem(raw json.RawMessage) parsedItem {
	item := parsedItem{Type: "message", Role: "user"}
	if len(raw) == 0 {
		return item
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return parsedItem{Type: "message", Role: "user"}
	}
	if item.Type == "" {
		item.Type = "message"
	}
	if item.Role == "" {
		item.Role = "user"
	}
	return item
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

// defaultAudioFormat is the GA PCM audio format object used when the client
// hasn't set one (audio.input/output.format).
const defaultAudioFormat = `{"type":"audio/pcm","rate":24000}`

// rawOr returns v if it holds JSON, else the default JSON literal. The result is
// a json.RawMessage so it serializes as raw JSON inside the session object.
func rawOr(v json.RawMessage, def string) json.RawMessage {
	if len(v) > 0 {
		return v
	}
	return json.RawMessage(def)
}

// marshalArgs renders a tool call's arguments as the JSON string the Realtime
// protocol carries in function_call items, defaulting to "{}" for none.
func marshalArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		// A map[string]any of scenario-authored args is effectively always
		// marshalable; fall back to an empty object rather than emitting invalid
		// JSON into the arguments stream. (Use raw_arguments to plant bad JSON.)
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

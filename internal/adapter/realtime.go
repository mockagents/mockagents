package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/realtime"
)

// ProtocolOpenAIRealtime is the wire-protocol label for the OpenAI Realtime API
// surface (NF-01): a WebSocket at GET /v1/realtime plus the ephemeral-token mint
// at POST /v1/realtime/client_secrets (and the legacy /v1/realtime/sessions). It
// is the only WebSocket transport in the mock; the per-event protocol logic and
// deterministic audio synthesis live in internal/realtime, so this file is just
// the socket plumbing (accept → read loop → write the session's events).
const ProtocolOpenAIRealtime = "openai-realtime"

// realtimeReadLimit bounds a single inbound WebSocket frame. Realtime audio is
// sent as base64 input_audio_buffer.append frames, which are larger than the
// coder/websocket default (32 KiB), so the limit is raised — while still capping
// a single frame so a client can't force an unbounded allocation.
const realtimeReadLimit = 16 << 20 // 16 MiB

// realtimePaceInterval is the inter-event delay for paced response emission on
// VAD-enabled sessions (Phase 2): small enough to keep tests fast, non-zero so
// barge-in / response.cancel have a real window to land in.
const realtimePaceInterval = 5 * time.Millisecond

// RealtimeHandler serves the OpenAI Realtime API.
type RealtimeHandler struct {
	Engine *engine.Engine
	// minted retains the session config supplied at ephemeral-key mint time
	// (POST /v1/realtime/client_secrets), keyed by the ek_ value, so a connect
	// presenting that key comes up with the configuration the client paid for
	// — the GA browser flow sets everything at mint and sends no
	// session.update.
	mintedMu sync.Mutex
	minted   map[string]mintedSession
}

type mintedSession struct {
	config    json.RawMessage // the request's session payload (session.update shape)
	expiresAt int64           // unix seconds — resolution fails after expiry
}

// maxMintedSessions bounds the mint store; past the cap new mints are simply
// not retained (the key still works for connecting — it just seeds nothing).
const maxMintedSessions = 1024

func (h *RealtimeHandler) rememberMinted(key string, cfg json.RawMessage, expiresAt int64) {
	if len(cfg) == 0 {
		return
	}
	h.mintedMu.Lock()
	defer h.mintedMu.Unlock()
	if h.minted == nil {
		h.minted = make(map[string]mintedSession)
	}
	now := time.Now().Unix()
	for k, v := range h.minted {
		if v.expiresAt < now {
			delete(h.minted, k)
		}
	}
	if len(h.minted) >= maxMintedSessions {
		return
	}
	h.minted[key] = mintedSession{config: cfg, expiresAt: expiresAt}
}

func (h *RealtimeHandler) mintedConfig(key string) (json.RawMessage, bool) {
	if key == "" {
		return nil, false
	}
	h.mintedMu.Lock()
	defer h.mintedMu.Unlock()
	m, ok := h.minted[key]
	if !ok || m.expiresAt < time.Now().Unix() {
		return nil, false
	}
	return m.config, true
}

// Name identifies this adapter in logs and diagnostics.
func (h *RealtimeHandler) Name() string { return "openai-realtime" }

// Routes returns the Realtime routes mounted through the adapter Registry.
func (h *RealtimeHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/realtime/client_secrets", Handler: h.HandleClientSecret},
		{Pattern: "POST /v1/realtime/sessions", Handler: h.HandleLegacySession},
		{Pattern: "GET /v1/realtime", Handler: h.HandleConnect},
	}
}

// buildEphemeralSession decodes a client_secrets/sessions request and builds
// the ephemeral key plus the effective GA session object (which embeds the key
// at session.client_secret — a required field of the GA response session). The
// mock ignores the key on connect (it accepts any client), so this is a
// well-formed stub the SDK can round-trip.
func (h *RealtimeHandler) buildEphemeralSession(r *http.Request) (value string, expiresAt int64, session map[string]any) {
	var outer struct {
		Model        string `json:"model"` // legacy flat
		Voice        string `json:"voice"` // legacy flat
		ExpiresAfter *struct {
			Anchor  string `json:"anchor"` // only "created_at" is defined
			Seconds int    `json:"seconds"`
		} `json:"expires_after"`
		// Kept raw: the verbatim session payload is retained per minted key so
		// a connect presenting the key can be seeded with it (SeedConfig
		// accepts the session.update shape, which this is).
		Session json.RawMessage `json:"session"`
	}
	_ = decodeJSONBody(r, &outer) // an empty/invalid body is tolerated (all fields optional)
	defer r.Body.Close()
	var body struct {
		Model string `json:"model"`
		Voice string `json:"voice"`
		// Legacy aliases live on the outer struct.
		Session struct {
			Type             string          `json:"type"`
			Model            string          `json:"model"`
			Voice            string          `json:"voice"` // beta nested-flat
			Instructions     string          `json:"instructions"`
			OutputModalities []string        `json:"output_modalities"`
			Tools            json.RawMessage `json:"tools"`
			ToolChoice       json.RawMessage `json:"tool_choice"`
			Audio            *struct {
				Input *struct {
					TurnDetection json.RawMessage `json:"turn_detection"`
				} `json:"input"`
				Output *struct {
					Voice string `json:"voice"`
				} `json:"output"`
			} `json:"audio"`
		} `json:"session"`
	}
	body.Model, body.Voice = outer.Model, outer.Voice
	if len(outer.Session) > 0 {
		_ = json.Unmarshal(outer.Session, &body.Session)
	}

	model := firstNonEmpty(body.Model, body.Session.Model, realtime.DefaultModel)
	// GA nests the voice at session.audio.output.voice; the beta nested-flat and
	// legacy flat spellings stay accepted.
	gaVoice := ""
	var turnDetection any
	if a := body.Session.Audio; a != nil {
		if a.Output != nil {
			gaVoice = a.Output.Voice
		}
		if a.Input != nil && len(a.Input.TurnDetection) > 0 {
			turnDetection = a.Input.TurnDetection
		}
	}
	voice := firstNonEmpty(gaVoice, body.Session.Voice, body.Voice, "alloy")

	// GA expires_after: {anchor:"created_at", seconds: 10..7200}, default 600 s.
	expiresIn := 600 * time.Second
	if outer.ExpiresAfter != nil && outer.ExpiresAfter.Seconds > 0 {
		expiresIn = time.Duration(min(max(outer.ExpiresAfter.Seconds, 10), 7200)) * time.Second
	}
	value = "ek_" + generateID()
	expiresAt = time.Now().Add(expiresIn).Unix()
	h.rememberMinted(value, outer.Session, expiresAt)

	// session is a GA discriminated union: type "transcription" mints an
	// input-transcription-only session (no output side, no tools).
	if body.Session.Type == "transcription" {
		session = map[string]any{
			"id":            "sess_" + generateID(),
			"object":        "realtime.transcription_session",
			"type":          "transcription",
			"include":       nil,
			"client_secret": map[string]any{"value": value, "expires_at": expiresAt},
			"audio": map[string]any{
				"input": map[string]any{
					"format": map[string]any{"type": "audio/pcm", "rate": 24000}, "transcription": nil,
					"turn_detection": turnDetection, "noise_reduction": nil,
				},
			},
		}
		return value, expiresAt, session
	}

	mods := body.Session.OutputModalities
	if len(mods) == 0 {
		mods = []string{"audio"} // GA default: ["audio"] or ["text"], never both
	}
	var tools any = []any{}
	if len(body.Session.Tools) > 0 {
		tools = body.Session.Tools
	}
	var toolChoice any = "auto"
	if len(body.Session.ToolChoice) > 0 {
		toolChoice = body.Session.ToolChoice
	}
	pcm := func() map[string]any { return map[string]any{"type": "audio/pcm", "rate": 24000} }

	session = map[string]any{
		// GA session shape: type:"realtime", voice nested under audio.output,
		// and the ephemeral key mirrored at client_secret (required by the GA
		// response session type).
		"id":                "sess_" + generateID(),
		"object":            "realtime.session",
		"type":              "realtime",
		"model":             model,
		"output_modalities": mods,
		"instructions":      body.Session.Instructions,
		"tools":             tools,
		"tool_choice":       toolChoice,
		"max_output_tokens": "inf",
		"client_secret":     map[string]any{"value": value, "expires_at": expiresAt},
		"audio": map[string]any{
			"input": map[string]any{
				"format": pcm(), "transcription": nil,
				"turn_detection": turnDetection, "noise_reduction": nil,
			},
			"output": map[string]any{"voice": voice, "format": pcm(), "speed": 1.0},
		},
	}
	return value, expiresAt, session
}

// HandleClientSecret serves the GA POST /v1/realtime/client_secrets envelope:
// {value, expires_at, session}.
func (h *RealtimeHandler) HandleClientSecret(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIRealtime)
	value, expiresAt, session := h.buildEphemeralSession(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"value": value, "expires_at": expiresAt, "session": session,
	})
}

// HandleLegacySession serves the pre-GA POST /v1/realtime/sessions shape: the
// BETA session object itself — top-level modalities / voice /
// input_audio_format etc., with the ephemeral key nested at
// session.client_secret (not the GA {value, expires_at, session} envelope).
// Beta ephemeral keys "expire one minute after issuance", so the default
// expiry here is 60 s (the GA client_secrets default is 600 s).
func (h *RealtimeHandler) HandleLegacySession(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIRealtime)
	var body struct {
		Model                   string          `json:"model"`
		Voice                   string          `json:"voice"`
		Instructions            string          `json:"instructions"`
		Modalities              []string        `json:"modalities"`
		InputAudioFormat        string          `json:"input_audio_format"`
		OutputAudioFormat       string          `json:"output_audio_format"`
		InputAudioTranscription json.RawMessage `json:"input_audio_transcription"`
		TurnDetection           json.RawMessage `json:"turn_detection"`
		Tools                   json.RawMessage `json:"tools"`
		ToolChoice              json.RawMessage `json:"tool_choice"`
		Temperature             *float64        `json:"temperature"`
		MaxResponseOutputTokens json.RawMessage `json:"max_response_output_tokens"`
		Session                 struct {
			Model string `json:"model"` // tolerated GA-style nesting
			Voice string `json:"voice"`
		} `json:"session"`
	}
	_ = decodeJSONBody(r, &body) // an empty/invalid body is tolerated (all fields optional)
	defer r.Body.Close()

	orRaw := func(raw json.RawMessage, def any) any {
		if len(raw) > 0 {
			return raw
		}
		return def
	}
	mods := body.Modalities
	if len(mods) == 0 {
		mods = []string{"audio", "text"} // beta default: both
	}
	temperature := 0.8
	if body.Temperature != nil {
		temperature = *body.Temperature
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                         "sess_" + generateID(),
		"object":                     "realtime.session",
		"model":                      firstNonEmpty(body.Model, body.Session.Model, realtime.DefaultModel),
		"modalities":                 mods,
		"instructions":               body.Instructions,
		"voice":                      firstNonEmpty(body.Voice, body.Session.Voice, "alloy"),
		"input_audio_format":         firstNonEmpty(body.InputAudioFormat, "pcm16"),
		"output_audio_format":        firstNonEmpty(body.OutputAudioFormat, "pcm16"),
		"input_audio_transcription":  orRaw(body.InputAudioTranscription, nil),
		"turn_detection":             orRaw(body.TurnDetection, nil),
		"tools":                      orRaw(body.Tools, []any{}),
		"tool_choice":                orRaw(body.ToolChoice, "auto"),
		"temperature":                temperature,
		"max_response_output_tokens": orRaw(body.MaxResponseOutputTokens, "inf"),
		"client_secret": map[string]any{
			"value": "ek_" + generateID(), "expires_at": time.Now().Add(60 * time.Second).Unix(),
		},
	})
}

// HandleConnect upgrades GET /v1/realtime to a WebSocket and runs the session:
// emit session.created, then loop reading client events and writing back the
// server events the session produces.
func (h *RealtimeHandler) HandleConnect(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIRealtime)

	// A GA browser client presents its ephemeral key either as a bearer token
	// or embedded in a subprotocol offer ("openai-insecure-api-key.<key>").
	// Extract it before Accept so (a) the offered subprotocol can be accepted
	// and (b) the minted session config can seed the session below.
	ephemeralKey := ""
	if bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); strings.HasPrefix(bearer, "ek_") {
		ephemeralKey = bearer
	}
	subprotocols := []string{"realtime"}
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(header, ",") {
			p = strings.TrimSpace(p)
			if token, ok := strings.CutPrefix(p, "openai-insecure-api-key."); ok {
				if ephemeralKey == "" {
					ephemeralKey = token
				}
				subprotocols = append(subprotocols, p)
			}
		}
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The real Realtime API negotiates the "realtime" subprotocol (plus the
		// browser key-bearing offers accepted above); a mock accepts any origin.
		Subprotocols:       subprotocols,
		InsecureSkipVerify: true,
	})
	if err != nil {
		return // Accept already wrote the HTTP error
	}
	defer c.CloseNow()
	c.SetReadLimit(realtimeReadLimit)

	ctx := r.Context()
	tenant := engine.TenantIDFromContext(ctx)
	sess := realtime.NewSession("sess_"+generateID(), r.URL.Query().Get("model"), h.generator(tenant))
	sess.SetExpiry(time.Now().Add(time.Hour).Unix()) // reported as session.expires_at
	// ?intent=transcription connects an input-transcription-only session (a
	// session.update {type:"transcription"} reaches the same state).
	if r.URL.Query().Get("intent") == "transcription" {
		sess.SetSessionType("transcription")
	}
	// A connect presenting a minted ephemeral key comes up with the session
	// config supplied at mint time — the GA browser flow configures everything
	// at POST /v1/realtime/client_secrets and sends no session.update.
	if cfg, ok := h.mintedConfig(ephemeralKey); ok {
		sess.SeedConfig(cfg)
	}
	// Paced emission (Phase 2): responses on VAD-enabled sessions stream their
	// ladder incrementally, creating the interruption window barge-in and
	// response.cancel act in. Burst behavior is unchanged for non-VAD sessions.
	sess.SetPacing(realtimePaceInterval)

	for _, ev := range sess.Greeting() {
		if writeEvent(ctx, c, ev) != nil {
			return
		}
	}

	// The Session is single-goroutine: only this loop touches it. A reader
	// goroutine feeds frames through a channel so the loop can select between
	// the client's next event and the session's next deadline (paced response
	// emission, idle timeout).
	done := make(chan struct{})
	defer close(done)
	frames := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				readErr <- err // client closed, read limit exceeded, or context cancelled
				return
			}
			if typ != websocket.MessageText {
				continue // the Realtime protocol is JSON text frames
			}
			select {
			case frames <- data:
			case <-done:
				return
			}
		}
	}()

	for {
		var timerC <-chan time.Time
		var timer *time.Timer
		if deadline, ok := sess.NextDeadline(); ok {
			timer = time.NewTimer(time.Until(deadline))
			timerC = timer.C
		}

		var events []realtime.Event
		select {
		case data := <-frames:
			var ce realtime.ClientEvent
			if err := json.Unmarshal(data, &ce); err != nil {
				// GA error object shape: param is null and event_id (the offending
				// client event's id) is unknowable for a body that didn't parse.
				events = []realtime.Event{{"type": "error", "event_id": "event_" + generateID(), "error": map[string]any{
					"type": "invalid_request_error", "message": "event is not valid JSON", "param": nil, "event_id": nil}}}
			} else {
				events = sess.Handle(ctx, &ce)
			}
		case now := <-timerC:
			events = sess.Tick(ctx, now)
		case <-readErr:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		}
		if timer != nil {
			timer.Stop()
		}
		for _, ev := range events {
			if writeEvent(ctx, c, ev) != nil {
				return
			}
		}
	}
}

// generator adapts the engine to the realtime.Generator signature, pinning the
// connection's tenant onto each sub-request's context.
func (h *RealtimeHandler) generator(tenant string) realtime.Generator {
	return func(ctx context.Context, model, sessionID string, history []engine.RequestMessage) (*engine.Response, error) {
		ctx = engine.WithTenantID(ctx, tenant)
		return h.Engine.ProcessRequestContext(ctx, &engine.InboundRequest{
			Model:     model,
			SessionID: sessionID,
			Messages:  history,
		})
	}
}

func writeEvent(ctx context.Context, c *websocket.Conn, ev realtime.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, data)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

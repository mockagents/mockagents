package adapter

import (
	"context"
	"encoding/json"
	"net/http"
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
}

// Name identifies this adapter in logs and diagnostics.
func (h *RealtimeHandler) Name() string { return "openai-realtime" }

// Routes returns the Realtime routes mounted through the adapter Registry.
func (h *RealtimeHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/realtime/client_secrets", Handler: h.HandleClientSecret},
		{Pattern: "POST /v1/realtime/sessions", Handler: h.HandleClientSecret},
		{Pattern: "GET /v1/realtime", Handler: h.HandleConnect},
	}
}

// HandleClientSecret mints an ephemeral Realtime session token. A browser client
// fetches this from its backend, then opens the WebSocket with it. The mock
// ignores the value on connect (it accepts any client), so it is just a
// well-formed stub the SDK can round-trip.
func (h *RealtimeHandler) HandleClientSecret(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIRealtime)

	var body struct {
		Model   string `json:"model"`
		Voice   string `json:"voice"`
		Session struct {
			Model string `json:"model"`
			Voice string `json:"voice"`
		} `json:"session"`
	}
	_ = decodeJSONBody(r, &body) // an empty/invalid body is tolerated (all fields optional)
	defer r.Body.Close()

	model := firstNonEmpty(body.Model, body.Session.Model, realtime.DefaultModel)
	voice := firstNonEmpty(body.Voice, body.Session.Voice, "alloy")

	writeJSON(w, http.StatusOK, map[string]any{
		"value":      "ek_" + generateID(),
		"expires_at": time.Now().Add(60 * time.Second).Unix(),
		"session": map[string]any{
			// GA session shape: type:"realtime", output_modalities, and voice nested
			// under audio.output (not a top-level field).
			"id":                "sess_" + generateID(),
			"object":            "realtime.session",
			"type":              "realtime",
			"model":             model,
			"output_modalities": []string{"audio"}, // GA default: ["audio"] or ["text"], never both
			"audio": map[string]any{
				"output": map[string]any{"voice": voice},
			},
		},
	})
}

// HandleConnect upgrades GET /v1/realtime to a WebSocket and runs the session:
// emit session.created, then loop reading client events and writing back the
// server events the session produces.
func (h *RealtimeHandler) HandleConnect(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIRealtime)

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The real Realtime API negotiates the "realtime" subprotocol; offer it so
		// an SDK client that requires it connects. A mock accepts any origin.
		Subprotocols:       []string{"realtime"},
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

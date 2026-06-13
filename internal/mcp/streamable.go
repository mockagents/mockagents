package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Streamable HTTP transport (MCP revision 2025-11-25).
//
// This is the single-endpoint transport the current MCP spec mandates: one URL
// (conventionally `/mcp`) answers POST, GET, and DELETE instead of the legacy
// "HTTP+SSE" pair (separate `GET /sse` + `POST /messages`). The behaviour the
// handler implements:
//
//   - POST — the client sends one JSON-RPC message. A request (has an `id`) is
//     answered either with a single `application/json` body or, when the client
//     advertises `Accept: text/event-stream`, with a short SSE stream that
//     carries any server notifications emitted while handling the request and
//     then the JSON-RPC response as the final `message` event before closing.
//     A notification or response (no `id`) is acknowledged with `202 Accepted`
//     and no body.
//   - GET — opens a long-lived SSE stream for server→client messages
//     (notifications, list-changed events). The stream is resumable: every
//     event carries a monotonic `id:` and a reconnecting client replays missed
//     events by sending `Last-Event-ID`.
//   - DELETE — terminates the session.
//
// Session lifecycle is keyed on the `Mcp-Session-Id` header: the server mints an
// id on `initialize` and returns it on that response; every later request must
// echo it or the server answers `404 Not Found` so the client knows to start a
// new session. The `MCP-Protocol-Version` header is validated on post-init
// requests, and the `Origin` header is checked to defend against DNS-rebinding
// (a disallowed origin gets `403 Forbidden`).
//
// The legacy POST-only HTTPHandler and the v0.3 bidirectional SSE endpoints are
// left in place for backward compatibility; this handler is additive and is the
// new default mount for `mockagents mcp --transport http`.

const (
	// headerSessionID carries the per-session id minted on initialize.
	headerSessionID = "Mcp-Session-Id"
	// headerProtocolVersion is the negotiated MCP revision the client must
	// send on every request after initialize.
	headerProtocolVersion = "MCP-Protocol-Version"
	// headerLastEventID is the standard SSE resumption header.
	headerLastEventID = "Last-Event-Id"

	// maxStreamableBodyBytes caps a single POST body (SEC-01 parity with the
	// legacy handler) so a hostile client can't drive an unbounded read.
	maxStreamableBodyBytes = 1 << 20 // 1 MiB
	// maxSessionLogEvents bounds the per-session replay buffer so a long-lived
	// session can't grow without limit; older events fall out of the resume
	// window (a reconnecting client past the window simply restarts).
	maxSessionLogEvents = 1024
	// defaultMaxSessions bounds the live session table (FIFO eviction).
	defaultMaxSessions = 256
	// maxSubscribersPerSession bounds concurrent GET streams sharing one session
	// so a single session id can't be used to exhaust goroutines/memory.
	maxSubscribersPerSession = 64
	// subscriberChanBuffer is the per-GET-stream event channel depth; a slower
	// consumer that overflows it recovers missed events via Last-Event-Id.
	subscriberChanBuffer = 64
	// streamHeartbeatInterval keeps idle SSE connections alive through proxies.
	streamHeartbeatInterval = 15 * time.Second
)

// SupportedProtocolVersions are the MCP revisions the streamable transport
// accepts in the MCP-Protocol-Version header, newest first. The newest entry
// is also types.DefaultMCPProtocolVersion (advertised on initialize).
var SupportedProtocolVersions = []string{
	"2025-11-25",
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

func protocolVersionSupported(v string) bool {
	for _, s := range SupportedProtocolVersions {
		if s == v {
			return true
		}
	}
	return false
}

// StreamableHTTPHandler serves the MCP Streamable HTTP transport for a single
// Server (one MCPServer definition). Mount it at `/mcp`.
type StreamableHTTPHandler struct {
	Server   *Server
	sessions *sessionManager

	// AllowedOrigins is an explicit allowlist of Origin header values. Loopback
	// origins (localhost / 127.0.0.1 / [::1]) are always allowed; a request
	// with no Origin header (non-browser client) is also allowed. Set a single
	// "*" entry to disable the check entirely.
	AllowedOrigins []string

	// HeartbeatInterval overrides the 15s GET-stream keepalive (tests use a
	// short value). Zero uses the default.
	HeartbeatInterval time.Duration
}

// NewStreamableHTTPHandler returns a ready-to-mount streamable handler.
func NewStreamableHTTPHandler(s *Server) *StreamableHTTPHandler {
	return &StreamableHTTPHandler{
		Server:   s,
		sessions: newSessionManager(defaultMaxSessions),
	}
}

// ServeHTTP implements http.Handler, dispatching on method.
func (h *StreamableHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.originAllowed(r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// originAllowed implements the DNS-rebinding guard. An empty Origin (non-browser
// client) is allowed; loopback hosts are always allowed; everything else must be
// in AllowedOrigins (or AllowedOrigins must contain "*").
func (h *StreamableHTTPHandler) originAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	for _, o := range h.AllowedOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return isLoopbackOrigin(origin)
}

// isLoopbackOrigin reports whether origin's host is a loopback address. A
// browser only ever sends a syntactically valid absolute Origin, so a value we
// can't parse is treated as untrusted.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// acceptsEventStream reports whether the client's Accept header opts into SSE.
func acceptsEventStream(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// validateProtocolVersion checks the MCP-Protocol-Version header on a post-init
// request. An absent header is tolerated (assume the default revision for
// backward compatibility); a present-but-unsupported value is rejected. Returns
// false after having already written the 400 response.
func validateProtocolVersion(w http.ResponseWriter, r *http.Request) bool {
	v := r.Header.Get(headerProtocolVersion)
	if v == "" {
		return true
	}
	if protocolVersionSupported(v) {
		return true
	}
	http.Error(w, fmt.Sprintf("unsupported MCP-Protocol-Version %q", v), http.StatusBadRequest)
	return false
}

// isInitialize reports whether body is an `initialize` request without decoding
// the whole payload twice.
func isInitialize(req *Request) bool {
	return req.Method == "initialize"
}

func (h *StreamableHTTPHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxStreamableBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "reading request: "+err.Error(), readCapStatus(err))
		return
	}
	defer r.Body.Close()

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		// Malformed JSON: answer with a JSON-RPC parse error so a client gets a
		// structured failure rather than a bare 400.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(newError(nil, ErrParseError, "invalid JSON", err.Error()))
		return
	}

	// Every request other than `initialize` must carry a valid session. An
	// absent header is a 400 (client bug); a present-but-unknown id is a 404 so
	// a client whose session expired knows to reinitialize (per spec).
	init := isInitialize(&req)
	if !init {
		if !validateProtocolVersion(w, r) {
			return
		}
		id := r.Header.Get(headerSessionID)
		if id == "" {
			http.Error(w, "missing Mcp-Session-Id", http.StatusBadRequest)
			return
		}
		if _, ok := h.sessions.get(id); !ok {
			http.Error(w, "no valid session; reinitialize", http.StatusNotFound)
			return
		}
	}

	// Dispatch the single decoded request once (no second JSON parse).
	resp := h.Server.Handle(&req)

	if resp == nil {
		// The inbound message was a notification or response: ack with 202 and
		// no body.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// A well-formed initialize mints the session; a malformed one (e.g. wrong
	// jsonrpc version → resp.Error) does not, so a client can't spend sessions
	// on garbage payloads.
	if init && resp.Error == nil {
		sess := h.sessions.create()
		w.Header().Set(headerSessionID, sess.id)
	}

	out, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "encoding response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if acceptsEventStream(r) {
		writePostSSE(w, out)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// writePostSSE answers a POST request as a short SSE stream carrying the single
// JSON-RPC response, then closing. POST-stream events are deliberately id-less:
// they are request-correlated and not part of the resumable server→client event
// log (only the GET stream is resumable), so they share no id namespace with it.
func writePostSSE(w http.ResponseWriter, response []byte) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// No streaming support: degrade to a plain JSON response so the client
		// still gets its answer.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", response)
	flusher.Flush()
}

func (h *StreamableHTTPHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if !validateProtocolVersion(w, r) {
		return
	}
	sess, ok := h.sessions.get(r.Header.Get(headerSessionID))
	if !ok {
		http.Error(w, "no valid session; reinitialize", http.StatusNotFound)
		return
	}
	if !acceptsEventStream(r) {
		http.Error(w, "GET requires Accept: text/event-stream", http.StatusNotAcceptable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	var after int64
	if v := r.Header.Get(headerLastEventID); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			after = n
		}
	}

	// Subscribe (and snapshot the replay set) BEFORE committing the 200 status
	// so an at-capacity session can still be rejected with a 429.
	replay, ch, cancel, atCap := sess.subscribe(after)
	if atCap {
		http.Error(w, "too many concurrent streams for this session", http.StatusTooManyRequests)
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Replay any events the client missed (resumability).
	for _, ev := range replay {
		if writeSSEEvent(w, ev.id, ev.data) != nil {
			return
		}
	}
	flusher.Flush()

	heartbeat := h.HeartbeatInterval
	if heartbeat <= 0 {
		heartbeat = streamHeartbeatInterval
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := io.WriteString(w, ":heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return // session terminated
			}
			if writeSSEEvent(w, ev.id, ev.data) != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *StreamableHTTPHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get(headerSessionID)
	if !h.sessions.delete(id) {
		http.Error(w, "no valid session", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Broadcast pushes a server-initiated notification to every live session's GET
// stream (and replay buffer). This is the out-of-band path used by the admin
// notify endpoint and by list-changed events; synchronous, request-correlated
// notifications instead ride the originating POST response.
func (h *StreamableHTTPHandler) Broadcast(n *Notification) {
	h.sessions.broadcast(n)
}

// StreamableNotifyHandler is the streamable-transport companion to the legacy
// NotifyHandler: a small admin endpoint that lets a test harness (or an
// operator) push a server-initiated notification onto every live session's GET
// stream from outside the process. POST a `{"method": "...", "params": {...}}`
// body. Unlike the legacy NotifyHandler (which feeds the v0.3 bidirectional
// queue via Server.EmitNotification), this one fans out through the streamable
// sessions so a GET subscriber on `/mcp` observes the event.
type StreamableNotifyHandler struct {
	H *StreamableHTTPHandler
}

// NewStreamableNotifyHandler binds the admin endpoint to a streamable handler.
func NewStreamableNotifyHandler(h *StreamableHTTPHandler) *StreamableNotifyHandler {
	return &StreamableNotifyHandler{H: h}
}

// ServeHTTP implements http.Handler.
func (h *StreamableNotifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Apply the same DNS-rebinding guard as the main endpoint so a browser page
	// can't inject notifications into every live session's stream.
	if !h.H.originAllowed(r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxStreamableBodyBytes)
	var body Notification
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid notification: "+err.Error(), readCapStatus(err))
		return
	}
	if body.Method == "" {
		http.Error(w, "method is required", http.StatusBadRequest)
		return
	}
	h.H.Broadcast(&body)
	w.WriteHeader(http.StatusAccepted)
}

// notificationEnvelope renders a Notification as the JSON-RPC 2.0 wire object a
// client decodes (a request with no id).
func notificationEnvelope(n *Notification) map[string]any {
	env := map[string]any{"jsonrpc": "2.0", "method": n.Method}
	if n.Params != nil {
		env["params"] = n.Params
	}
	return env
}

// writeSSEEvent writes one `id:`/`event:`/`data:` SSE frame. The event name is
// always `message` per the Streamable HTTP spec.
func writeSSEEvent(w io.Writer, id int64, data []byte) error {
	_, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", id, data)
	return err
}

// --- session manager -------------------------------------------------------

// sessionManager holds the live streamable sessions, capped FIFO.
type sessionManager struct {
	mu       sync.Mutex
	sessions map[string]*streamSession
	order    []string
	max      int
}

func newSessionManager(max int) *sessionManager {
	if max <= 0 {
		max = defaultMaxSessions
	}
	return &sessionManager{sessions: make(map[string]*streamSession), max: max}
}

func (m *sessionManager) create() *streamSession {
	s := newStreamSession(newSessionID())
	m.mu.Lock()
	m.sessions[s.id] = s
	m.order = append(m.order, s.id)
	for len(m.order) > m.max {
		evict := m.order[0]
		m.order = m.order[1:]
		if old, ok := m.sessions[evict]; ok {
			delete(m.sessions, evict)
			old.close()
		}
	}
	m.mu.Unlock()
	return s
}

func (m *sessionManager) get(id string) (*streamSession, bool) {
	if id == "" {
		return nil, false
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	return s, ok
}

func (m *sessionManager) delete(id string) bool {
	if id == "" {
		return false
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
		for i, oid := range m.order {
			if oid == id {
				m.order = append(m.order[:i:i], m.order[i+1:]...)
				break
			}
		}
	}
	m.mu.Unlock()
	if ok {
		s.close()
	}
	return ok
}

// broadcast snapshots the live sessions under the lock, then fans the
// notification out without holding it (broadcastNotification takes the per-
// session lock).
func (m *sessionManager) broadcast(n *Notification) {
	m.mu.Lock()
	live := make([]*streamSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		live = append(live, s)
	}
	m.mu.Unlock()
	for _, s := range live {
		s.broadcastNotification(n)
	}
}

// newSessionID returns a 128-bit hex token (visible-ASCII per the spec). A
// crypto/rand read failure is an unrecoverable platform fault (and, since Go
// 1.24, rand.Read never returns an error) — we panic rather than ever issue a
// guessable id.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("mcp: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// --- per-session state -----------------------------------------------------

// loggedEvent is one server→client SSE event retained for resumption.
type loggedEvent struct {
	id   int64
	data []byte
}

// streamSession owns the server→client event log and the live GET subscribers
// for one Mcp-Session-Id.
type streamSession struct {
	id string

	mu     sync.Mutex
	nextID int64
	log    []loggedEvent
	subs   map[chan loggedEvent]struct{}
	closed bool
}

func newStreamSession(id string) *streamSession {
	return &streamSession{
		id:   id,
		subs: make(map[chan loggedEvent]struct{}),
	}
}

// broadcastNotification appends a notification to the resumable log and pushes
// it to every live GET subscriber. A subscriber whose buffer is full is skipped
// (it will replay the event via Last-Event-ID on reconnect).
func (s *streamSession) broadcastNotification(n *Notification) {
	data, err := json.Marshal(notificationEnvelope(n))
	if err != nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.nextID++
	ev := loggedEvent{id: s.nextID, data: data}
	s.log = append(s.log, ev)
	if len(s.log) > maxSessionLogEvents {
		drop := len(s.log) - maxSessionLogEvents
		s.log = append(s.log[:0], s.log[drop:]...)
	}
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
	s.mu.Unlock()
}

// subscribe registers a GET subscriber and returns the events it missed (id >
// after) for immediate replay, the live channel, and a cancel hook. The replay
// snapshot and the registration happen under the same lock so no event can slip
// through the gap. atCap is true when the session already has the maximum
// concurrent streams (the caller should reject with 429) — no subscriber is
// registered in that case. A terminated session returns a closed channel so the
// caller's stream loop exits immediately.
func (s *streamSession) subscribe(after int64) (replay []loggedEvent, ch chan loggedEvent, cancel func(), atCap bool) {
	ch = make(chan loggedEvent, subscriberChanBuffer)
	noop := func() {}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		close(ch)
		return nil, ch, noop, false
	}
	if len(s.subs) >= maxSubscribersPerSession {
		s.mu.Unlock()
		close(ch)
		return nil, ch, noop, true
	}
	for _, ev := range s.log {
		if ev.id > after {
			replay = append(replay, ev)
		}
	}
	s.subs[ch] = struct{}{}
	s.mu.Unlock()

	cancel = func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
		}
		s.mu.Unlock()
	}
	return replay, ch, cancel, false
}

// close terminates the session, closing every live subscriber channel.
func (s *streamSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	for ch := range s.subs {
		close(ch)
		delete(s.subs, ch)
	}
	s.mu.Unlock()
}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// OutboundKind tags an item in the outbound queue so the SSE transport
// can pick the right SSE event name.
type OutboundKind string

const (
	// OutboundRequest is a server-initiated JSON-RPC request — the
	// client is expected to process it and POST back a matching response.
	OutboundRequest OutboundKind = "request"
	// OutboundNotification is a server-initiated notification — fire-
	// and-forget, no correlation id.
	OutboundNotification OutboundKind = "notification"
)

// OutboundMessage is a single item the bidirectional transport writes
// out to a subscribed client. For OutboundRequest items the Request
// field is populated and carries an id; for OutboundNotification items
// the Notification field is populated.
type OutboundMessage struct {
	Kind         OutboundKind
	Request      *Request
	Notification *Notification
}

// bidirectional holds the state that powers server-initiated requests
// (sampling/createMessage, roots/list) and the SSE transport that
// ships them out to subscribed clients.
//
// The map of pending responses is keyed by stringified JSON-RPC id and
// the request id is a monotonic counter so collisions are impossible
// within a single Server lifetime.
type bidirectional struct {
	mu sync.Mutex
	// outbound is the buffered queue. When a subscriber is attached it
	// is additionally signaled via the sub channel; unsubscribed
	// messages stay buffered until someone subscribes.
	outbound []*OutboundMessage
	// pending maps string(id) -> response channel so DeliverResponse
	// can route replies back to the blocked SendRequest caller.
	pending map[string]chan *Response
	// sub is the currently-attached subscriber channel. At most one
	// subscriber is active at any time; attaching a second one closes
	// the first (matching the "new tab steals the SSE stream" pattern
	// that real MCP proxies use).
	sub chan *OutboundMessage

	nextID atomic.Int64
}

func newBidirectional() *bidirectional {
	return &bidirectional{
		pending: make(map[string]chan *Response),
	}
}

// Subscribe attaches a channel that receives every outbound message
// from now on. Any messages queued before the subscription are replayed
// synchronously in FIFO order. Calling Subscribe a second time closes
// the previous subscriber. The returned cancel function detaches the
// subscription and drains any unread messages back into the buffer so
// a reconnecting client does not lose work.
func (b *bidirectional) Subscribe(buffer int) (<-chan *OutboundMessage, func()) {
	if buffer <= 0 {
		buffer = 16
	}
	b.mu.Lock()
	// Steal the previous subscription if one exists.
	if b.sub != nil {
		close(b.sub)
	}
	ch := make(chan *OutboundMessage, buffer)
	b.sub = ch
	// Replay any buffered messages. If the buffer overflows we keep
	// the tail in the outbound slice and deliver it on the next drain.
	drained := 0
	for i, msg := range b.outbound {
		select {
		case ch <- msg:
			drained = i + 1
		default:
			// channel full — stop replaying; whatever is left stays
			// buffered for the next subscriber.
			goto done
		}
	}
done:
	b.outbound = append(b.outbound[:0], b.outbound[drained:]...)
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.sub == ch {
			// Drain anything the subscriber never read and push it
			// back to the head of the queue.
			close(ch)
			var residual []*OutboundMessage
			for m := range ch {
				residual = append(residual, m)
			}
			b.outbound = append(residual, b.outbound...)
			b.sub = nil
		}
	}
	return ch, cancel
}

// enqueue pushes a message to the outbound queue and, if a subscriber
// is attached, delivers it directly. Overflow (subscriber channel
// full) is handled by falling back to the buffered queue so messages
// are never dropped.
func (b *bidirectional) enqueue(msg *OutboundMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sub != nil {
		select {
		case b.sub <- msg:
			return
		default:
			// channel full — fall through and buffer.
		}
	}
	b.outbound = append(b.outbound, msg)
	// Bounded like the pending queue (audit M-25): with no subscriber attached
	// this only ever grew. Drop the oldest so a late subscriber still gets the
	// most recent messages.
	if over := len(b.outbound) - maxPendingNotifications; over > 0 {
		b.outbound = append(b.outbound[:0], b.outbound[over:]...)
	}
}

// newServerID returns the next server-initiated request id as a raw
// JSON number. Using a numeric id matches what real MCP servers do and
// keeps round-trips stable through encoding/decoding.
func (b *bidirectional) newServerID() json.RawMessage {
	n := b.nextID.Add(1)
	return json.RawMessage(strconv.FormatInt(n, 10))
}

// registerPending stores a response channel for the given id and
// returns the channel plus a cleanup hook that removes the entry when
// the caller is done (even on error paths).
func (b *bidirectional) registerPending(id json.RawMessage) (chan *Response, func()) {
	key := string(id)
	ch := make(chan *Response, 1)
	b.mu.Lock()
	b.pending[key] = ch
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.pending, key)
		b.mu.Unlock()
	}
}

// DeliverResponse routes a client-sent response to the SendRequest
// caller blocked on its id. Returns an error when no pending request
// matches (late reply, unknown id, or duplicate delivery).
func (s *Server) DeliverResponse(resp *Response) error {
	if resp == nil {
		return fmt.Errorf("mcp: nil response")
	}
	if len(resp.ID) == 0 {
		return fmt.Errorf("mcp: response missing id")
	}
	key := string(resp.ID)
	s.bi.mu.Lock()
	ch, ok := s.bi.pending[key]
	if ok {
		delete(s.bi.pending, key)
	}
	s.bi.mu.Unlock()
	if !ok {
		return fmt.Errorf("mcp: no pending request for id %s", key)
	}
	// The channel is buffered size 1 so this never blocks; the
	// registerPending cleanup hook is a no-op when the entry was
	// already removed here.
	ch <- resp
	return nil
}

// SendRequest sends a server-initiated JSON-RPC request to whichever
// client is currently subscribed to the SSE event stream and blocks
// until DeliverResponse routes a matching reply. Returns the decoded
// response (which may itself carry an RPC error) or a context error
// on timeout/cancellation.
//
// Callers that need no reply (notifications) should use EmitNotification
// instead — it's non-blocking and has no id.
func (s *Server) SendRequest(ctx context.Context, method string, params map[string]any) (*Response, error) {
	id := s.bi.newServerID()
	req := &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("mcp: marshal params: %w", err)
		}
		req.Params = raw
	}

	ch, cleanup := s.bi.registerPending(id)
	defer cleanup()

	s.bi.enqueue(&OutboundMessage{Kind: OutboundRequest, Request: req})

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Sample is a convenience wrapper around SendRequest for the
// `sampling/createMessage` method. Accepts the params as a map so
// callers don't need to build a typed struct for the mock.
func (s *Server) Sample(ctx context.Context, params map[string]any) (*Response, error) {
	return s.SendRequest(ctx, "sampling/createMessage", params)
}

// ListRoots is a convenience wrapper around SendRequest for the
// `roots/list` method. The params argument is usually empty but left
// configurable for forward-compat with future MCP revisions.
func (s *Server) ListRoots(ctx context.Context, params map[string]any) (*Response, error) {
	return s.SendRequest(ctx, "roots/list", params)
}

// pushNotification redirects EmitNotification through the bidirectional
// queue as well, so SSE subscribers receive notifications in the same
// stream as server-initiated requests. The existing pending slice on
// Server is kept for transports that haven't opted into SSE (plain HTTP
// POST callers still see `X-MCP-Pending-Notifications` bundled with the
// response).
func (s *Server) pushNotification(n *Notification) {
	s.bi.enqueue(&OutboundMessage{Kind: OutboundNotification, Notification: n})
}

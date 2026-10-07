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
//
// Delivery is a single ordered queue. Every message is appended to
// outbound; the current subscriber's pump goroutine pops the head and hands
// it to the subscriber over an UNBUFFERED channel, so a message leaves the
// queue only when the reader actually takes it. The previous design split
// messages between the queue and a buffered channel: a stolen stream lost
// whatever sat in its channel buffer (audit M-27, review P-05), and a message
// that overflowed the buffer could be overtaken by a later one and stranded
// until the client reconnected (review P-06). With one container neither can
// happen.
type bidirectional struct {
	mu sync.Mutex
	// outbound is the ordered queue of undelivered messages.
	outbound []*OutboundMessage
	// pending maps string(id) -> response channel so DeliverResponse
	// can route replies back to the blocked SendRequest caller.
	pending map[string]chan *Response
	// sub is the attached subscription. At most one is active; attaching a
	// second detaches the first (the "new tab steals the SSE stream" pattern
	// real MCP proxies use).
	sub *subscription

	nextID atomic.Int64
}

// subscription is one attached reader and the pump that feeds it.
type subscription struct {
	ch   chan *OutboundMessage // unbuffered; closed by the pump on exit
	wake chan struct{}         // cap 1: "the queue may have grown"
	done chan struct{}         // closed to detach
	once sync.Once
}

func (s *subscription) stop() { s.once.Do(func() { close(s.done) }) }

func newBidirectional() *bidirectional {
	return &bidirectional{
		pending: make(map[string]chan *Response),
	}
}

// Subscribe attaches a reader that receives every queued and future
// outbound message in FIFO order. Calling Subscribe again detaches the
// previous reader, whose channel is then closed; anything it had not
// received stays queued for the new one. The returned cancel function
// detaches this reader the same way. buffer is accepted for API
// compatibility and ignored: delivery is unbuffered by design (see
// bidirectional).
func (b *bidirectional) Subscribe(buffer int) (<-chan *OutboundMessage, func()) {
	_ = buffer
	sub := &subscription{
		ch:   make(chan *OutboundMessage),
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	b.mu.Lock()
	if b.sub != nil {
		b.sub.stop()
	}
	b.sub = sub
	b.mu.Unlock()

	go b.pump(sub)

	cancel := func() {
		sub.stop()
		b.mu.Lock()
		if b.sub == sub {
			b.sub = nil
		}
		b.mu.Unlock()
	}
	return sub.ch, cancel
}

// pump hands queued messages to sub one at a time, in order, until sub is
// detached. A message is removed from the queue only once the reader has
// received it; on detach the in-flight message is put back at the head.
func (b *bidirectional) pump(sub *subscription) {
	defer close(sub.ch)
	for {
		b.mu.Lock()
		// A detached pump must not pop: it would hold a message the new
		// subscriber's pump is waiting for.
		select {
		case <-sub.done:
			b.mu.Unlock()
			return
		default:
		}
		if len(b.outbound) == 0 {
			b.mu.Unlock()
			select {
			case <-sub.wake:
				continue
			case <-sub.done:
				return
			}
		}
		msg := b.outbound[0]
		b.outbound = b.outbound[1:]
		b.mu.Unlock()

		select {
		case sub.ch <- msg:
		case <-sub.done:
			b.mu.Lock()
			b.outbound = append([]*OutboundMessage{msg}, b.outbound...)
			next := b.sub
			b.mu.Unlock()
			// The replacement pump may already be asleep on an empty queue;
			// tell it the message is back.
			if next != nil {
				select {
				case next.wake <- struct{}{}:
				default:
				}
			}
			return
		}
	}
}

// enqueue appends a message to the queue and wakes the attached pump. With
// no subscriber the queue is bounded like the pending queue (audit M-25):
// the oldest messages are dropped so a late subscriber still gets the most
// recent ones.
func (b *bidirectional) enqueue(msg *OutboundMessage) {
	b.mu.Lock()
	b.outbound = append(b.outbound, msg)
	if over := len(b.outbound) - maxPendingNotifications; over > 0 {
		b.outbound = append(b.outbound[:0], b.outbound[over:]...)
	}
	sub := b.sub
	b.mu.Unlock()
	if sub != nil {
		select {
		case sub.wake <- struct{}{}:
		default: // already signalled
		}
	}
}

// dropRequest removes a not-yet-delivered request from the queue, so a
// request whose SendRequest timed out is never delivered to a later
// subscriber (whose reply would only get a 404).
func (b *bidirectional) dropRequest(id json.RawMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, m := range b.outbound {
		if m.Kind == OutboundRequest && m.Request != nil && string(m.Request.ID) == string(id) {
			b.outbound = append(b.outbound[:i], b.outbound[i+1:]...)
			return
		}
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
		s.bi.dropRequest(id)
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

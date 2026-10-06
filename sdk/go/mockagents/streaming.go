package mockagents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StreamChunk is a protocol-agnostic streaming delta. It mirrors the
// shape of the Python and TypeScript SDK types one-for-one so example
// code translates across languages.
type StreamChunk struct {
	// Text is the newly delivered text fragment, empty on non-text events.
	Text string
	// ToolCallDelta, when non-nil, is the incremental tool-call update
	// carrying an (index, name, fragment) triple. The fragment holds the
	// JSON arguments chunk; Name is set on the first chunk of a call.
	ToolCallDelta *ToolCallDelta
	// FinishReason is populated on the final chunk ("stop", "end_turn", …).
	FinishReason string
	// Finished is true when the stream has delivered its terminal event.
	Finished bool
	// Raw is the underlying provider event for callers that need more.
	Raw map[string]any
}

// ToolCallDelta is the incremental tool-call payload on a StreamChunk.
type ToolCallDelta struct {
	Index    int
	Name     string
	Fragment string
}

// sseFrame is one parsed SSE record: the optional event: line and the
// joined data: lines.
type sseFrame struct {
	Event string
	Data  string
}

// parseSSEFrame parses a single "event: …\ndata: …" block. Returns nil
// when the frame carries no data line. Multiple data: lines are joined
// with newlines per the SSE spec; comments starting with ':' are
// ignored; exactly one leading space after the colon is stripped.
func parseSSEFrame(frame string) *sseFrame {
	var (
		event string
		data  []string
	)
	for _, raw := range strings.Split(frame, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "data:"):
			payload := line[len("data:"):]
			if strings.HasPrefix(payload, " ") {
				payload = payload[1:]
			}
			data = append(data, payload)
		}
	}
	if len(data) == 0 {
		return nil
	}
	return &sseFrame{Event: event, Data: strings.Join(data, "\n")}
}

// RawEventStream yields provider-shaped event dicts from ChatStream or
// MessageStream. Use it like a bufio.Scanner: call Next in a loop, read
// Value each iteration, then check Err. Close releases the HTTP body.
//
//	for stream.Next() {
//	    ev := stream.Value()
//	    // …
//	}
//	if err := stream.Err(); err != nil { … }
//	_ = stream.Close()
type RawEventStream struct {
	body      io.ReadCloser
	scanner   *bufio.Scanner
	cancel    context.CancelFunc
	stopAt    func(map[string]any) bool // returns true when this event is terminal
	done      bool
	value     map[string]any
	err       error
	completed bool // the terminal sentinel ([DONE] / message_stop) arrived
	truncated bool // the body ended cleanly without that sentinel
	malformed int  // data frames that were not a JSON object (skipped)
}

// Next advances the stream. Returns true when Value holds a new event.
// After Next returns false, inspect Err to tell EOF from failure, and
// Truncated / MalformedFrames to tell a clean completion from a faulty one.
func (s *RawEventStream) Next() bool {
	if s == nil || s.done {
		return false
	}
	for s.scanner.Scan() {
		frame := parseSSEFrame(s.scanner.Text())
		if frame == nil {
			continue
		}
		if frame.Data == "[DONE]" {
			s.completed = true
			s.done = true
			return false
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(frame.Data), &ev); err != nil || ev == nil {
			// Malformed data lines are skipped but counted, so an injected
			// `malformed` fault is observable (review K-14).
			s.malformed++
			continue
		}
		s.value = ev
		if s.stopAt != nil && s.stopAt(ev) {
			// Deliver the terminal event, then stop on the next call.
			s.completed = true
			s.done = true
		}
		return true
	}
	if err := s.scanner.Err(); err != nil {
		s.err = err
	} else {
		// A clean EOF without the terminal sentinel: a truncated stream.
		s.truncated = true
	}
	s.done = true
	return false
}

// Completed reports whether the terminal sentinel ([DONE] for OpenAI,
// message_stop for Anthropic) arrived.
func (s *RawEventStream) Completed() bool { return s != nil && s.completed }

// Truncated reports whether the body ended without the terminal sentinel,
// e.g. under a `streaming.truncateAfter` fault. A stream the caller stopped
// reading early, or one that failed with Err, is not reported as truncated.
func (s *RawEventStream) Truncated() bool { return s != nil && s.truncated }

// MalformedFrames counts the data frames that were not a JSON object and were
// skipped, e.g. under a `streaming.malformed` fault.
func (s *RawEventStream) MalformedFrames() int {
	if s == nil {
		return 0
	}
	return s.malformed
}

// Value returns the event most recently surfaced by Next.
func (s *RawEventStream) Value() map[string]any {
	if s == nil {
		return nil
	}
	return s.value
}

// Err returns the first error encountered while reading, or nil.
func (s *RawEventStream) Err() error {
	if s == nil {
		return nil
	}
	return s.err
}

// Close releases the underlying response body and cancels the request
// context. Safe to call multiple times.
func (s *RawEventStream) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.body != nil {
		err := s.body.Close()
		s.body = nil
		return err
	}
	return nil
}

// requestSSE POSTs a JSON payload and returns a scanner configured to
// split the response body on SSE frame boundaries (a blank line).
func (c *Client) requestSSE(ctx context.Context, path string, headers map[string]string, payload any) (io.ReadCloser, *bufio.Scanner, context.CancelFunc, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal payload: %w", err)
	}
	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.applyAuth(req)
	// SSE clients must not apply a read timeout — use a per-request
	// context instead so Close() can cancel mid-stream.
	httpClient := c.httpClient
	if httpClient.Timeout > 0 {
		cloned := *httpClient
		cloned.Timeout = 0
		httpClient = &cloned
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		return nil, nil, nil, &HTTPError{Status: resp.StatusCode, Body: string(body)}
	}
	scanner := bufio.NewScanner(&newlineNormalizer{r: resp.Body})
	// Raise the frame cap — default 64 KiB is fine for typical SSE but
	// pathological chunks can exceed it. 1 MiB matches the adapter
	// body-size ceiling on the server side.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	scanner.Split(splitSSEFrames)
	return resp.Body, scanner, cancel, nil
}

// splitSSEFrames is a bufio.SplitFunc that yields one SSE frame (all the
// lines up to a blank-line terminator) at a time. EOF drains the tail. It
// expects LF line endings: wrap the body in a newlineNormalizer first.
func splitSSEFrames(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if len(data) == 0 && atEOF {
		return 0, nil, nil
	}
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, data[:i], nil
	}
	if atEOF {
		return len(data), bytes.TrimRight(data, "\r\n"), nil
	}
	return 0, nil, nil
}

// newlineNormalizer rewrites CRLF and bare CR line endings to LF as bytes are
// read. The SSE spec allows all three (and proxies rewrite one into another);
// normalising before frame splitting means a frame boundary is always a
// plain blank line, so mixed endings can no longer merge two events into one
// unparseable frame (review K-12). A CR that ends one read and an LF that
// starts the next are still a single line ending.
type newlineNormalizer struct {
	r         io.Reader
	pendingCR bool // the last byte emitted was a CR rewritten to LF
}

func (n *newlineNormalizer) Read(p []byte) (int, error) {
	for {
		nr, err := n.r.Read(p)
		out := 0
		for i := 0; i < nr; i++ {
			b := p[i]
			if n.pendingCR {
				n.pendingCR = false
				if b == '\n' {
					continue // second half of a CRLF already emitted as LF
				}
			}
			if b == '\r' {
				b = '\n'
				n.pendingCR = true
			}
			p[out] = b
			out++
		}
		// Never report (0, nil) just because a read held only the swallowed
		// LF of a split CRLF; read again instead.
		if out > 0 || err != nil || nr == 0 {
			return out, err
		}
	}
}

// ChatStream opens an OpenAI Chat Completions stream and returns a
// RawEventStream that yields the parsed delta payloads from each
// “data:“ line. Terminates on the “[DONE]“ sentinel.
func (c *Client) ChatStream(ctx context.Context, messages []ChatMessage, opts ChatOptions) (*RawEventStream, error) {
	model := opts.Model
	if model == "" {
		model = DefaultOpenAIModel
	}
	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}
	if opts.Tools != nil {
		payload["tools"] = opts.Tools
	}
	if opts.ToolChoice != nil {
		payload["tool_choice"] = opts.ToolChoice
	}
	if opts.Temperature != nil {
		payload["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		payload["max_tokens"] = *opts.MaxTokens
	}
	for k, v := range opts.Extra {
		payload[k] = v
	}
	headers := map[string]string{"Content-Type": "application/json"}
	if opts.SessionID != "" {
		headers["X-Session-Id"] = opts.SessionID
	}
	body, scanner, cancel, err := c.requestSSE(ctx, "/v1/chat/completions", headers, payload)
	if err != nil {
		return nil, err
	}
	return &RawEventStream{body: body, scanner: scanner, cancel: cancel}, nil
}

// MessageStream opens an Anthropic Messages stream. Yields
// “message_start“ / “content_block_*“ / “message_delta“ /
// “message_stop“ events and terminates cleanly after “message_stop“.
func (c *Client) MessageStream(ctx context.Context, messages []ChatMessage, opts MessageOptions) (*RawEventStream, error) {
	model := opts.Model
	if model == "" {
		model = DefaultAnthropicModel
	}
	max := opts.MaxTokens
	if max == 0 {
		max = 1024
	}
	payload := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": max,
		"stream":     true,
	}
	if opts.System != "" {
		payload["system"] = opts.System
	}
	if opts.Tools != nil {
		payload["tools"] = opts.Tools
	}
	for k, v := range opts.Extra {
		payload[k] = v
	}
	headers := c.anthropicHeaders(opts.SessionID)
	body, scanner, cancel, err := c.requestSSE(ctx, "/v1/messages", headers, payload)
	if err != nil {
		return nil, err
	}
	stop := func(ev map[string]any) bool {
		t, _ := ev["type"].(string)
		return t == "message_stop"
	}
	return &RawEventStream{body: body, scanner: scanner, cancel: cancel, stopAt: stop}, nil
}

// ChunkStream yields StreamChunks for IterStream consumers. Mirrors the
// RawEventStream surface: Next / Value / Err / Close.
type ChunkStream struct {
	raw       *RawEventStream
	normalize func(ev map[string]any, state *normalizeState) []StreamChunk
	state     normalizeState
	pending   []StreamChunk
	value     StreamChunk
	done      bool
	finished  bool // a Finished chunk was delivered
}

type normalizeState struct {
	currentToolIndex int
	currentToolName  string
	finalStop        string
}

// Next advances the stream; see RawEventStream.Next.
func (s *ChunkStream) Next() bool {
	if s == nil || s.done {
		return false
	}
	for len(s.pending) == 0 {
		if !s.raw.Next() {
			s.done = true
			return false
		}
		s.pending = s.normalize(s.raw.Value(), &s.state)
	}
	s.value = s.pending[0]
	s.pending = s.pending[1:]
	if s.value.Finished {
		s.finished = true
		s.done = true
	}
	return true
}

// Value returns the chunk surfaced by the last Next.
func (s *ChunkStream) Value() StreamChunk {
	if s == nil {
		return StreamChunk{}
	}
	return s.value
}

// Err reports the first read error from the underlying raw stream.
func (s *ChunkStream) Err() error {
	if s == nil || s.raw == nil {
		return nil
	}
	return s.raw.Err()
}

// Completed reports whether the terminal event arrived: a Finished chunk (the
// OpenAI finish_reason chunk or Anthropic message_stop) or the raw sentinel.
func (s *ChunkStream) Completed() bool {
	return s != nil && (s.finished || s.raw.Completed())
}

// Truncated reports whether the stream ended without its terminal event; see
// RawEventStream.Truncated.
func (s *ChunkStream) Truncated() bool { return s != nil && s.raw.Truncated() }

// MalformedFrames counts skipped non-JSON data frames; see
// RawEventStream.MalformedFrames.
func (s *ChunkStream) MalformedFrames() int {
	if s == nil {
		return 0
	}
	return s.raw.MalformedFrames()
}

// Close releases underlying resources.
func (s *ChunkStream) Close() error {
	if s == nil || s.raw == nil {
		return nil
	}
	return s.raw.Close()
}

// IterStreamOptions is the union of ChatOptions and MessageOptions plus
// the protocol selector.
type IterStreamOptions struct {
	Protocol    string // "openai" (default) or "anthropic"
	Model       string
	SessionID   string
	Tools       []any
	ToolChoice  any
	Temperature *float64
	MaxTokens   *int
	System      string
	Extra       map[string]any
}

// IterStream opens a streaming completion on the selected protocol and
// returns a protocol-agnostic ChunkStream. Pick the wire format by
// setting opts.Protocol to "openai" (default) or "anthropic".
func (c *Client) IterStream(ctx context.Context, messages []ChatMessage, opts IterStreamOptions) (*ChunkStream, error) {
	protocol := opts.Protocol
	if protocol == "" {
		protocol = "openai"
	}
	switch protocol {
	case "openai":
		raw, err := c.ChatStream(ctx, messages, ChatOptions{
			Model:       opts.Model,
			SessionID:   opts.SessionID,
			Tools:       opts.Tools,
			ToolChoice:  opts.ToolChoice,
			Temperature: opts.Temperature,
			MaxTokens:   opts.MaxTokens,
			Extra:       opts.Extra,
		})
		if err != nil {
			return nil, err
		}
		return &ChunkStream{raw: raw, normalize: normalizeOpenAIStream}, nil
	case "anthropic":
		var max int
		if opts.MaxTokens != nil {
			max = *opts.MaxTokens
		}
		raw, err := c.MessageStream(ctx, messages, MessageOptions{
			Model:     opts.Model,
			SessionID: opts.SessionID,
			System:    opts.System,
			MaxTokens: max,
			Tools:     opts.Tools,
			Extra:     opts.Extra,
		})
		if err != nil {
			return nil, err
		}
		return &ChunkStream{raw: raw, normalize: normalizeAnthropicStream}, nil
	default:
		return nil, fmt.Errorf("mockagents: unknown protocol %q", protocol)
	}
}

// normalizeOpenAIStream converts an OpenAI chunk event into zero or one
// StreamChunks. Padding chunks (no text, no tool delta, not finished)
// are dropped — consumers never have to filter empty deltas themselves.
func normalizeOpenAIStream(ev map[string]any, _ *normalizeState) []StreamChunk {
	choices, _ := ev["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return nil
	}
	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		delta = map[string]any{}
	}

	text, _ := delta["content"].(string)

	var tcDelta *ToolCallDelta
	if tcs, _ := delta["tool_calls"].([]any); len(tcs) > 0 {
		if tc, _ := tcs[0].(map[string]any); tc != nil {
			idx := 0
			if v, ok := tc["index"].(float64); ok {
				idx = int(v)
			}
			name := ""
			frag := ""
			if fn, _ := tc["function"].(map[string]any); fn != nil {
				name, _ = fn["name"].(string)
				frag, _ = fn["arguments"].(string)
			}
			tcDelta = &ToolCallDelta{Index: idx, Name: name, Fragment: frag}
		}
	}

	finishReason, _ := choice["finish_reason"].(string)
	finished := finishReason != ""

	if text == "" && tcDelta == nil && !finished {
		return nil
	}
	return []StreamChunk{{
		Text:          text,
		ToolCallDelta: tcDelta,
		FinishReason:  finishReason,
		Finished:      finished,
		Raw:           ev,
	}}
}

// normalizeAnthropicStream converts one Anthropic event into zero or
// more StreamChunks. Carries the current tool index/name across events
// via normalizeState so input_json_delta fragments line up with the
// right tool-call block.
func normalizeAnthropicStream(ev map[string]any, st *normalizeState) []StreamChunk {
	et, _ := ev["type"].(string)
	switch et {
	case "content_block_start":
		block, _ := ev["content_block"].(map[string]any)
		if block == nil {
			return nil
		}
		if t, _ := block["type"].(string); t == "tool_use" {
			idx := st.currentToolIndex + 1
			if v, ok := ev["index"].(float64); ok {
				idx = int(v)
			}
			st.currentToolIndex = idx
			st.currentToolName, _ = block["name"].(string)
			return []StreamChunk{{
				Text:          "",
				ToolCallDelta: &ToolCallDelta{Index: idx, Name: st.currentToolName, Fragment: ""},
				Raw:           ev,
			}}
		}
		return nil
	case "content_block_delta":
		delta, _ := ev["delta"].(map[string]any)
		if delta == nil {
			return nil
		}
		dt, _ := delta["type"].(string)
		switch dt {
		case "text_delta":
			text, _ := delta["text"].(string)
			if text == "" {
				return nil
			}
			return []StreamChunk{{Text: text, Raw: ev}}
		case "input_json_delta":
			frag, _ := delta["partial_json"].(string)
			if frag == "" {
				return nil
			}
			return []StreamChunk{{
				ToolCallDelta: &ToolCallDelta{
					Index:    st.currentToolIndex,
					Name:     st.currentToolName,
					Fragment: frag,
				},
				Raw: ev,
			}}
		}
		return nil
	case "message_delta":
		delta, _ := ev["delta"].(map[string]any)
		if delta == nil {
			return nil
		}
		if stop, _ := delta["stop_reason"].(string); stop != "" {
			st.finalStop = stop
		}
		return nil
	case "message_stop":
		reason := st.finalStop
		if reason == "" {
			reason = "end_turn"
		}
		return []StreamChunk{{FinishReason: reason, Finished: true, Raw: ev}}
	}
	return nil
}

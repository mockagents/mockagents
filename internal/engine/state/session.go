package state

import (
	"sync"
	"time"
)

// Message represents a single message in a conversation.
type Message struct {
	Role      string        `json:"role"`
	Content   string        `json:"content"`
	ToolCalls []ToolCallMsg `json:"tool_calls,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// ToolCallMsg is a tool call recorded in conversation history.
type ToolCallMsg struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// Session holds the conversation state for a single client session.
type Session struct {
	mu         sync.Mutex
	ID         string         `json:"id"`
	AgentName  string         `json:"agent_name"`
	Messages   []Message      `json:"messages"`
	TurnCount  int            `json:"turn_count"`
	Variables  map[string]any `json:"variables,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	LastAccess time.Time      `json:"last_access"`
	TTL        time.Duration  `json:"-"`
	// MaxHistory caps len(Messages); older entries are dropped after each
	// completed turn (audit H-06). 0 = unlimited.
	MaxHistory int `json:"-"`
}

// WithLocked runs fn while holding the session's mutation lock. Use it
// for multi-step read/modify/write sequences that must stay atomic for
// one conversation turn.
//
// Re-entry constraint (F-SS-001): s.mu is a non-reentrant sync.Mutex, so
// fn must NOT call any other exported method that locks it
// (LatestUserMessage, IsExpired, AppendUserMessage, AppendAssistantMessage,
// ApplyTurn, or a nested WithLocked) — doing so self-deadlocks. fn should
// touch only the session's fields directly.
func (s *Session) WithLocked(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

// ApplyTurn builds a proposed turn against an isolated variables snapshot,
// then commits the user message, assistant message, turn number, and variables
// together. A failed build leaves the session exactly as it was, so retries
// observe the same turn and cannot grow history through an error path.
// The whole turn is guarded by the session lock so
// concurrent requests for the same session cannot interleave history
// updates or scenario matching.
//
// build runs while s.mu is held, which gives it two contracts:
//   - Variables (F-SS-002): non-empty state is passed as a recursive
//     JSON-compatible clone. An existing empty map is reused and cleared on
//     failure to avoid a per-turn allocation. Mutations are committed only
//     when build succeeds. build must not retain and mutate the map after
//     returning.
//   - Re-entry (F-SS-001): build must not call back into a session method
//     that locks s.mu (see WithLocked) — the mutex is non-reentrant.
func (s *Session) ApplyTurn(
	userContent string,
	build func(turnCount int, variables map[string]any) (assistantContent string, toolCalls []ToolCallMsg, err error),
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	variables := s.Variables
	reusedEmptyVariables := len(variables) == 0 && variables != nil
	if !reusedEmptyVariables {
		variables = cloneVariables(variables)
	}
	assistantContent, toolCalls, err := build(s.TurnCount+1, variables)
	if err != nil {
		// Reusing an existing empty map avoids allocating on the common path.
		// Restore that map to its pre-turn state if build added anything before
		// failing. Non-empty state still uses the recursive snapshot above.
		if reusedEmptyVariables {
			clear(variables)
		}
		return err
	}
	s.Variables = variables
	s.appendUserMessage(userContent)
	s.appendAssistantMessage(assistantContent, toolCalls)
	return nil
}

func cloneVariables(in map[string]any) map[string]any {
	if in == nil {
		return make(map[string]any)
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneVariableValue(v)
	}
	return out
}

func cloneVariableValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, child := range value {
			out[k] = cloneVariableValue(child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = cloneVariableValue(child)
		}
		return out
	default:
		return value
	}
}

// initialMessageCap is the pre-allocated capacity for a new session's
// Messages slice. Chosen so a typical 3–8 turn conversation never
// reallocates; longer conversations grow exponentially as normal.
// Profiling (2026-04-14) showed Session.AppendUserMessage at ~10%
// cumulative CPU driven entirely by growslice out of the zero-cap
// default.
const initialMessageCap = 16

// NewSession creates a new session with the given ID and agent name.
//
// A ttl <= 0 means the session never expires (F-SS-006): IsExpired treats a
// non-positive TTL as eternal by design — see TestSession_NoTTL. This is NOT
// clamped here on purpose; clamping would remove that capability. In normal
// operation sessions are created via MemoryStore.GetOrCreate, which always
// passes the store's already-clamped positive ttl, so an accidental eternal
// session is only reachable by calling NewSession with ttl<=0 directly.
func NewSession(id, agentName string, ttl time.Duration) *Session {
	now := time.Now()
	return &Session{
		ID:         id,
		AgentName:  agentName,
		Messages:   make([]Message, 0, initialMessageCap),
		Variables:  make(map[string]any),
		CreatedAt:  now,
		LastAccess: now,
		TTL:        ttl,
	}
}

// AppendUserMessage adds a user message and increments the turn count.
func (s *Session) AppendUserMessage(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendUserMessage(content)
}

func (s *Session) appendUserMessage(content string) {
	now := time.Now() // single clock read for both fields (F-SS-003)
	s.Messages = append(s.Messages, Message{
		Role:      "user",
		Content:   content,
		Timestamp: now,
	})
	s.TurnCount++
	s.LastAccess = now
}

// AppendAssistantMessage adds an assistant response to the history.
func (s *Session) AppendAssistantMessage(content string, toolCalls []ToolCallMsg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendAssistantMessage(content, toolCalls)
}

func (s *Session) appendAssistantMessage(content string, toolCalls []ToolCallMsg) {
	now := time.Now() // single clock read for both fields (F-SS-003)
	s.Messages = append(s.Messages, Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
		Timestamp: now,
	})
	s.LastAccess = now
	// Trim to the retained-history cap after the turn is complete, dropping
	// the oldest entries. A client that pins one session id for a long run
	// used to grow Messages forever (audit H-06). TurnCount is untouched.
	if s.MaxHistory > 0 && len(s.Messages) > s.MaxHistory {
		drop := len(s.Messages) - s.MaxHistory
		s.Messages = append(s.Messages[:0], s.Messages[drop:]...)
	}
}

// lastAccess returns LastAccess under the session lock, for the store's
// LRU eviction scan.
func (s *Session) lastAccess() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LastAccess
}

// IsExpired returns true if the session has exceeded its TTL.
func (s *Session) IsExpired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.TTL <= 0 {
		return false
	}
	return time.Since(s.LastAccess) > s.TTL
}

// LatestUserMessage returns the content of the most recent user message.
func (s *Session) LatestUserMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == "user" {
			return s.Messages[i].Content
		}
	}
	return ""
}

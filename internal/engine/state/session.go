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

// ApplyTurn appends the user message, invokes build with the new turn
// number and live variables map, then appends the assistant message
// returned by build. The whole turn is guarded by the session lock so
// concurrent requests for the same session cannot interleave history
// updates or scenario matching.
//
// build runs while s.mu is held, which gives it two contracts:
//   - Variables (F-SS-002): the map passed in is the session's *live*
//     Variables, not a copy. build may read and mutate it freely, but
//     ONLY from within this call — the session lock is the only thing
//     serializing access, so stashing the map and touching it later races
//     with the next turn. No accessor exposes Variables outside the lock.
//   - Re-entry (F-SS-001): build must not call back into a session method
//     that locks s.mu (see WithLocked) — the mutex is non-reentrant.
func (s *Session) ApplyTurn(
	userContent string,
	build func(turnCount int, variables map[string]any) (assistantContent string, toolCalls []ToolCallMsg, err error),
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.appendUserMessage(userContent)
	assistantContent, toolCalls, err := build(s.TurnCount, s.Variables)
	if err != nil {
		return err
	}
	s.appendAssistantMessage(assistantContent, toolCalls)
	return nil
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

package state

import (
	"time"
)

// Message represents a single message in a conversation.
type Message struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []ToolCallMsg  `json:"tool_calls,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// ToolCallMsg is a tool call recorded in conversation history.
type ToolCallMsg struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// Session holds the conversation state for a single client session.
type Session struct {
	ID         string            `json:"id"`
	AgentName  string            `json:"agent_name"`
	Messages   []Message         `json:"messages"`
	TurnCount  int               `json:"turn_count"`
	Variables  map[string]any    `json:"variables,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	LastAccess time.Time         `json:"last_access"`
	TTL        time.Duration     `json:"-"`
}

// NewSession creates a new session with the given ID and agent name.
func NewSession(id, agentName string, ttl time.Duration) *Session {
	now := time.Now()
	return &Session{
		ID:         id,
		AgentName:  agentName,
		Messages:   make([]Message, 0),
		Variables:  make(map[string]any),
		CreatedAt:  now,
		LastAccess: now,
		TTL:        ttl,
	}
}

// AppendUserMessage adds a user message and increments the turn count.
func (s *Session) AppendUserMessage(content string) {
	s.Messages = append(s.Messages, Message{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	s.TurnCount++
	s.LastAccess = time.Now()
}

// AppendAssistantMessage adds an assistant response to the history.
func (s *Session) AppendAssistantMessage(content string, toolCalls []ToolCallMsg) {
	s.Messages = append(s.Messages, Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
		Timestamp: time.Now(),
	})
	s.LastAccess = time.Now()
}

// IsExpired returns true if the session has exceeded its TTL.
func (s *Session) IsExpired() bool {
	if s.TTL <= 0 {
		return false
	}
	return time.Since(s.LastAccess) > s.TTL
}

// LatestUserMessage returns the content of the most recent user message.
func (s *Session) LatestUserMessage() string {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == "user" {
			return s.Messages[i].Content
		}
	}
	return ""
}

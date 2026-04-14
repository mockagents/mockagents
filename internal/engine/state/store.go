package state

import (
	"sync"
	"time"
)

const DefaultSessionTTL = 30 * time.Minute

// Store defines the interface for session state storage.
type Store interface {
	// Get retrieves a session by ID. Returns nil if not found or expired.
	Get(id string) *Session
	// GetOrCreate retrieves an existing session or creates a new one.
	GetOrCreate(id, agentName string) *Session
	// Save persists session state.
	Save(session *Session)
	// Delete removes a session.
	Delete(id string)
	// Count returns the number of active sessions.
	Count() int
	// Cleanup removes expired sessions.
	Cleanup()
}

// MemoryStore is an in-memory session store with TTL-based expiration.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewMemoryStore creates an in-memory session store.
func NewMemoryStore(ttl time.Duration) *MemoryStore {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &MemoryStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

func (s *MemoryStore) Get(id string) *Session {
	s.mu.RLock()
	session, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	if session.IsExpired() {
		s.Delete(id)
		return nil
	}
	return session
}

func (s *MemoryStore) GetOrCreate(id, agentName string) *Session {
	if session := s.Get(id); session != nil {
		return session
	}
	session := NewSession(id, agentName, s.ttl)
	s.Save(session)
	return session
}

func (s *MemoryStore) Save(session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
}

func (s *MemoryStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *MemoryStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func (s *MemoryStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		if session.IsExpired() {
			delete(s.sessions, id)
		}
	}
}

// StartCleanupTicker starts a background goroutine that periodically
// cleans up expired sessions. Returns a stop function.
func (s *MemoryStore) StartCleanupTicker(interval time.Duration) func() {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				s.Cleanup()
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(done) }
}

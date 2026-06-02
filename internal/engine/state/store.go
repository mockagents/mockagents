package state

import (
	"sync"
	"time"
)

const DefaultSessionTTL = 30 * time.Minute

// Store defines the interface for session state storage.
//
// Aliasing contract (F-ST-006): Get and GetOrCreate return the store's
// internal *Session pointer, not a copy. Callers must mutate it only
// through the session's own lock (ApplyTurn / WithLocked); there is no
// separate "save" step — in-place changes are immediately visible to the
// next reader of the same id.
type Store interface {
	// Get retrieves a session by ID. Returns nil if not found or expired.
	// The returned pointer is shared (see the aliasing contract above).
	Get(id string) *Session
	// GetOrCreate retrieves an existing session or creates a new one. The
	// returned pointer is shared and already stored — mutations persist
	// without any follow-up call.
	GetOrCreate(id, agentName string) *Session
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
		// Compare-and-delete (F-ST-003): evict only THIS expired pointer.
		// Between the RUnlock above and here another goroutine may have
		// replaced the id with a fresh session via GetOrCreate; an
		// unconditional Delete(id) would drop that live session (TOCTOU).
		s.deleteIfSame(id, session)
		return nil
	}
	return session
}

// deleteIfSame removes id only if it still maps to the given session
// pointer, so a concurrent replacement under the same id is preserved.
func (s *MemoryStore) deleteIfSame(id string, session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.sessions[id]; ok && cur == session {
		delete(s.sessions, id)
	}
}

func (s *MemoryStore) GetOrCreate(id, agentName string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok {
		if session.IsExpired() {
			delete(s.sessions, id)
		} else {
			return session
		}
	}
	session := NewSession(id, agentName, s.ttl)
	// Key on the lookup id, not session.ID (F-ST-007): they are equal today
	// since NewSession copies id verbatim, but keying on id keeps the map
	// consistent with how callers look sessions up even if NewSession ever
	// normalizes the id.
	s.sessions[id] = session
	return session
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
	// Snapshot the sessions under a read lock so Get/GetOrCreate aren't
	// blocked while we scan (F-ST-005). The previous version held the
	// exclusive write lock across the whole map iteration *and* each
	// per-session IsExpired() (which takes that session's own lock),
	// stalling all callers for the duration of the scan.
	s.mu.RLock()
	snapshot := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		snapshot = append(snapshot, session)
	}
	s.mu.RUnlock()

	expired := make([]*Session, 0)
	for _, session := range snapshot {
		if session.IsExpired() {
			expired = append(expired, session)
		}
	}
	if len(expired) == 0 {
		return
	}

	// Delete only the still-expired, still-same pointers. A session may
	// have been replaced (different pointer) or refreshed (same pointer,
	// LastAccess bumped by a concurrent turn) since the snapshot — in
	// either case it must survive. The store→session lock order here
	// matches every other path, so re-checking IsExpired is deadlock-free.
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range expired {
		if cur, ok := s.sessions[session.ID]; ok && cur == session && session.IsExpired() {
			delete(s.sessions, session.ID)
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

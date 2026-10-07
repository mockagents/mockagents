package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/trace"
)

// ErrRunNotFound is returned for unknown run ids.
var ErrRunNotFound = errors.New("run not found")

type entry struct {
	run   *Run
	trace *trace.Trace
	subs  map[int]chan struct{}
}

// Store holds runs and their traces in memory, notifies subscribers of
// changes and optionally persists to a JSON state file.
type Store struct {
	mu      sync.Mutex
	runs    map[string]*entry
	order   []string
	subSeq  int
	dirty   bool
	maxRuns int
}

// NewStore returns an empty store that keeps at most maxRuns finished runs
// (oldest finished runs are evicted first; in_progress runs are never evicted).
func NewStore(maxRuns int) *Store {
	if maxRuns <= 0 {
		maxRuns = 500
	}
	return &Store{runs: map[string]*entry{}, maxRuns: maxRuns}
}

func cloneRun(r *Run) *Run {
	raw, err := json.Marshal(r)
	if err != nil {
		panic(fmt.Sprintf("clone run: %v", err))
	}
	var out Run
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(fmt.Sprintf("clone run: %v", err))
	}
	return &out
}

func (s *Store) add(r *Run, tr *trace.Trace) {
	s.mu.Lock()
	s.runs[r.ID] = &entry{run: r, trace: tr, subs: map[int]chan struct{}{}}
	s.order = append(s.order, r.ID)
	s.dirty = true
	s.evictLocked()
	s.mu.Unlock()
}

func (s *Store) evictLocked() {
	if len(s.order) <= s.maxRuns {
		return
	}
	keep := s.order[:0]
	excess := len(s.order) - s.maxRuns
	for _, id := range s.order {
		e := s.runs[id]
		if excess > 0 && e.run.Terminal() && len(e.subs) == 0 {
			delete(s.runs, id)
			excess--
			continue
		}
		keep = append(keep, id)
	}
	s.order = keep
}

// update mutates a run under the lock. fn returning false means "no change".
// Mutating a terminal run is refused, which makes the terminal transition final.
func (s *Store) update(id string, fn func(r *Run) bool) bool {
	s.mu.Lock()
	e, ok := s.runs[id]
	if !ok || e.run.Terminal() {
		s.mu.Unlock()
		return false
	}
	changed := fn(e.run)
	if changed {
		now := time.Now()
		e.run.UpdatedAt = now
		e.run.HeartbeatAt = now
		s.dirty = true
	}
	subs := subscribers(e)
	s.mu.Unlock()
	if changed {
		notify(subs)
	}
	return changed
}

// finish performs the one-way terminal transition. It returns false if the
// run was already terminal (first writer wins).
func (s *Store) finish(id string, status Status, output any, runErr *RunError) bool {
	s.mu.Lock()
	e, ok := s.runs[id]
	if !ok || e.run.Terminal() {
		s.mu.Unlock()
		return false
	}
	now := time.Now()
	r := e.run
	r.Status = status
	r.Phase = PhaseDone
	r.CurrentStep = ""
	r.Output = output
	r.Error = runErr
	r.FinishedAt = &now
	r.UpdatedAt = now
	r.DurationMS = now.Sub(r.CreatedAt).Milliseconds()
	for _, st := range r.Steps {
		if st.Status == StepRunning {
			st.Status = StepFailed
			st.EndedAt = &now
			st.DurationMS = now.Sub(st.StartedAt).Milliseconds()
			if runErr != nil {
				st.Error = "run ended: " + runErr.Message
			}
		}
	}
	s.dirty = true
	subs := subscribers(e)
	s.mu.Unlock()
	notify(subs)
	return true
}

// touch records a heartbeat (called on every trace change).
func (s *Store) touch(id string) {
	s.mu.Lock()
	e, ok := s.runs[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	if !e.run.Terminal() {
		e.run.HeartbeatAt = time.Now()
	}
	subs := subscribers(e)
	s.mu.Unlock()
	notify(subs)
}

// subscribers copies a run's subscriber channels. The caller must hold s.mu:
// Subscribe and its unsubscribe func mutate the map, so it must never be
// ranged over after the lock is released.
func subscribers(e *entry) []chan struct{} {
	out := make([]chan struct{}, 0, len(e.subs))
	for _, ch := range e.subs {
		out = append(out, ch)
	}
	return out
}

// notify signals each subscriber without blocking. Call it after releasing
// s.mu, with a slice taken by subscribers while the lock was held.
func notify(subs []chan struct{}) {
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default: // a notification is already queued; coalesce
		}
	}
}

// Get returns a deep copy of a run.
func (s *Store) Get(id string) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.runs[id]
	if !ok {
		return nil, ErrRunNotFound
	}
	return cloneRun(e.run), nil
}

// Trace returns the run's spans.
func (s *Store) Trace(id string) ([]trace.Span, error) {
	s.mu.Lock()
	e, ok := s.runs[id]
	s.mu.Unlock()
	if !ok {
		return nil, ErrRunNotFound
	}
	return e.trace.Snapshot(), nil
}

// RunFilter selects runs.
type RunFilter struct {
	Workflow string
	Status   Status
	Limit    int
}

// List returns run summaries (without events), newest first.
func (s *Store) List(f RunFilter) []*Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*Run{}
	for i := len(s.order) - 1; i >= 0; i-- {
		r := s.runs[s.order[i]].run
		if f.Workflow != "" && r.Workflow != f.Workflow {
			continue
		}
		if f.Status != "" && r.Status != f.Status {
			continue
		}
		c := cloneRun(r)
		c.Events = nil
		out = append(out, c)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out
}

// inProgress returns copies of every in_progress run (watchdog).
func (s *Store) inProgress() []*Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Run
	for _, id := range s.order {
		if r := s.runs[id].run; r.Status == StatusInProgress {
			out = append(out, cloneRun(r))
		}
	}
	return out
}

// Counts returns the number of runs per status.
func (s *Store) Counts() map[Status]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[Status]int{StatusInProgress: 0, StatusCompleted: 0, StatusFailed: 0}
	for _, e := range s.runs {
		out[e.run.Status]++
	}
	return out
}

// Subscribe returns a channel that receives a signal after each change to
// the run, plus an unsubscribe func.
func (s *Store) Subscribe(id string) (<-chan struct{}, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.runs[id]
	if !ok {
		return nil, nil, ErrRunNotFound
	}
	s.subSeq++
	n := s.subSeq
	ch := make(chan struct{}, 1)
	e.subs[n] = ch
	return ch, func() {
		s.mu.Lock()
		delete(e.subs, n)
		s.mu.Unlock()
	}, nil
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

type stateFile struct {
	Version int                     `json:"version"`
	SavedAt time.Time               `json:"saved_at"`
	Runs    []*Run                  `json:"runs"`
	Traces  map[string][]trace.Span `json:"traces"`
	Reviews []ReviewItem            `json:"reviews"`
}

// snapshot builds a persistable copy; it clears the dirty flag.
func (s *Store) snapshot() (*stateFile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil, false
	}
	st := &stateFile{Version: 1, SavedAt: time.Now(), Traces: map[string][]trace.Span{}}
	for _, id := range s.order {
		e := s.runs[id]
		st.Runs = append(st.Runs, cloneRun(e.run))
		st.Traces[id] = e.trace.Snapshot()
	}
	s.dirty = false
	return st, true
}

func (s *Store) markDirty() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
}

// writeState writes atomically (temp file + rename).
func writeState(path string, st *stateFile) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadState reads a state file. Runs that were in_progress are failed with
// code "interrupted": they cannot be resumed (their goroutines and pending
// human waits died with the process), and leaving them in_progress would be
// exactly the "stale pipeline" this engine promises never to show.
func (s *Store) loadState(path string) (*stateFile, int, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var st stateFile
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, 0, fmt.Errorf("parse state file %s: %w", path, err)
	}
	recovered := 0
	now := time.Now()
	sort.SliceStable(st.Runs, func(i, j int) bool { return st.Runs[i].CreatedAt.Before(st.Runs[j].CreatedAt) })
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range st.Runs {
		tr := trace.Restore(st.Traces[r.ID])
		if r.Status == StatusInProgress || (r.Status != StatusCompleted && r.Status != StatusFailed) {
			r.Status = StatusFailed
			r.Phase = PhaseDone
			r.Error = &RunError{Code: CodeInterrupted, Message: "the server stopped while this run was in progress; it was failed on restart instead of being left stale"}
			r.FinishedAt = &now
			r.UpdatedAt = now
			for _, step := range r.Steps {
				if step.Status == StepRunning {
					step.Status = StepFailed
					step.Error = "interrupted by restart"
				}
			}
			tr.CloseOpen(errors.New("interrupted by restart"))
			recovered++
		}
		s.runs[r.ID] = &entry{run: r, trace: tr, subs: map[int]chan struct{}{}}
		s.order = append(s.order, r.ID)
	}
	s.dirty = recovered > 0
	return &st, recovered, nil
}

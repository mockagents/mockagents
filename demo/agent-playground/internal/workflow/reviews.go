package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"
)

// Review kinds.
const (
	// ReviewGate is a blocking checkpoint on a workflow's final AI output.
	ReviewGate = "output_gate"
	// ReviewToolApproval blocks a side-effecting tool until a human decides.
	ReviewToolApproval = "tool_approval"
	// ReviewAudit is a non-blocking record of an intermediate AI output. A
	// human can approve or flag it at any time, even after the run ends.
	ReviewAudit = "output_audit"
)

// Review statuses.
const (
	ReviewPending      = "pending"
	ReviewApproved     = "approved"
	ReviewRejected     = "rejected"
	ReviewRevised      = "revised"
	ReviewFlagged      = "flagged"
	ReviewExpired      = "expired"
	ReviewAutoApproved = "auto_approved"
)

// Review actions.
const (
	ActionApprove = "approve"
	ActionReject  = "reject"
	ActionRevise  = "revise"
	ActionFlag    = "flag"
)

// Decision is a human (or policy) decision on a review item.
type Decision struct {
	Action    string    `json:"action"`
	Comment   string    `json:"comment,omitempty"`
	Reviewer  string    `json:"reviewer,omitempty"`
	DecidedAt time.Time `json:"decided_at"`
	Auto      bool      `json:"auto,omitempty"`
}

// ReviewItem is one human-review checkpoint.
type ReviewItem struct {
	ID             string         `json:"id"`
	RunID          string         `json:"run_id"`
	Workflow       string         `json:"workflow"`
	Kind           string         `json:"kind"`
	Blocking       bool           `json:"blocking"`
	Title          string         `json:"title"`
	Agent          string         `json:"agent,omitempty"`
	Content        string         `json:"content"`
	Context        map[string]any `json:"context,omitempty"`
	Status         string         `json:"status"`
	AllowedActions []string       `json:"allowed_actions"`
	Decision       *Decision      `json:"decision,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	Deadline       *time.Time     `json:"deadline,omitempty"`
}

// Review errors (mapped to HTTP 404 / 409 / 400).
var (
	ErrReviewNotFound   = errors.New("review not found")
	ErrReviewNotPending = errors.New("review is no longer pending")
	ErrActionNotAllowed = errors.New("action not allowed for this review")
	ErrReviewTimeout    = errors.New("review timed out")
)

// ReviewBook stores review items and wakes the goroutines waiting on them.
type ReviewBook struct {
	mu       sync.Mutex
	items    map[string]*ReviewItem
	order    []string
	waiters  map[string]chan Decision
	onChange func(runID string)
	seq      int
}

// NewReviewBook returns an empty book. onChange is called after mutations.
func NewReviewBook(onChange func(runID string)) *ReviewBook {
	return &ReviewBook{items: map[string]*ReviewItem{}, waiters: map[string]chan Decision{}, onChange: onChange}
}

// Create stores a new pending item and returns a copy of it.
func (b *ReviewBook) Create(it ReviewItem) ReviewItem {
	b.mu.Lock()
	b.seq++
	it.ID = fmt.Sprintf("rev_%04d", b.seq)
	it.Status = ReviewPending
	if it.CreatedAt.IsZero() {
		it.CreatedAt = time.Now()
	}
	if it.Blocking {
		b.waiters[it.ID] = make(chan Decision, 1)
	}
	cp := it
	b.items[it.ID] = &cp
	b.order = append(b.order, it.ID)
	b.mu.Unlock()
	b.changed(it.RunID)
	return it
}

// Get returns a copy of an item.
func (b *ReviewBook) Get(id string) (ReviewItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.items[id]
	if !ok {
		return ReviewItem{}, ErrReviewNotFound
	}
	return *it, nil
}

// Filter selects review items.
type Filter struct {
	Status   string
	RunID    string
	Kind     string
	Blocking *bool
}

// List returns matching items, newest first.
func (b *ReviewBook) List(f Filter) []ReviewItem {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []ReviewItem{}
	for i := len(b.order) - 1; i >= 0; i-- {
		it := b.items[b.order[i]]
		if f.Status != "" && it.Status != f.Status {
			continue
		}
		if f.RunID != "" && it.RunID != f.RunID {
			continue
		}
		if f.Kind != "" && it.Kind != f.Kind {
			continue
		}
		if f.Blocking != nil && it.Blocking != *f.Blocking {
			continue
		}
		out = append(out, *it)
	}
	return out
}

// Decide records a decision. Blocking items wake their waiter.
func (b *ReviewBook) Decide(id string, d Decision) (ReviewItem, error) {
	b.mu.Lock()
	it, ok := b.items[id]
	if !ok {
		b.mu.Unlock()
		return ReviewItem{}, ErrReviewNotFound
	}
	if it.Status != ReviewPending {
		cp := *it
		b.mu.Unlock()
		return cp, ErrReviewNotPending
	}
	if !slices.Contains(it.AllowedActions, d.Action) {
		cp := *it
		b.mu.Unlock()
		return cp, fmt.Errorf("%w: %q (allowed: %v)", ErrActionNotAllowed, d.Action, it.AllowedActions)
	}
	if d.DecidedAt.IsZero() {
		d.DecidedAt = time.Now()
	}
	if d.Reviewer == "" {
		d.Reviewer = "anonymous"
	}
	it.Decision = &d
	it.Status = statusFor(d)
	if ch := b.waiters[id]; ch != nil {
		ch <- d // buffered(1); one decision per item, guarded by the pending check
		delete(b.waiters, id)
	}
	cp := *it
	b.mu.Unlock()
	b.changed(cp.RunID)
	return cp, nil
}

func statusFor(d Decision) string {
	switch d.Action {
	case ActionApprove:
		if d.Auto {
			return ReviewAutoApproved
		}
		return ReviewApproved
	case ActionReject:
		return ReviewRejected
	case ActionRevise:
		return ReviewRevised
	case ActionFlag:
		return ReviewFlagged
	}
	return ReviewPending
}

// Wait blocks until item id is decided, ctx ends, or timeout elapses.
func (b *ReviewBook) Wait(ctx context.Context, id string, timeout time.Duration) (Decision, error) {
	b.mu.Lock()
	ch := b.waiters[id]
	it, ok := b.items[id]
	var decided *Decision
	if ok && it.Decision != nil {
		d := *it.Decision
		decided = &d
	}
	b.mu.Unlock()
	if !ok {
		return Decision{}, ErrReviewNotFound
	}
	if decided != nil {
		return *decided, nil
	}
	if ch == nil {
		return Decision{}, ErrReviewNotPending
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case d := <-ch:
		return d, nil
	case <-ctx.Done():
		return Decision{}, ctx.Err()
	case <-timer.C:
		return Decision{}, ErrReviewTimeout
	}
}

// Expire marks one pending item expired (no decision arrived in time).
func (b *ReviewBook) Expire(id, reason string) {
	b.mu.Lock()
	it, ok := b.items[id]
	if !ok || it.Status != ReviewPending {
		b.mu.Unlock()
		return
	}
	it.Status = ReviewExpired
	it.Decision = &Decision{Action: "expire", Comment: reason, Reviewer: "system", DecidedAt: time.Now(), Auto: true}
	delete(b.waiters, id)
	runID := it.RunID
	b.mu.Unlock()
	b.changed(runID)
}

// ExpireRun expires every pending BLOCKING item of a finished run. Audit
// items stay open: post-hoc review of a finished run's outputs is legitimate.
func (b *ReviewBook) ExpireRun(runID, reason string) {
	b.mu.Lock()
	var ids []string
	for _, id := range b.order {
		it := b.items[id]
		if it.RunID == runID && it.Blocking && it.Status == ReviewPending {
			ids = append(ids, id)
		}
	}
	b.mu.Unlock()
	for _, id := range ids {
		b.Expire(id, reason)
	}
}

// All returns every item in creation order (persistence).
func (b *ReviewBook) All() []ReviewItem {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ReviewItem, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, *b.items[id])
	}
	return out
}

// Restore loads items (persistence). Pending blocking items are expired,
// because their runs did not survive the restart.
func (b *ReviewBook) Restore(items []ReviewItem) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	for _, it := range items {
		cp := it
		if cp.Blocking && cp.Status == ReviewPending {
			cp.Status = ReviewExpired
			cp.Decision = &Decision{Action: "expire", Comment: "server restarted", Reviewer: "system", DecidedAt: time.Now(), Auto: true}
		}
		b.items[cp.ID] = &cp
		b.order = append(b.order, cp.ID)
		var n int
		if _, err := fmt.Sscanf(cp.ID, "rev_%d", &n); err == nil && n > b.seq {
			b.seq = n
		}
	}
}

func (b *ReviewBook) changed(runID string) {
	if b.onChange != nil {
		b.onChange(runID)
	}
}

// Package workflow is the playground's orchestration engine. It runs workflow
// definitions as background runs and guarantees that a run never gets stuck.
//
// A run's Status has exactly three values: in_progress, completed and
// failed. Every in_progress run is guaranteed to reach one of the other two
// because:
//
//  1. Every run executes under a context with a hard deadline (run_timeout).
//  2. Every model call is bounded (per-attempt timeout x at most 5 retries,
//     with backoff that never sleeps past the deadline); every tool call has
//     its own timeout; every tool loop is capped at max_tool_turns.
//  3. Every human wait (review gate, tool approval) has a review timeout that
//     is shorter than the run timeout, with an explicit on_timeout policy.
//  4. A panic inside a workflow is recovered and fails the run.
//  5. A watchdog fails any run that passes its deadline or stops making
//     progress (no heartbeat) while not waiting on a human.
//  6. The terminal transition is first-writer-wins, so a run cannot flip
//     back, and finishing cancels the run's context, closes open trace spans
//     and expires its pending reviews.
//  7. With a state file, runs that were in_progress when the process died are
//     failed with code "interrupted" on restart, never resumed into limbo.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/retry"
)

// Status is a run's lifecycle state. Only these three values exist.
type Status string

// Run statuses.
const (
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

// Phases describe what an in_progress run is doing. Phase is informational;
// Status is the state machine.
const (
	PhaseStarting         = "starting"
	PhaseRunning          = "running"
	PhaseAwaitingReview   = "awaiting_review"
	PhaseAwaitingApproval = "awaiting_approval"
	PhaseDone             = "done"
)

// Error codes recorded on failed runs.
const (
	CodeAgentFailed      = "agent_failed"
	CodeGuardFailed      = "guard_failed"
	CodeReviewRejected   = "review_rejected"
	CodeReviewTimeout    = "review_timeout"
	CodeDeadlineExceeded = "deadline_exceeded"
	CodeWatchdogStalled  = "watchdog_stalled"
	CodeCancelled        = "cancelled"
	CodePanic            = "panic"
	CodeInterrupted      = "interrupted"
	CodeStepFailed       = "step_failed"
	CodeDrillFailed      = "drill_failed"
	CodeShutdown         = "shutdown"
)

// RunError explains a failed run.
type RunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Step    string `json:"step,omitempty"`
}

func (e *RunError) Error() string { return e.Code + ": " + e.Message }

// Fail builds a *RunError (workflows return it to pick their own code).
func Fail(code, format string, args ...any) *RunError {
	return &RunError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// RunOptions are per-run overrides supplied at launch.
type RunOptions struct {
	// ReviewMode overrides defaults.review.mode ("manual" | "auto").
	ReviewMode string `json:"review_mode,omitempty"`
	// ReviewTimeoutMS overrides defaults.review.timeout_ms.
	ReviewTimeoutMS int `json:"review_timeout_ms,omitempty"`
	// RunTimeoutMS overrides defaults.run_timeout_ms.
	RunTimeoutMS int `json:"run_timeout_ms,omitempty"`
	// Retry overrides every agent's retry policy for this run.
	Retry *retry.Policy `json:"retry,omitempty"`
	// Tiers pins agents to a tier for this run: {"summarizer": "llm"}.
	Tiers map[string]string `json:"tiers,omitempty"`
	// Stream asks streaming-capable agents to stream (forwarded to SSE clients).
	Stream bool `json:"stream,omitempty"`
}

// Validate checks option ranges.
func (o RunOptions) Validate() error {
	var errs []error
	if o.ReviewMode != "" && o.ReviewMode != "manual" && o.ReviewMode != "auto" {
		errs = append(errs, fmt.Errorf("options.review_mode must be manual or auto"))
	}
	if o.ReviewTimeoutMS != 0 && (o.ReviewTimeoutMS < 1000 || o.ReviewTimeoutMS > 24*3600*1000) {
		errs = append(errs, fmt.Errorf("options.review_timeout_ms must be between 1000 and 86400000"))
	}
	if o.RunTimeoutMS != 0 && (o.RunTimeoutMS < 1000 || o.RunTimeoutMS > 24*3600*1000) {
		errs = append(errs, fmt.Errorf("options.run_timeout_ms must be between 1000 and 86400000"))
	}
	if o.Retry != nil {
		if err := o.Retry.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("options.retry: %w", err))
		}
	}
	for agent, tier := range o.Tiers {
		if tier != "slm" && tier != "llm" && tier != "auto" {
			errs = append(errs, fmt.Errorf("options.tiers.%s must be slm, llm or auto", agent))
		}
	}
	return errors.Join(errs...)
}

// Step statuses.
const (
	StepRunning   = "running"
	StepCompleted = "completed"
	StepFailed    = "failed"
	StepSkipped   = "skipped"
)

// Step is one recorded unit of a run.
type Step struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Agent      string     `json:"agent,omitempty"`
	Tier       string     `json:"tier,omitempty"`
	Model      string     `json:"model,omitempty"`
	Attempts   int        `json:"attempts,omitempty"`
	Retries    int        `json:"retries,omitempty"`
	Fallback   bool       `json:"fallback,omitempty"`
	Escalated  bool       `json:"escalated,omitempty"`
	CostUSD    float64    `json:"cost_usd,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	DurationMS int64      `json:"duration_ms"`
	Output     any        `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	Notes      []string   `json:"notes,omitempty"`
}

// Stats aggregates a run's model activity.
type Stats struct {
	LLMCalls    int     `json:"llm_calls"`
	Attempts    int     `json:"attempts"`
	Retries     int     `json:"retries"`
	Fallbacks   int     `json:"fallbacks"`
	Escalations int     `json:"escalations"`
	ToolCalls   int     `json:"tool_calls"`
	TokensIn    int     `json:"tokens_in"`
	TokensOut   int     `json:"tokens_out"`
	CostUSD     float64 `json:"cost_usd"`
	SLMCalls    int     `json:"slm_calls"`
	LLMTierCall int     `json:"llm_tier_calls"`
}

// RunEvent is one entry in a run's live event log.
type RunEvent struct {
	Time time.Time      `json:"time"`
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

// maxEvents bounds a run's event log.
const maxEvents = 400

// Run is one execution of a workflow.
type Run struct {
	ID          string         `json:"id"`
	Workflow    string         `json:"workflow"`
	Status      Status         `json:"status"`
	Phase       string         `json:"phase"`
	CurrentStep string         `json:"current_step,omitempty"`
	Input       map[string]any `json:"input"`
	Options     RunOptions     `json:"options"`
	Output      any            `json:"output,omitempty"`
	Error       *RunError      `json:"error,omitempty"`
	Steps       []*Step        `json:"steps"`
	ReviewIDs   []string       `json:"review_ids"`
	Stats       Stats          `json:"stats"`
	Events      []RunEvent     `json:"events,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	HeartbeatAt time.Time      `json:"heartbeat_at"`
	Deadline    time.Time      `json:"deadline"`
	FinishedAt  *time.Time     `json:"finished_at,omitempty"`
	DurationMS  int64          `json:"duration_ms"`
}

// Terminal reports whether the run has finished.
func (r *Run) Terminal() bool { return r.Status == StatusCompleted || r.Status == StatusFailed }

// Waiting reports whether the run is blocked on a human (the watchdog's
// stall check skips these; the review timeout bounds them instead).
func (r *Run) Waiting() bool {
	return r.Phase == PhaseAwaitingReview || r.Phase == PhaseAwaitingApproval
}

// Info describes a workflow for the API and UI.
type Info struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Patterns    []string       `json:"patterns"`
	Agents      []string       `json:"agents"`
	InputSchema map[string]any `json:"input_schema"`
	Examples    []Example      `json:"examples"`
	Hidden      bool           `json:"hidden,omitempty"`
}

// Example is a ready-to-run input.
type Example struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Input       map[string]any `json:"input"`
	Options     *RunOptions    `json:"options,omitempty"`
}

// Definition is a runnable workflow.
type Definition interface {
	Info() Info
	// Validate rejects bad input before a run is created (HTTP 400).
	Validate(input map[string]any) error
	// Run executes the workflow. Returning nil completes the run with the
	// output; returning an error fails it (a *RunError picks the code).
	Run(ctx context.Context, x *Exec) (any, error)
}

// Sentinel causes used to cancel run contexts.
var (
	errCancelled = errors.New("cancelled by user")
	errWatchdog  = errors.New("watchdog")
	errShutdown  = errors.New("server shutting down")
)

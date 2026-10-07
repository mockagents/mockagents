package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/metrics"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/trace"
)

// ErrUnknownWorkflow is returned for unregistered workflow names.
var ErrUnknownWorkflow = errors.New("unknown workflow")

// InputError is a launch request rejected before a run is created.
type InputError struct{ Err error }

func (e *InputError) Error() string { return e.Err.Error() }
func (e *InputError) Unwrap() error { return e.Err }

// Engine runs workflows.
type Engine struct {
	Runtime *agents.Runtime
	Config  *config.Store
	Store   *Store
	Reviews *ReviewBook
	Metrics *metrics.Registry
	Logger  *slog.Logger
	// Arm, when set, re-arms stateful mock faults (the resilience drill uses
	// it to reset mockagents' fail_first counters). It may be nil.
	Arm func(ctx context.Context, mockAgents []string) error
	// StatePath enables persistence when non-empty.
	StatePath string
	// WatchdogGrace is added to a run's deadline before the watchdog fires.
	WatchdogGrace time.Duration

	mu      sync.Mutex
	defs    map[string]Definition
	cancels map[string]context.CancelCauseFunc
	wg      sync.WaitGroup
	seq     int
	closed  bool
}

// NewEngine wires an engine.
func NewEngine(rt *agents.Runtime, cfg *config.Store, m *metrics.Registry, logger *slog.Logger) *Engine {
	e := &Engine{
		Runtime:       rt,
		Config:        cfg,
		Store:         NewStore(500),
		Metrics:       m,
		Logger:        logger,
		WatchdogGrace: 2 * time.Second,
		defs:          map[string]Definition{},
		cancels:       map[string]context.CancelCauseFunc{},
	}
	e.Reviews = NewReviewBook(func(runID string) { e.Store.touch(runID); e.Store.markDirty() })
	return e
}

// Register adds a workflow definition.
func (e *Engine) Register(defs ...Definition) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, d := range defs {
		e.defs[d.Info().Name] = d
	}
}

// Definitions lists registered workflows, sorted by name.
func (e *Engine) Definitions(includeHidden bool) []Info {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Info, 0, len(e.defs))
	for _, d := range e.defs {
		if info := d.Info(); includeHidden || !info.Hidden {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Definition returns one workflow's info.
func (e *Engine) Definition(name string) (Info, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	d, ok := e.defs[name]
	if !ok {
		return Info{}, false
	}
	return d.Info(), true
}

// StartOptions carries non-serializable launch hooks.
type StartOptions struct {
	// OnEvent receives live agent events (SSE streaming).
	OnEvent func(agents.Event)
}

// Start validates the request, creates the run (status in_progress) and
// executes it in the background. It returns the initial snapshot.
func (e *Engine) Start(name string, input map[string]any, opts RunOptions, so StartOptions) (*Run, error) {
	e.mu.Lock()
	def, ok := e.defs[name]
	closed := e.closed
	e.mu.Unlock()
	if closed {
		return nil, &InputError{Err: errShutdown}
	}
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownWorkflow, name)
	}
	if input == nil {
		input = map[string]any{}
	}
	if err := opts.Validate(); err != nil {
		return nil, &InputError{Err: err}
	}
	if err := def.Validate(input); err != nil {
		return nil, &InputError{Err: err}
	}
	cfg := e.Config.Current()
	if opts.Retry != nil && opts.Retry.MaxBackoffMS+config.StallMarginMS >= cfg.Workflows.WatchdogStallMS {
		return nil, &InputError{Err: fmt.Errorf("options.retry.max_backoff_ms must be at least %d ms below workflows.watchdog_stall_ms (%d)", config.StallMarginMS, cfg.Workflows.WatchdogStallMS)}
	}
	for agent := range opts.Tiers {
		if _, ok := cfg.Agent(agent); !ok {
			return nil, &InputError{Err: fmt.Errorf("options.tiers: unknown agent %q", agent)}
		}
	}
	timeout := time.Duration(cfg.Defaults.RunTimeoutMS) * time.Millisecond
	if opts.RunTimeoutMS > 0 {
		timeout = time.Duration(opts.RunTimeoutMS) * time.Millisecond
	}
	now := time.Now()
	e.mu.Lock()
	e.seq++
	id := fmt.Sprintf("run_%s_%04d", now.UTC().Format("150405"), e.seq)
	e.mu.Unlock()
	run := &Run{
		ID: id, Workflow: name, Status: StatusInProgress, Phase: PhaseStarting,
		Input: input, Options: opts, Steps: []*Step{}, ReviewIDs: []string{},
		CreatedAt: now, UpdatedAt: now, HeartbeatAt: now, Deadline: now.Add(timeout),
	}
	tr := trace.New(func() { e.Store.touch(id) })
	e.Store.add(run, tr)

	base, cancel := context.WithCancelCause(context.Background())
	ctx, cancelDeadline := context.WithDeadline(base, run.Deadline)
	e.mu.Lock()
	e.cancels[id] = cancel
	e.mu.Unlock()
	e.Metrics.Inc("playground_runs_started_total", "workflow", name)
	e.refreshGauge()

	x := &Exec{e: e, runID: id, workflow: name, input: input, opts: opts, onEvent: so.OnEvent, trace: tr}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer cancelDeadline()
		e.execute(trace.WithTrace(ctx, tr), def, x)
	}()
	return e.Store.Get(id)
}

func (e *Engine) execute(ctx context.Context, def Definition, x *Exec) {
	var (
		out any
		err error
	)
	ctx, wspan := trace.Start(ctx, "workflow "+x.workflow, trace.KindWorkflow, map[string]any{"run_id": x.runID})
	func() {
		defer func() {
			if r := recover(); r != nil {
				e.Logger.Error("workflow panic", "run", x.runID, "panic", r, "stack", string(debug.Stack()))
				err = &RunError{Code: CodePanic, Message: fmt.Sprintf("workflow panicked: %v", r)}
			}
		}()
		x.setPhase(PhaseRunning)
		out, err = def.Run(ctx, x)
	}()
	wspan.End(err)
	if err == nil {
		e.finish(x.runID, StatusCompleted, out, nil)
		return
	}
	e.finish(x.runID, StatusFailed, out, e.classify(ctx, err, x))
}

// classify maps a workflow error to a RunError.
func (e *Engine) classify(ctx context.Context, err error, x *Exec) *RunError {
	step := x.lastStep()
	if cause := context.Cause(ctx); cause != nil {
		switch {
		case errors.Is(cause, errCancelled):
			return &RunError{Code: CodeCancelled, Message: cause.Error(), Step: step}
		case errors.Is(cause, errWatchdog):
			return &RunError{Code: CodeWatchdogStalled, Message: cause.Error(), Step: step}
		case errors.Is(cause, errShutdown):
			return &RunError{Code: CodeShutdown, Message: cause.Error(), Step: step}
		case errors.Is(cause, context.DeadlineExceeded):
			return &RunError{Code: CodeDeadlineExceeded, Message: "run exceeded its run_timeout", Step: step}
		}
	}
	var re *RunError
	if errors.As(err, &re) {
		if re.Step == "" {
			re.Step = step
		}
		return re
	}
	var ce *agents.CallError
	if errors.As(err, &ce) {
		return &RunError{Code: CodeAgentFailed, Message: err.Error(), Step: step}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &RunError{Code: CodeDeadlineExceeded, Message: "run exceeded its run_timeout", Step: step}
	}
	return &RunError{Code: CodeStepFailed, Message: err.Error(), Step: step}
}

// finish is the single terminal transition. Whoever arrives first (the
// workflow goroutine, Cancel, or the watchdog) wins; later calls are no-ops.
func (e *Engine) finish(id string, status Status, out any, runErr *RunError) bool {
	if !e.Store.finish(id, status, out, runErr) {
		return false
	}
	reason := "run " + string(status)
	if runErr != nil {
		reason = "run failed: " + runErr.Code
	}
	e.Reviews.ExpireRun(id, reason)
	e.Store.mu.Lock()
	ent := e.Store.runs[id]
	e.Store.mu.Unlock()
	if ent != nil {
		ent.trace.CloseOpen(errors.New(reason))
	}
	e.mu.Lock()
	cancel := e.cancels[id]
	delete(e.cancels, id)
	e.mu.Unlock()
	if cancel != nil {
		cancel(errors.New("run finished"))
	}
	run, _ := e.Store.Get(id)
	code := ""
	if runErr != nil {
		code = runErr.Code
	}
	if run != nil {
		e.Metrics.Inc("playground_runs_finished_total", "workflow", run.Workflow, "status", string(status), "code", code)
	}
	e.refreshGauge()
	e.Logger.Info("run finished", "run", id, "status", status, "code", code)
	e.persist()
	return true
}

// Cancel fails a run on request. It returns false if the run already finished.
func (e *Engine) Cancel(id, reason string) (bool, error) {
	run, err := e.Store.Get(id)
	if err != nil {
		return false, err
	}
	if run.Terminal() {
		return false, nil
	}
	msg := "cancelled by user"
	if reason != "" {
		msg += ": " + reason
	}
	e.mu.Lock()
	cancel := e.cancels[id]
	e.mu.Unlock()
	if cancel != nil {
		cancel(fmt.Errorf("%w: %s", errCancelled, reason))
	}
	return e.finish(id, StatusFailed, nil, &RunError{Code: CodeCancelled, Message: msg}), nil
}

// RunWatchdog checks in_progress runs every interval until ctx ends.
func (e *Engine) RunWatchdog(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.CheckStale(time.Now())
		}
	}
}

// CheckStale applies the watchdog rules once and returns the ids it failed.
func (e *Engine) CheckStale(now time.Time) []string {
	stall := time.Duration(e.Config.Current().Workflows.WatchdogStallMS) * time.Millisecond
	var killed []string
	for _, r := range e.Store.inProgress() {
		var re *RunError
		switch {
		case now.After(r.Deadline.Add(e.WatchdogGrace)):
			re = &RunError{Code: CodeDeadlineExceeded, Message: fmt.Sprintf("watchdog: run passed its deadline (%s)", r.Deadline.Format(time.RFC3339))}
		case !r.Waiting() && now.Sub(r.HeartbeatAt) > stall:
			re = &RunError{Code: CodeWatchdogStalled, Message: fmt.Sprintf("watchdog: no progress for %s", now.Sub(r.HeartbeatAt).Round(time.Second))}
		}
		if re == nil {
			continue
		}
		re.Step = r.CurrentStep
		e.mu.Lock()
		cancel := e.cancels[r.ID]
		e.mu.Unlock()
		if cancel != nil {
			cancel(fmt.Errorf("%w: %s", errWatchdog, re.Message))
		}
		if e.finish(r.ID, StatusFailed, nil, re) {
			e.Metrics.Inc("playground_watchdog_kills_total", "code", re.Code)
			e.Logger.Warn("watchdog failed a run", "run", r.ID, "code", re.Code, "reason", re.Message)
			killed = append(killed, r.ID)
		}
	}
	return killed
}

// Wait blocks until the run is terminal, until(run) is true, or ctx ends,
// and returns the latest snapshot.
func (e *Engine) Wait(ctx context.Context, id string, until func(*Run) bool) (*Run, error) {
	ch, unsub, err := e.Store.Subscribe(id)
	if err != nil {
		return nil, err
	}
	defer unsub()
	for {
		run, err := e.Store.Get(id)
		if err != nil {
			return nil, err
		}
		if run.Terminal() || (until != nil && until(run)) {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return run, nil
		case <-ch:
		}
	}
}

// Shutdown stops accepting runs, fails every in_progress run with code
// "shutdown" (pending human waits cannot survive the process) and waits for
// workflow goroutines to exit.
func (e *Engine) Shutdown(ctx context.Context) {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	for _, r := range e.Store.inProgress() {
		e.mu.Lock()
		cancel := e.cancels[r.ID]
		e.mu.Unlock()
		if cancel != nil {
			cancel(errShutdown)
		}
		e.finish(r.ID, StatusFailed, nil, &RunError{Code: CodeShutdown, Message: "server shut down while the run was in progress", Step: r.CurrentStep})
	}
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	e.persist()
}

// LoadState restores runs and reviews from StatePath.
func (e *Engine) LoadState() (int, error) {
	if e.StatePath == "" {
		return 0, nil
	}
	st, recovered, err := e.Store.loadState(e.StatePath)
	if err != nil || st == nil {
		return 0, err
	}
	e.Reviews.Restore(st.Reviews)
	e.mu.Lock()
	e.seq = len(st.Runs)
	e.mu.Unlock()
	e.persist()
	return recovered, nil
}

// RunPersister flushes state every interval until ctx ends.
func (e *Engine) RunPersister(ctx context.Context, interval time.Duration) {
	if e.StatePath == "" {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.persist()
		}
	}
}

var persistMu sync.Mutex

func (e *Engine) persist() {
	if e.StatePath == "" {
		return
	}
	persistMu.Lock()
	defer persistMu.Unlock()
	st, dirty := e.Store.snapshot()
	if !dirty {
		return
	}
	st.Reviews = e.Reviews.All()
	if err := writeState(e.StatePath, st); err != nil {
		e.Logger.Warn("could not persist state", "path", e.StatePath, "error", err)
		e.Store.markDirty()
	}
}

func (e *Engine) refreshGauge() {
	e.Metrics.Set("playground_runs_in_progress", float64(e.Store.Counts()[StatusInProgress]))
}

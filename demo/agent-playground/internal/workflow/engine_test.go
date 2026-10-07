package workflow

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/metrics"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/tools"
)

// fn is a test workflow whose body is a closure.
type fn struct {
	name string
	run  func(ctx context.Context, x *Exec) (any, error)
}

func (f fn) Info() Info                                    { return Info{Name: f.name} }
func (f fn) Validate(map[string]any) error                 { return nil }
func (f fn) Run(ctx context.Context, x *Exec) (any, error) { return f.run(ctx, x) }

func newEngine(t *testing.T) *Engine {
	t.Helper()
	reg := tools.NewRegistry()
	cfg, err := config.Load("../../config/playground.json", reg.Names())
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(cfg, reg.Names())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := metrics.New()
	rt := &agents.Runtime{Config: store, Providers: agents.NewProviders("http://127.0.0.1:1", ""), Tools: reg, Metrics: m, Logger: logger}
	e := NewEngine(rt, store, m, logger)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		e.Shutdown(ctx)
	})
	return e
}

func wait(t *testing.T, e *Engine, id string) *Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := e.Wait(ctx, id, nil)
	if err != nil || !r.Terminal() {
		t.Fatalf("run %s did not finish: %+v %v", id, r, err)
	}
	return r
}

func TestPanicFailsTheRun(t *testing.T) {
	e := newEngine(t)
	e.Register(fn{"boom", func(context.Context, *Exec) (any, error) { panic("kaboom") }})
	r, _ := e.Start("boom", nil, RunOptions{}, StartOptions{})
	r = wait(t, e, r.ID)
	if r.Status != StatusFailed || r.Error.Code != CodePanic {
		t.Fatalf("got %s %+v", r.Status, r.Error)
	}
}

func TestRunDeadlineFailsTheRun(t *testing.T) {
	e := newEngine(t)
	e.Register(fn{"slow", func(ctx context.Context, x *Exec) (any, error) {
		_, err := x.Step(ctx, "sleep", "logic", func(ctx context.Context, _ *StepHandle) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
		return nil, err
	}})
	r, _ := e.Start("slow", nil, RunOptions{RunTimeoutMS: 1000}, StartOptions{})
	r = wait(t, e, r.ID)
	if r.Status != StatusFailed || r.Error.Code != CodeDeadlineExceeded || r.Error.Step != "sleep" {
		t.Fatalf("got %s %+v", r.Status, r.Error)
	}
	if r.Steps[0].Status != StepFailed {
		t.Errorf("open step must be closed as failed: %+v", r.Steps[0])
	}
}

// A workflow that ignores its context entirely (a bug) is still failed by
// the watchdog: first for stalling, and in any case at its deadline.
func TestWatchdogFailsAStuckRun(t *testing.T) {
	e := newEngine(t)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	e.Register(fn{"stuck", func(ctx context.Context, x *Exec) (any, error) {
		<-block // ignores ctx on purpose
		return nil, nil
	}})
	r, _ := e.Start("stuck", nil, RunOptions{}, StartOptions{})
	time.Sleep(50 * time.Millisecond)
	if got := e.CheckStale(time.Now()); len(got) != 0 {
		t.Fatalf("a fresh run must not be killed: %v", got)
	}
	stall := time.Duration(e.Config.Current().Workflows.WatchdogStallMS) * time.Millisecond
	killed := e.CheckStale(time.Now().Add(stall + time.Second))
	if len(killed) != 1 || killed[0] != r.ID {
		t.Fatalf("watchdog should fail the stalled run, killed %v", killed)
	}
	got, _ := e.Store.Get(r.ID)
	if got.Status != StatusFailed || got.Error.Code != CodeWatchdogStalled {
		t.Fatalf("got %s %+v", got.Status, got.Error)
	}
	if e.Metrics.Sum("playground_watchdog_kills_total") != 1 {
		t.Error("watchdog kill not counted")
	}
}

func TestWatchdogSparesRunsWaitingOnHumans(t *testing.T) {
	e := newEngine(t)
	e.Register(fn{"gate", func(ctx context.Context, x *Exec) (any, error) {
		_, _, err := x.Gate(ctx, "t", "a", "c", nil, false)
		return nil, err
	}})
	r, _ := e.Start("gate", nil, RunOptions{ReviewMode: "manual"}, StartOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.Wait(ctx, r.ID, (*Run).Waiting); err != nil {
		t.Fatal(err)
	}
	stall := time.Duration(e.Config.Current().Workflows.WatchdogStallMS) * time.Millisecond
	if killed := e.CheckStale(time.Now().Add(stall + time.Second)); len(killed) != 0 {
		t.Fatal("a run awaiting review is bounded by its review timeout, not the stall check")
	}
	// ...but never past its deadline.
	got, _ := e.Store.Get(r.ID)
	if killed := e.CheckStale(got.Deadline.Add(time.Minute)); len(killed) != 1 {
		t.Fatal("the deadline applies even while waiting on a human")
	}
	got = wait(t, e, r.ID)
	if got.Error.Code != CodeDeadlineExceeded {
		t.Fatalf("code %s", got.Error.Code)
	}
	if items := e.Reviews.List(Filter{RunID: r.ID}); items[0].Status != ReviewExpired {
		t.Errorf("the pending gate must expire with its run: %+v", items[0])
	}
}

func TestTerminalTransitionIsFinal(t *testing.T) {
	e := newEngine(t)
	release := make(chan struct{})
	e.Register(fn{"late", func(ctx context.Context, x *Exec) (any, error) {
		<-release
		return "finished late", nil
	}})
	r, _ := e.Start("late", nil, RunOptions{}, StartOptions{})
	if ok, _ := e.Cancel(r.ID, "test"); !ok {
		t.Fatal("cancel failed")
	}
	close(release)
	time.Sleep(100 * time.Millisecond)
	got, _ := e.Store.Get(r.ID)
	if got.Status != StatusFailed || got.Error.Code != CodeCancelled || got.Output != nil {
		t.Fatalf("a finished run must not flip back: %s %v %+v", got.Status, got.Output, got.Error)
	}
}

func TestStateFileRecoveryFailsInterruptedRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e1 := newEngine(t)
	e1.StatePath = path
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	e1.Register(fn{"hang", func(ctx context.Context, x *Exec) (any, error) { <-hold; return nil, nil }},
		fn{"ok", func(context.Context, *Exec) (any, error) { return "done", nil }})
	okRun, _ := e1.Start("ok", nil, RunOptions{}, StartOptions{})
	wait(t, e1, okRun.ID)
	hung, _ := e1.Start("hang", nil, RunOptions{}, StartOptions{})
	e1.persist() // simulate a crash: state on disk still says in_progress

	e2 := newEngine(t)
	e2.StatePath = path
	n, err := e2.LoadState()
	if err != nil || n != 1 {
		t.Fatalf("recovered=%d err=%v", n, err)
	}
	got, _ := e2.Store.Get(hung.ID)
	if got.Status != StatusFailed || got.Error.Code != CodeInterrupted {
		t.Fatalf("interrupted run: %s %+v", got.Status, got.Error)
	}
	if done, _ := e2.Store.Get(okRun.ID); done.Status != StatusCompleted || done.Output != "done" {
		t.Fatalf("completed run must survive: %+v", done)
	}
	if spans, _ := e2.Store.Trace(hung.ID); len(spans) == 0 {
		t.Error("trace should be restored")
	}
}

func TestReviewBookSemantics(t *testing.T) {
	b := NewReviewBook(nil)
	it := b.Create(ReviewItem{RunID: "r", Kind: ReviewGate, Blocking: true, AllowedActions: []string{ActionApprove, ActionReject}})
	if _, err := b.Decide(it.ID, Decision{Action: ActionRevise}); !errors.Is(err, ErrActionNotAllowed) {
		t.Errorf("revise not allowed: %v", err)
	}
	done := make(chan Decision, 1)
	go func() { d, _ := b.Wait(context.Background(), it.ID, time.Second); done <- d }()
	time.Sleep(20 * time.Millisecond)
	if _, err := b.Decide(it.ID, Decision{Action: ActionApprove, Reviewer: "x"}); err != nil {
		t.Fatal(err)
	}
	if d := <-done; d.Action != ActionApprove {
		t.Fatalf("waiter got %+v", d)
	}
	if _, err := b.Decide(it.ID, Decision{Action: ActionReject}); !errors.Is(err, ErrReviewNotPending) {
		t.Errorf("second decision: %v", err)
	}
	it2 := b.Create(ReviewItem{RunID: "r", Blocking: true, AllowedActions: []string{ActionApprove}})
	if _, err := b.Wait(context.Background(), it2.ID, 20*time.Millisecond); !errors.Is(err, ErrReviewTimeout) {
		t.Errorf("timeout: %v", err)
	}
	audit := b.Create(ReviewItem{RunID: "r", Kind: ReviewAudit, AllowedActions: []string{ActionApprove, ActionFlag}})
	b.ExpireRun("r", "done")
	if got, _ := b.Get(it2.ID); got.Status != ReviewExpired {
		t.Errorf("blocking item should expire: %s", got.Status)
	}
	if got, _ := b.Get(audit.ID); got.Status != ReviewPending {
		t.Errorf("audit items stay open after the run: %s", got.Status)
	}
}

func TestShutdownFailsInFlightRuns(t *testing.T) {
	e := newEngine(t)
	e.Register(fn{"gate", func(ctx context.Context, x *Exec) (any, error) {
		_, _, err := x.Gate(ctx, "t", "a", "c", nil, false)
		return nil, err
	}})
	r, _ := e.Start("gate", nil, RunOptions{ReviewMode: "manual"}, StartOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.Wait(ctx, r.ID, (*Run).Waiting); err != nil {
		t.Fatal(err)
	}
	e.Shutdown(ctx)
	got, _ := e.Store.Get(r.ID)
	if got.Status != StatusFailed || got.Error.Code != CodeShutdown {
		t.Fatalf("got %s %+v", got.Status, got.Error)
	}
	if _, err := e.Start("gate", nil, RunOptions{}, StartOptions{}); err == nil {
		t.Fatal("a shut-down engine must refuse new runs")
	}
}

func TestOptionsValidation(t *testing.T) {
	bad := []RunOptions{{ReviewMode: "maybe"}, {RunTimeoutMS: 10}, {Tiers: map[string]string{"x": "xl"}}}
	for _, o := range bad {
		if o.Validate() == nil {
			t.Errorf("%+v should be invalid", o)
		}
	}
}

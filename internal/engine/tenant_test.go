package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

func makeTenantAgent(name, model, tenantID string) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: name, TenantID: tenantID},
		Spec: types.AgentSpec{
			Model: model,
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "default",
					Response: types.ScenarioResponse{Content: "from " + name},
				}},
			},
		},
	}
}

// --- Registry visibility ---

func TestAgentRegistry_GetForTenant_GlobalVisibleToAll(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("global-bot", "gpt-4o", ""))

	if got := r.GetForTenant("global-bot", ""); got == nil {
		t.Error("anonymous caller should see global agent")
	}
	if got := r.GetForTenant("global-bot", "ten_a"); got == nil {
		t.Error("tenant caller should also see global agent")
	}
}

func TestAgentRegistry_GetForTenant_ScopedHiddenFromOthers(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("acme-bot", "gpt-4o", "ten_acme"))

	if got := r.GetForTenant("acme-bot", "ten_acme"); got == nil {
		t.Error("owner tenant should see the agent")
	}
	if got := r.GetForTenant("acme-bot", "ten_other"); got != nil {
		t.Error("other tenant should NOT see the agent")
	}
	if got := r.GetForTenant("acme-bot", ""); got != nil {
		t.Error("anonymous caller should NOT see tenant-scoped agent")
	}
}

func TestAgentRegistry_GetByModelForTenant_PrefersOwner(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("global", "shared-model", ""))
	r.Register(makeTenantAgent("acme", "shared-model", "ten_acme"))

	// Tenant caller resolves to their own override.
	got := r.GetByModelForTenant("shared-model", "ten_acme")
	if got == nil || got.Metadata.Name != "acme" {
		t.Errorf("tenant resolution mismatch: %+v", got)
	}
	// Anonymous caller falls back to the global agent.
	got = r.GetByModelForTenant("shared-model", "")
	if got == nil || got.Metadata.Name != "global" {
		t.Errorf("anonymous resolution mismatch: %+v", got)
	}
	// A different tenant also gets the global one (not the acme override).
	got = r.GetByModelForTenant("shared-model", "ten_other")
	if got == nil || got.Metadata.Name != "global" {
		t.Errorf("other-tenant resolution mismatch: %+v", got)
	}
}

// TestAgentRegistry_GetByModelForTenant_DeterministicOwnerTieBreak covers
// F-AR-002: when several same-tenant agents share a model, the lookup must
// always return the lexicographically smallest name rather than whichever
// the randomized map iteration happens to reach first. Looping exercises a
// range of iteration orders — the pre-fix "return first match" would yield
// a different name on some iterations.
func TestAgentRegistry_GetByModelForTenant_DeterministicOwnerTieBreak(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("zeta", "shared", "ten_acme"))
	r.Register(makeTenantAgent("alpha", "shared", "ten_acme"))
	r.Register(makeTenantAgent("mid", "shared", "ten_acme"))

	for i := 0; i < 50; i++ {
		got := r.GetByModelForTenant("shared", "ten_acme")
		if got == nil || got.Metadata.Name != "alpha" {
			t.Fatalf("iter %d: owner tie-break not deterministic: got %v, want alpha", i, got)
		}
	}
}

// TestAgentRegistry_GetByModelForTenant_DeterministicGlobalTieBreak covers
// the F-AR-002 fallback path: a tenant caller with no own agent for the
// model must deterministically resolve to the smallest-named global.
func TestAgentRegistry_GetByModelForTenant_DeterministicGlobalTieBreak(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("g-zeta", "shared", ""))
	r.Register(makeTenantAgent("g-alpha", "shared", ""))

	for i := 0; i < 50; i++ {
		got := r.GetByModelForTenant("shared", "ten_acme")
		if got == nil || got.Metadata.Name != "g-alpha" {
			t.Fatalf("iter %d: global tie-break not deterministic: got %v, want g-alpha", i, got)
		}
	}
}

// TestAgentRegistry_GetByModelForTenant_IndexMaintained covers the PERF-01
// byModelTenant index staying correct as agents are removed and re-registered
// under a different model.
func TestAgentRegistry_GetByModelForTenant_IndexMaintained(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("alpha", "m", ""))
	r.Register(makeTenantAgent("bravo", "m", ""))
	if got := r.GetByModelForTenant("m", ""); got == nil || got.Metadata.Name != "alpha" {
		t.Fatalf("smallest-name winner: got %v, want alpha", got)
	}

	// Remove the winner → the next-smallest must answer.
	if err := r.Remove("alpha"); err != nil {
		t.Fatal(err)
	}
	if got := r.GetByModelForTenant("m", ""); got == nil || got.Metadata.Name != "bravo" {
		t.Errorf("after removing alpha: got %v, want bravo", got)
	}

	// Re-register bravo under a different model → old bucket empties, new fills.
	r.Register(makeTenantAgent("bravo", "m2", ""))
	if got := r.GetByModelForTenant("m", ""); got != nil {
		t.Errorf("model m should be empty after bravo moved, got %v", got)
	}
	if got := r.GetByModelForTenant("m2", ""); got == nil || got.Metadata.Name != "bravo" {
		t.Errorf("model m2: got %v, want bravo", got)
	}
}

// TestAgentRegistry_GetByModelForTenant_NoAllocs is the PERF-01 guard: the hot
// path is an allocation-free index read.
func TestAgentRegistry_GetByModelForTenant_NoAllocs(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("a", "m", ""))
	r.Register(makeTenantAgent("b", "m", "ten_x"))
	allocs := testing.AllocsPerRun(100, func() {
		_ = r.GetByModelForTenant("m", "ten_x")
		_ = r.GetByModelForTenant("m", "")
	})
	if allocs != 0 {
		t.Errorf("GetByModelForTenant allocated %.1f/op, want 0 (PERF-01)", allocs)
	}
}

// BenchmarkGetByModelForTenant_ManyAgents proves the lookup stays flat (O(1))
// as the registry grows — the whole point of PERF-01.
func BenchmarkGetByModelForTenant_ManyAgents(b *testing.B) {
	r := NewAgentRegistry()
	for i := 0; i < 1000; i++ {
		owner := ""
		if i%2 == 0 {
			owner = "ten_x"
		}
		r.Register(makeTenantAgent(fmt.Sprintf("agent-%04d", i), "shared", owner))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.GetByModelForTenant("shared", "ten_x")
	}
}

func TestAgentRegistry_ListForTenant_FiltersScoped(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("zglobal", "g", ""))
	r.Register(makeTenantAgent("acme-x", "gpt-4o", "ten_acme"))
	r.Register(makeTenantAgent("acme-y", "claude", "ten_acme"))
	r.Register(makeTenantAgent("beta-z", "claude", "ten_beta"))

	acme := r.ListForTenant("ten_acme")
	names := make([]string, len(acme))
	for i, a := range acme {
		names[i] = a.Metadata.Name
	}
	want := []string{"acme-x", "acme-y", "zglobal"}
	if !equalStringSlices(names, want) {
		t.Errorf("acme view = %v, want %v", names, want)
	}

	anon := r.ListForTenant("")
	if len(anon) != 1 || anon[0].Metadata.Name != "zglobal" {
		t.Errorf("anonymous view = %v, want [zglobal]", anon)
	}

	beta := r.ListForTenant("ten_beta")
	betaNames := make([]string, len(beta))
	for i, a := range beta {
		betaNames[i] = a.Metadata.Name
	}
	if !equalStringSlices(betaNames, []string{"beta-z", "zglobal"}) {
		t.Errorf("beta view = %v", betaNames)
	}
}

// --- Engine integration ---

func newTestEngineForTenants(t *testing.T) *Engine {
	t.Helper()
	r := NewAgentRegistry()
	// Two global agents so the "single-agent default" fallback
	// cannot fire and mask a missed lookup. The tests that follow
	// rely on resolution failing cleanly for cross-tenant attempts.
	r.Register(makeTenantAgent("global-bot", "gpt-4o", ""))
	r.Register(makeTenantAgent("global-other", "gpt-4o-mini", ""))
	r.Register(makeTenantAgent("acme-bot", "claude-x", "ten_acme"))
	r.Register(makeTenantAgent("beta-bot", "claude-y", "ten_beta"))

	return NewEngine(r, state.NewMemoryStore(0), slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func TestEngine_TenantScopedResolveByName(t *testing.T) {
	e := newTestEngineForTenants(t)

	ctx := WithTenantID(context.Background(), "ten_acme")
	resp, err := e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "acme-bot",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("acme tenant should resolve own agent: %v", err)
	}
	if resp.AgentName != "acme-bot" {
		t.Errorf("got %q", resp.AgentName)
	}
}

func TestEngine_TenantCannotResolveOtherTenantAgent(t *testing.T) {
	e := newTestEngineForTenants(t)

	ctx := WithTenantID(context.Background(), "ten_acme")
	_, err := e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "beta-bot", // belongs to ten_beta
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sess-2",
	})
	if err == nil {
		t.Fatal("expected ErrAgentNotFound for cross-tenant lookup")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v", err)
	}
}

func TestEngine_AnonymousCallerSeesOnlyGlobal(t *testing.T) {
	e := newTestEngineForTenants(t)

	ctx := context.Background()
	resp, err := e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "global-bot",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sess-3",
	})
	if err != nil {
		t.Fatalf("anonymous should resolve global agent: %v", err)
	}
	if resp.AgentName != "global-bot" {
		t.Errorf("got %q", resp.AgentName)
	}

	// And cannot reach a tenant-scoped one.
	_, err = e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "acme-bot",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sess-4",
	})
	if err == nil {
		t.Fatal("anonymous should not resolve tenant-scoped agent")
	}
}

// --- TenantIDFromContext / WithTenantID round-trip ---

func TestTenantIDContextRoundTrip(t *testing.T) {
	if got := TenantIDFromContext(context.Background()); got != "" {
		t.Errorf("default = %q, want empty", got)
	}
	ctx := WithTenantID(context.Background(), "ten_xyz")
	if got := TenantIDFromContext(ctx); got != "ten_xyz" {
		t.Errorf("round trip = %q", got)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- Session tenant isolation (review finding X-02) ---

// newTurnEchoEngine builds an engine with two global agents whose response
// echoes the turn number, so a test can observe whether callers that reuse
// the same session_id (across tenants or across agents) share state.
func newTurnEchoEngine(t *testing.T) *Engine {
	t.Helper()
	echoAgent := func(name, model string) *types.AgentDefinition {
		return &types.AgentDefinition{
			Metadata: types.Metadata{Name: name, TenantID: ""},
			Spec: types.AgentSpec{
				Model: model,
				Behavior: types.BehaviorConfig{
					Scenarios: []types.Scenario{{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "turn={{ .TurnNumber }}"},
					}},
				},
			},
		}
	}
	r := NewAgentRegistry()
	r.Register(echoAgent("echo", "echo-model"))
	r.Register(echoAgent("echo2", "echo2-model"))
	return NewEngine(r, state.NewMemoryStore(0), slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func turnForAgent(t *testing.T, e *Engine, agentName, tenantID, sessionID string) string {
	t.Helper()
	ctx := context.Background()
	if tenantID != "" {
		ctx = WithTenantID(ctx, tenantID)
	}
	resp, err := e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: agentName,
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("process (agent=%q tenant=%q session=%q): %v", agentName, tenantID, sessionID, err)
	}
	return resp.Content
}

func turnFor(t *testing.T, e *Engine, tenantID, sessionID string) string {
	t.Helper()
	return turnForAgent(t, e, "echo", tenantID, sessionID)
}

func TestEngine_SessionsIsolatedAcrossTenants(t *testing.T) {
	e := newTurnEchoEngine(t)

	// The same client session_id "shared" is used by two tenants and an
	// anonymous caller; each must keep an independent conversation.
	if got := turnFor(t, e, "ten_a", "shared"); got != "turn=1" {
		t.Fatalf("ten_a first turn = %q, want turn=1", got)
	}
	if got := turnFor(t, e, "ten_a", "shared"); got != "turn=2" {
		t.Fatalf("ten_a second turn = %q, want turn=2", got)
	}
	// Before X-02 this returned turn=3 (state leaked from ten_a).
	if got := turnFor(t, e, "ten_b", "shared"); got != "turn=1" {
		t.Fatalf("cross-tenant leak: ten_b turn = %q, want turn=1", got)
	}
	if got := turnFor(t, e, "", "shared"); got != "turn=1" {
		t.Fatalf("anonymous leak: turn = %q, want turn=1", got)
	}
	if got := turnFor(t, e, "", "shared"); got != "turn=2" {
		t.Fatalf("anonymous second turn = %q, want turn=2", got)
	}
	// ten_b advances on its own namespace, unaffected by the others.
	if got := turnFor(t, e, "ten_b", "shared"); got != "turn=2" {
		t.Fatalf("ten_b second turn = %q, want turn=2", got)
	}
}

func TestEngine_SessionsIsolatedAcrossAgents(t *testing.T) {
	e := newTurnEchoEngine(t)

	// Same tenant (anonymous) and same client session_id, two different
	// agents. Each agent must keep its own conversation (review finding
	// X-03 — GetOrCreate previously ignored the agent for an existing id).
	if got := turnForAgent(t, e, "echo", "", "shared"); got != "turn=1" {
		t.Fatalf("echo first turn = %q, want turn=1", got)
	}
	if got := turnForAgent(t, e, "echo", "", "shared"); got != "turn=2" {
		t.Fatalf("echo second turn = %q, want turn=2", got)
	}
	// Before X-03 this returned turn=3 (echo2 inherited echo's session).
	if got := turnForAgent(t, e, "echo2", "", "shared"); got != "turn=1" {
		t.Fatalf("cross-agent leak: echo2 turn = %q, want turn=1", got)
	}
	if got := turnForAgent(t, e, "echo2", "", "shared"); got != "turn=2" {
		t.Fatalf("echo2 second turn = %q, want turn=2", got)
	}
	// echo is unaffected by echo2's activity.
	if got := turnForAgent(t, e, "echo", "", "shared"); got != "turn=3" {
		t.Fatalf("echo third turn = %q, want turn=3", got)
	}
}

func TestEngine_SingleTenantSessionBehaviorUnchanged(t *testing.T) {
	e := newTurnEchoEngine(t)
	// No tenant context: consecutive requests with the same id accumulate,
	// and a different id is independent — exactly as before tenancy.
	if got := turnFor(t, e, "", "s1"); got != "turn=1" {
		t.Fatalf("first = %q", got)
	}
	if got := turnFor(t, e, "", "s1"); got != "turn=2" {
		t.Fatalf("second = %q", got)
	}
	if got := turnFor(t, e, "", "s2"); got != "turn=1" {
		t.Fatalf("other id should be independent = %q", got)
	}
}

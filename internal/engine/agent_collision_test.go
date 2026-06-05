package engine

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

// REF-08 slice A: two tenants may each own an agent with the same name. These
// tests pin the new per-(tenant, name) keying — independent resolution, global
// shadowing, and tenant-precise removal.

func TestAgentRegistry_PerTenantNameCollision(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("bot", "model-a", "ten_a"))
	r.Register(makeTenantAgent("bot", "model-b", "ten_b"))

	if got := r.GetForTenant("bot", "ten_a"); got == nil || got.Spec.Model != "model-a" {
		t.Errorf("ten_a bot = %+v, want model-a", got)
	}
	if got := r.GetForTenant("bot", "ten_b"); got == nil || got.Spec.Model != "model-b" {
		t.Errorf("ten_b bot = %+v, want model-b", got)
	}
	// Neither leaks to a third tenant or an anonymous caller.
	if r.GetForTenant("bot", "") != nil {
		t.Error("anonymous caller must not see a tenant-owned bot")
	}
	if r.GetForTenant("bot", "ten_c") != nil {
		t.Error("a third tenant must not see either bot")
	}
	// Both agents are counted; the shared name is listed once.
	if r.Count() != 2 {
		t.Errorf("Count = %d, want 2", r.Count())
	}
	if names := r.ListNames(); len(names) != 1 || names[0] != "bot" {
		t.Errorf("ListNames = %v, want [bot]", names)
	}
	// Model resolution is independent per tenant.
	if got := r.GetByModelForTenant("model-a", "ten_a"); got == nil || got.Metadata.TenantID != "ten_a" {
		t.Errorf("model-a/ten_a = %+v", got)
	}
	if got := r.GetByModelForTenant("model-b", "ten_b"); got == nil || got.Metadata.TenantID != "ten_b" {
		t.Errorf("model-b/ten_b = %+v", got)
	}
}

func TestAgentRegistry_TenantShadowsGlobal(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("bot", "global-model", ""))
	r.Register(makeTenantAgent("bot", "tenant-model", "ten_a"))

	// ten_a sees its own; ten_b and anonymous fall back to the global.
	if got := r.GetForTenant("bot", "ten_a"); got == nil || got.Spec.Model != "tenant-model" {
		t.Errorf("ten_a = %+v, want tenant-model", got)
	}
	if got := r.GetForTenant("bot", "ten_b"); got == nil || got.Spec.Model != "global-model" {
		t.Errorf("ten_b = %+v, want global fallback", got)
	}
	if got := r.GetForTenant("bot", ""); got == nil || got.Spec.Model != "global-model" {
		t.Errorf("anonymous = %+v, want global", got)
	}

	// Each tenant's list shows exactly one "bot" — its own view of it.
	la := r.ListForTenant("ten_a")
	if len(la) != 1 || la[0].Spec.Model != "tenant-model" {
		t.Errorf("ten_a list = %+v, want one tenant-model bot", la)
	}
	lb := r.ListForTenant("ten_b")
	if len(lb) != 1 || lb[0].Spec.Model != "global-model" {
		t.Errorf("ten_b list = %+v, want one global bot", lb)
	}
}

func TestAgentRegistry_RemoveForTenant_Precision(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("bot", "model-a", "ten_a"))
	r.Register(makeTenantAgent("bot", "model-b", "ten_b"))

	if err := r.RemoveForTenant("bot", "ten_a"); err != nil {
		t.Fatal(err)
	}
	if r.GetForTenant("bot", "ten_a") != nil {
		t.Error("ten_a bot should be gone")
	}
	if got := r.GetForTenant("bot", "ten_b"); got == nil || got.Spec.Model != "model-b" {
		t.Error("ten_b bot must survive the precise removal")
	}
	if r.Count() != 1 {
		t.Errorf("Count = %d, want 1", r.Count())
	}
	if err := r.RemoveForTenant("bot", "ten_a"); err == nil {
		t.Error("expected an error removing an already-absent (tenant, name) pair")
	}
}

func TestAgentRegistry_Remove_SweepsAllTenants(t *testing.T) {
	// Legacy Remove(name) drops the name from every bucket — the file-watcher
	// behavior preserved from the global-keyed registry.
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("bot", "model-a", "ten_a"))
	r.Register(makeTenantAgent("bot", "model-b", "ten_b"))
	if err := r.Remove("bot"); err != nil {
		t.Fatal(err)
	}
	if r.Count() != 0 {
		t.Errorf("Count = %d, want 0 after the name sweep", r.Count())
	}
	if err := r.Remove("bot"); err == nil {
		t.Error("expected an error removing an absent name")
	}
}

// TestEngine_SameNameDifferentTenants proves end-to-end that two tenants owning
// an identically-named agent resolve to their OWN definition through the engine.
func TestEngine_SameNameDifferentTenants(t *testing.T) {
	mk := func(tenant, content string) *types.AgentDefinition {
		return &types.AgentDefinition{
			Metadata: types.Metadata{Name: "assistant", TenantID: tenant},
			Spec: types.AgentSpec{
				Model: "m-" + tenant,
				Behavior: types.BehaviorConfig{
					Scenarios: []types.Scenario{{
						Name:     "default",
						Response: types.ScenarioResponse{Content: content},
					}},
				},
			},
		}
	}
	r := NewAgentRegistry()
	r.Register(mk("ten_a", "A-response"))
	r.Register(mk("ten_b", "B-response"))
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	e := NewEngine(r, state.NewMemoryStore(0), logger)

	respA, err := e.ProcessRequestContext(WithTenantID(context.Background(), "ten_a"), &InboundRequest{
		AgentName: "assistant",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sa",
	})
	if err != nil || respA.Content != "A-response" {
		t.Fatalf("ten_a assistant: content=%q err=%v, want A-response", respA.Content, err)
	}
	respB, err := e.ProcessRequestContext(WithTenantID(context.Background(), "ten_b"), &InboundRequest{
		AgentName: "assistant",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sb",
	})
	if err != nil || respB.Content != "B-response" {
		t.Fatalf("ten_b assistant: content=%q err=%v, want B-response", respB.Content, err)
	}
}

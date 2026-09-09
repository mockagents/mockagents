package engine

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// tenantAgentWithChaos builds two same-named agents owned by different
// tenants, which is exactly what the registry allows and what the shared
// counters could not tell apart.
func tenantAgentWithChaos(tenantID string, cfg *types.ChaosConfig) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "support", TenantID: tenantID},
		Spec: types.AgentSpec{
			Behavior: types.BehaviorConfig{Chaos: cfg},
		},
	}
}

// TestChaosFailFirstIsPerTenant is the audit M-02 guard for the FailFirst
// counter: two tenants owning an agent of the same name each get their own
// "fail the first N" allowance. Keyed on the bare name, tenant A's two
// requests consumed tenant B's budget and B never saw its injected failures.
func TestChaosFailFirstIsPerTenant(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	cfg := func() *types.ChaosConfig {
		return &types.ChaosConfig{
			Errors: &types.ChaosErrorConfig{FailFirst: 2, StatusCode: http.StatusServiceUnavailable},
		}
	}
	acme := tenantAgentWithChaos("acme", cfg())
	globex := tenantAgentWithChaos("globex", cfg())

	// Exhaust acme's allowance.
	for i := 0; i < 2; i++ {
		if err := inj.Before(context.Background(), acme); AsChaosError(err) == nil {
			t.Fatalf("acme request %d: expected an injected failure, got %v", i+1, err)
		}
	}
	if err := inj.Before(context.Background(), acme); err != nil {
		t.Errorf("acme should have recovered after 2 failures, got %v", err)
	}

	// globex must still get its own two failures.
	for i := 0; i < 2; i++ {
		if err := inj.Before(context.Background(), globex); AsChaosError(err) == nil {
			t.Fatalf("globex request %d: expected an injected failure, got %v — acme's traffic consumed its budget", i+1, err)
		}
	}
	if err := inj.Before(context.Background(), globex); err != nil {
		t.Errorf("globex should have recovered after 2 failures, got %v", err)
	}
}

// TestChaosRateLimitIsPerTenant: the rolling window is per tenant too, so one
// tenant's traffic cannot produce 429s for another.
func TestChaosRateLimitIsPerTenant(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	cfg := func() *types.ChaosConfig {
		return &types.ChaosConfig{
			RateLimit: &types.ChaosRateLimitConfig{Requests: 2, WindowMs: 60_000},
		}
	}
	acme := tenantAgentWithChaos("acme", cfg())
	globex := tenantAgentWithChaos("globex", cfg())

	for i := 0; i < 2; i++ {
		if err := inj.Before(context.Background(), acme); err != nil {
			t.Fatalf("acme request %d should be allowed, got %v", i+1, err)
		}
	}
	err := inj.Before(context.Background(), acme)
	if ce := AsChaosError(err); ce == nil || ce.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("acme's third request should be rate limited, got %v", err)
	}

	// globex starts with a full budget.
	for i := 0; i < 2; i++ {
		if err := inj.Before(context.Background(), globex); err != nil {
			t.Fatalf("globex request %d should be allowed, got %v — it inherited acme's window", i+1, err)
		}
	}
}

// TestChaosSingleTenantUnchanged: with no tenant set (the single-tenant
// default) the scoping is invisible — one namespace, same behavior as before.
func TestChaosSingleTenantUnchanged(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{FailFirst: 1, StatusCode: http.StatusInternalServerError},
	})
	if err := inj.Before(context.Background(), agent); AsChaosError(err) == nil {
		t.Fatalf("first request should fail, got %v", err)
	}
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Errorf("second request should recover, got %v", err)
	}
}

// TestChaosResetAgentClearsCounters: a redefined agent starts clean, so
// "fail the first N" means the first N again after an edit or reload.
func TestChaosResetAgentClearsCounters(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := tenantAgentWithChaos("acme", &types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{FailFirst: 1, StatusCode: http.StatusInternalServerError},
	})

	if err := inj.Before(context.Background(), agent); AsChaosError(err) == nil {
		t.Fatalf("first request should fail, got %v", err)
	}
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Fatalf("second request should recover, got %v", err)
	}

	inj.ResetAgent("acme", "support")

	if err := inj.Before(context.Background(), agent); AsChaosError(err) == nil {
		t.Error("after a reset the fail-first allowance should start over, but the request succeeded")
	}
	// A reset of a different tenant's agent must not touch this one.
	inj.ResetAgent("globex", "support")
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Errorf("resetting another tenant's agent cleared this one's counter: %v", err)
	}
}

// TestRegistryResetsChaosOnChange wires the whole path: registering an agent
// through the registry the engine owns clears its chaos counters.
func TestRegistryResetsChaosOnChange(t *testing.T) {
	registry := NewAgentRegistry()
	eng := NewEngine(registry, nil, nil)
	agent := tenantAgentWithChaos("acme", &types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{FailFirst: 1, StatusCode: http.StatusInternalServerError},
	})
	eng.Chaos.Now = time.Now

	registry.Register(agent)
	if err := eng.Chaos.Before(context.Background(), agent); AsChaosError(err) == nil {
		t.Fatalf("first request should fail, got %v", err)
	}
	if err := eng.Chaos.Before(context.Background(), agent); err != nil {
		t.Fatalf("second request should recover, got %v", err)
	}

	// A reload / write-API replace re-registers the definition.
	registry.Register(agent)
	if err := eng.Chaos.Before(context.Background(), agent); AsChaosError(err) == nil {
		t.Error("re-registering the agent should reset its fail-first counter, but the request succeeded")
	}

	// Removing it clears the counters too.
	eng.Chaos.ResetAgent("acme", "support")
	if err := registry.RemoveForTenant("support", "acme"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := eng.Chaos.Before(context.Background(), agent); AsChaosError(err) == nil {
		t.Error("after removal the counter should be clean, but the request succeeded")
	}
}

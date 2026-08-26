package chaos

import "testing"

func TestDecidePrecedence(t *testing.T) {
	zero := 0.0
	policy := Policy{Seed: 42, Rate: &zero}
	if got := Decide(policy, "req-1", "status", "status"); !got.Apply || got.Source != "request-force" {
		t.Fatalf("force matching action = %+v", got)
	}
	if got := Decide(policy, "req-1", "status", "off"); got.Apply || got.Source != "request-off" {
		t.Fatalf("request off = %+v", got)
	}
	if got := Decide(policy, "req-1", "status", "disconnect"); got.Apply {
		t.Fatalf("different forced action selected status: %+v", got)
	}
}

func TestDecideFixedSeedIsStableAndActionScoped(t *testing.T) {
	rate := 0.5
	p := Policy{Seed: 8675309, Rate: &rate}
	first := Decide(p, "request-123", "malformed", "")
	for i := 0; i < 20; i++ {
		if got := Decide(p, "request-123", "malformed", ""); got != first {
			t.Fatalf("decision changed: first=%+v got=%+v", first, got)
		}
	}

	seenApply, seenSkip := false, false
	for i := 0; i < 100; i++ {
		got := Decide(p, string(rune(i)), "malformed", "")
		seenApply = seenApply || got.Apply
		seenSkip = seenSkip || !got.Apply
	}
	if !seenApply || !seenSkip {
		t.Fatalf("seeded sample did not exercise both outcomes")
	}
}

func TestDecideNilRatePreservesAlwaysOn(t *testing.T) {
	if got := Decide(Policy{}, "any", "status", ""); !got.Apply || got.Source != "configured" {
		t.Fatalf("legacy decision = %+v", got)
	}
}

func TestForOperationOverridesServiceRate(t *testing.T) {
	one, zero := 1.0, 0.0
	base := Policy{Seed: 7, Rate: &one}
	policy, operation := ForOperation(base, "tools/call", map[string]float64{"tools/call": zero})
	if got := Decide(policy, "req-1\x00"+operation, "error", ""); got.Apply || got.Source != "operation-rate" {
		t.Fatalf("operation override = %+v", got)
	}
	policy, _ = ForOperation(base, "tools/list", map[string]float64{"tools/call": zero})
	if got := Decide(policy, "req-1", "error", ""); !got.Apply || got.Source != "seeded-rate" {
		t.Fatalf("service fallback = %+v", got)
	}
}

func TestForSequenceOverridesOperationRate(t *testing.T) {
	zero, one := 0.0, 1.0
	policy, _ := ForOperation(Policy{Rate: &one}, "tools/call", map[string]float64{"tools/call": one})
	policy = ForSequence(policy, 2, map[uint64]float64{2: zero})
	if got := Decide(policy, "req-2", "error", ""); got.Apply || got.Source != "sequence-rate" {
		t.Fatalf("sequence override = %+v", got)
	}
	if got := Decide(policy, "req-2", "error", "error"); !got.Apply || got.Source != "request-force" {
		t.Fatalf("request force = %+v", got)
	}
}

func TestForFixturePrecedence(t *testing.T) {
	one, zero := 1.0, 0.0
	policy, _ := ForOperation(Policy{Rate: &one}, "tools/call", map[string]float64{"tools/call": one})
	policy = ForFixture(policy, "weather", map[string]float64{"weather": zero})
	if got := Decide(policy, "req", "error", ""); got.Apply || got.Source != "fixture-rate" {
		t.Fatalf("fixture override = %+v", got)
	}
	policy = ForSequence(policy, 2, map[uint64]float64{2: one})
	if got := Decide(policy, "req", "error", ""); !got.Apply || got.Source != "sequence-rate" {
		t.Fatalf("sequence override = %+v", got)
	}
}

func TestInheritGlobalIsLowestPrecedence(t *testing.T) {
	globalOne, serviceZero := 1.0, 0.0
	inherited := InheritGlobal(Policy{}, 42, &globalOne)
	if inherited.Rate == nil || *inherited.Rate != 1 || inherited.Seed != 42 || inherited.Source != "global-rate" {
		t.Fatalf("inherited policy = %+v", inherited)
	}
	service := InheritGlobal(Policy{Seed: 7, Rate: &serviceZero}, 42, &globalOne)
	if service.Rate == nil || *service.Rate != 0 || service.Seed != 7 || service.Source != "" {
		t.Fatalf("service policy changed = %+v", service)
	}
	operation, _ := ForOperation(inherited, "/healthy", map[string]float64{"/healthy": 0})
	if got := Decide(operation, "req", "status", ""); got.Apply || got.Source != "operation-rate" {
		t.Fatalf("operation did not override global = %+v", got)
	}
	if got := Decide(operation, "req", "status", "status"); !got.Apply || got.Source != "request-force" {
		t.Fatalf("request did not override operation = %+v", got)
	}
}

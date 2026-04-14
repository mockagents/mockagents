package contract

import (
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func sampleAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion,
		Kind:       types.AgentKind,
		Metadata:   types.Metadata{Name: "support"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Model:    "gpt-4o",
			Tools: []types.ToolDefinition{{
				Name:        "lookup_order",
				Description: "Look up an order by id.",
				Parameters: types.JSONSchemaObject{
					"type":     "object",
					"required": []interface{}{"order_id"},
					"properties": map[string]interface{}{
						"order_id": map[string]interface{}{"type": "string"},
					},
				},
			}},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{Name: "happy-path", Response: types.ScenarioResponse{Content: "ok"}},
					{Name: "fallback", Response: types.ScenarioResponse{Content: "fallback"}},
				},
				Streaming: &types.StreamingConfig{Enabled: true},
			},
		},
	}
}

func TestExtractIsDeterministic(t *testing.T) {
	c := Extract(sampleAgent())
	if c.Protocol != "openai-chat-completions" || c.Model != "gpt-4o" {
		t.Errorf("unexpected protocol/model: %+v", c)
	}
	if len(c.Tools) != 1 || c.Tools[0].Name != "lookup_order" {
		t.Errorf("tool extraction: %+v", c.Tools)
	}
	// Scenarios are sorted lexically regardless of declaration order.
	if len(c.Scenarios) != 2 || c.Scenarios[0] != "fallback" || c.Scenarios[1] != "happy-path" {
		t.Errorf("scenario sort failed: %+v", c.Scenarios)
	}
	if c.Streaming == nil || !c.Streaming.Enabled {
		t.Errorf("streaming not preserved: %+v", c.Streaming)
	}
}

func TestDiffIdentical(t *testing.T) {
	c := Extract(sampleAgent())
	if changes := Diff(c, c); len(changes) != 0 {
		t.Errorf("identical contracts should diff empty, got %+v", changes)
	}
}

func TestDiffBreakingToolRemoved(t *testing.T) {
	oldC := Extract(sampleAgent())
	newDef := sampleAgent()
	newDef.Spec.Tools = nil
	newC := Extract(newDef)

	changes := Diff(oldC, newC)
	if !HasBreaking(changes) {
		t.Fatalf("expected breaking change, got %+v", changes)
	}
	var found bool
	for _, c := range changes {
		if strings.Contains(c.Message, "removed") && strings.Contains(c.Path, "lookup_order") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tool-removed change, got %+v", changes)
	}
}

func TestDiffAdditiveToolAdded(t *testing.T) {
	oldC := Extract(sampleAgent())
	newDef := sampleAgent()
	newDef.Spec.Tools = append(newDef.Spec.Tools, types.ToolDefinition{
		Name:       "cancel_order",
		Parameters: types.JSONSchemaObject{"type": "object"},
	})
	newC := Extract(newDef)

	changes := Diff(oldC, newC)
	if HasBreaking(changes) {
		t.Errorf("adding a tool should not be breaking, got %+v", changes)
	}
	var found bool
	for _, c := range changes {
		if c.Severity == SeverityAdditive && strings.Contains(c.Message, "added") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected additive tool-added change, got %+v", changes)
	}
}

func TestDiffBreakingNewRequiredParam(t *testing.T) {
	oldC := Extract(sampleAgent())
	newDef := sampleAgent()
	newDef.Spec.Tools[0].Parameters = types.JSONSchemaObject{
		"type":     "object",
		"required": []interface{}{"order_id", "customer_id"},
		"properties": map[string]interface{}{
			"order_id":    map[string]interface{}{"type": "string"},
			"customer_id": map[string]interface{}{"type": "string"},
		},
	}
	newC := Extract(newDef)

	changes := Diff(oldC, newC)
	if !HasBreaking(changes) {
		t.Fatalf("new required param should be breaking, got %+v", changes)
	}
}

func TestDiffBreakingPropertyTypeChange(t *testing.T) {
	oldC := Extract(sampleAgent())
	newDef := sampleAgent()
	newDef.Spec.Tools[0].Parameters = types.JSONSchemaObject{
		"type":     "object",
		"required": []interface{}{"order_id"},
		"properties": map[string]interface{}{
			"order_id": map[string]interface{}{"type": "integer"},
		},
	}
	newC := Extract(newDef)

	if !HasBreaking(Diff(oldC, newC)) {
		t.Errorf("changing a property type should be breaking")
	}
}

func TestDiffBreakingProtocolChange(t *testing.T) {
	oldC := Extract(sampleAgent())
	newDef := sampleAgent()
	newDef.Spec.Protocol = "anthropic-messages"
	newC := Extract(newDef)

	if !HasBreaking(Diff(oldC, newC)) {
		t.Errorf("protocol change should be breaking")
	}
}

func TestDiffBreakingScenarioRemoved(t *testing.T) {
	oldC := Extract(sampleAgent())
	newDef := sampleAgent()
	newDef.Spec.Behavior.Scenarios = newDef.Spec.Behavior.Scenarios[:1]
	newC := Extract(newDef)

	if !HasBreaking(Diff(oldC, newC)) {
		t.Errorf("removing a scenario should be breaking")
	}
}

func TestDiffInfoModelChange(t *testing.T) {
	oldC := Extract(sampleAgent())
	newDef := sampleAgent()
	newDef.Spec.Model = "gpt-4o-mini"
	newC := Extract(newDef)

	changes := Diff(oldC, newC)
	if HasBreaking(changes) {
		t.Errorf("model change should not be breaking")
	}
	var found bool
	for _, c := range changes {
		if c.Path == "model" && c.Severity == SeverityInfo {
			found = true
		}
	}
	if !found {
		t.Errorf("expected info-severity model change, got %+v", changes)
	}
}

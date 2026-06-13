package config

import (
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func agentWithConnection(cc *types.ChaosConnectionConfig) *types.AgentDefinition {
	return &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "conn"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Chaos:     &types.ChaosConfig{Connection: cc},
				Scenarios: []types.Scenario{{Name: "default", Response: types.ScenarioResponse{Content: "ok"}}},
			},
		},
	}
}

// validateConn returns (hasErrors, joinedErrorString); Validate returns a nil
// list when there are no errors, so both are nil-safe here.
func validateConn(t *testing.T, cc *types.ChaosConnectionConfig) (bool, string) {
	t.Helper()
	errs := (&Validator{}).Validate(agentWithConnection(cc), "test.yaml", nil)
	if errs == nil {
		return false, ""
	}
	return errs.HasErrors(), errs.Error()
}

func TestValidateConnection_Valid(t *testing.T) {
	for _, mode := range []string{"reset", "empty", "random", "peer-reset", "random-then-close", "garbage"} {
		if bad, msg := validateConn(t, &types.ChaosConnectionConfig{Mode: mode, Rate: 1}); bad {
			t.Errorf("mode %q should be valid, got %v", mode, msg)
		}
	}
}

func TestValidateConnection_UnknownMode(t *testing.T) {
	bad, msg := validateConn(t, &types.ChaosConnectionConfig{Mode: "explode", Rate: 1})
	if !bad || !strings.Contains(msg, "connection.mode") {
		t.Errorf("unknown mode should error on connection.mode, got %v", msg)
	}
}

func TestValidateConnection_NoTrigger(t *testing.T) {
	bad, msg := validateConn(t, &types.ChaosConnectionConfig{Mode: "reset"}) // no rate, no fail_first
	if !bad || !strings.Contains(msg, "no trigger") {
		t.Errorf("missing trigger should error, got %v", msg)
	}
}

func TestValidateConnection_RateOutOfRange(t *testing.T) {
	bad, msg := validateConn(t, &types.ChaosConnectionConfig{Mode: "empty", Rate: 1.5})
	if !bad || !strings.Contains(msg, "connection.rate") {
		t.Errorf("rate>1 should error, got %v", msg)
	}
}

func TestValidateConnection_FailFirstTrigger(t *testing.T) {
	if bad, msg := validateConn(t, &types.ChaosConnectionConfig{Mode: "reset", FailFirst: 3}); bad {
		t.Errorf("fail_first alone is a valid trigger, got %v", msg)
	}
}

func TestConnectionResetPresetExpands(t *testing.T) {
	def := presetAgent("connection-reset", nil)
	ApplyDefaults(def)
	c := def.Spec.Behavior.Chaos
	if c.Connection == nil || c.Connection.Mode != "reset" || c.Connection.Rate != 1 {
		t.Fatalf("connection-reset preset did not expand: %+v", c.Connection)
	}
	if !c.Enabled {
		t.Error("preset should mark chaos enabled")
	}
}

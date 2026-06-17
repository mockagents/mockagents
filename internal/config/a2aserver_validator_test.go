package config

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func validA2ADef() *types.A2AServerDefinition {
	return &types.A2AServerDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.A2AServerKind,
		Metadata: types.Metadata{Name: "weather-a2a"},
		Spec: types.A2AServerSpec{
			Card: types.A2AAgentCard{Name: "Weather Agent",
				Skills: []types.A2ASkill{{ID: "forecast", Name: "Forecast"}}},
			Responses: []types.A2AMessageResponse{
				{Match: "weather", Text: "sunny"},
				{Default: true, Text: "?"},
			},
		},
	}
}

func TestValidateA2AServer_OK(t *testing.T) {
	if errs := ValidateA2AServer(validA2ADef(), "", nil); errs != nil {
		t.Fatalf("valid A2AServer reported errors: %v", errs.Error())
	}
}

func TestValidateA2AServer_Errors(t *testing.T) {
	cases := map[string]func(*types.A2AServerDefinition){
		"missing card name": func(d *types.A2AServerDefinition) { d.Spec.Card.Name = "" },
		"skill missing id":  func(d *types.A2AServerDefinition) { d.Spec.Card.Skills[0].ID = "" },
		"match and default": func(d *types.A2AServerDefinition) { d.Spec.Responses[0].Default = true },
		"two defaults": func(d *types.A2AServerDefinition) {
			d.Spec.Responses[0] = types.A2AMessageResponse{Default: true, Text: "x"}
		},
		"neither match/def": func(d *types.A2AServerDefinition) { d.Spec.Responses[0] = types.A2AMessageResponse{Text: "x"} },
		"unknown state":     func(d *types.A2AServerDefinition) { d.Spec.Responses[0].State = "bogus" },
		"wrong kind":        func(d *types.A2AServerDefinition) { d.Kind = "Agent" },
		"bad metadata name": func(d *types.A2AServerDefinition) { d.Metadata.Name = "Bad Name" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := validA2ADef()
			mutate(d)
			if errs := ValidateA2AServer(d, "", nil); errs == nil {
				t.Errorf("expected validation error for %q", name)
			}
		})
	}
}

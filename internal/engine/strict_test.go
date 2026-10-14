package engine

import (
	"io"
	"log/slog"
	"testing"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

func boolPtr(b bool) *bool { return &b }

func TestResolveStrictTools(t *testing.T) {
	cases := []struct {
		name string
		cfg  *types.StrictToolsConfig
		env  StrictMode
		want StrictToolModes
	}{
		{"nil cfg, no env", nil, StrictOff,
			StrictToolModes{StrictOff, StrictOff, StrictOff}},
		{"nil cfg, env warn", nil, StrictWarn,
			StrictToolModes{StrictWarn, StrictWarn, StrictWarn}},
		{"block without level implies strict", &types.StrictToolsConfig{}, StrictOff,
			StrictToolModes{StrictEnforce, StrictEnforce, StrictEnforce}},
		{"level warn fills dimensions", &types.StrictToolsConfig{Level: "warn"}, StrictOff,
			StrictToolModes{StrictWarn, StrictWarn, StrictWarn}},
		{"boolean false excludes a dimension",
			&types.StrictToolsConfig{Level: "strict", IDs: boolPtr(false)}, StrictOff,
			StrictToolModes{StrictOff, StrictEnforce, StrictEnforce}},
		{"agent level overrides env",
			&types.StrictToolsConfig{Level: "off"}, StrictEnforce,
			StrictToolModes{StrictOff, StrictOff, StrictOff}},
	}
	for _, tc := range cases {
		if got := resolveStrictTools(tc.cfg, tc.env); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestParseStrictLevel(t *testing.T) {
	for in, want := range map[string]StrictMode{
		"off": StrictOff, "": StrictOff, "banana": StrictOff,
		"warn": StrictWarn, "WARN": StrictWarn,
		"strict": StrictEnforce, "1": StrictEnforce, "true": StrictEnforce,
	} {
		if got := ParseStrictLevel(in); got != want {
			t.Errorf("ParseStrictLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestStrictParallelCap_ToolLoopConverges: under an enforced parallel cap the
// client only ever sees the scenario's FIRST call, so the tool-loop
// convergence guard must compare the echo against that capped list.
// Comparing against the full scenario list ([A,B] vs echoed [A]) never
// matched, so the capped [A] was re-issued after every tool result — an
// infinite tool loop.
func TestStrictParallelCap_ToolLoopConverges(t *testing.T) {
	reg := NewAgentRegistry()
	reg.Register(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "parallel"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Tools: []types.ToolDefinition{
				{Name: "get_weather", Responses: []types.ToolResponseRule{{IsDefault: true, Response: map[string]any{"temp": 72}}}},
				{Name: "get_time", Responses: []types.ToolResponseRule{{IsDefault: true, Response: map[string]any{"time": "noon"}}}},
			},
			Behavior: types.BehaviorConfig{
				StrictTools: &types.StrictToolsConfig{Level: "strict"},
				Scenarios: []types.Scenario{{
					Name: "default",
					Response: types.ScenarioResponse{
						Content: "Checking.",
						ToolCalls: []types.ToolCallSpec{
							{Name: "get_weather", Arguments: map[string]any{"city": "NYC"}},
							{Name: "get_time", Arguments: map[string]any{"tz": "EST"}},
						},
					},
				}},
			},
		},
	})
	eng := NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	noParallel := ToolChoice{ParallelDisabled: true}

	// Turn 1: the cap delivers only the first scenario call.
	turn1, err := eng.ProcessRequest(&InboundRequest{
		AgentName:  "parallel",
		ToolChoice: noParallel,
		Messages:   []RequestMessage{{Role: "user", Content: "weather and time?"}},
	})
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if len(turn1.ToolCalls) != 1 || turn1.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("turn 1: want the capped [get_weather], got %+v", turn1.ToolCalls)
	}

	// Turn 2: the client echoes the call it saw and answers it. The guard
	// must consume the identical re-issue instead of returning it again.
	turn2, err := eng.ProcessRequest(&InboundRequest{
		AgentName:  "parallel",
		ToolChoice: noParallel,
		Messages: []RequestMessage{
			{Role: "user", Content: "weather and time?"},
			{Role: "assistant", ToolCalls: []EchoedToolCall{{
				ID: "call_1", Name: "get_weather", Arguments: map[string]any{"city": "NYC"},
			}}},
			{Role: "tool", Content: `{"temp":72}`, IsToolResult: true, ToolResultIDs: []string{"call_1"}},
		},
	})
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if len(turn2.ToolCalls) != 0 {
		t.Fatalf("tool loop did not converge: turn 2 re-issued %+v", turn2.ToolCalls)
	}
	if turn2.Content != "Checking." {
		t.Errorf("turn 2 content = %q, want the scenario text", turn2.Content)
	}
}

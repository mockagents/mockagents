package adapter

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
)

// Round-11 c3: each adapter's tool_choice spelling maps to the engine's
// provider-neutral contract (parsing only — enforcement is knob-gated).
func TestParseOpenAIToolChoice(t *testing.T) {
	f := false
	cases := []struct {
		name     string
		tc       any
		parallel *bool
		want     engine.ToolChoice
	}{
		{"none", "none", nil, engine.ToolChoice{None: true}},
		{"required", "required", nil, engine.ToolChoice{Required: true}},
		{"auto", "auto", nil, engine.ToolChoice{}},
		{"named", map[string]any{"type": "function", "function": map[string]any{"name": "get_time"}}, nil,
			engine.ToolChoice{Required: true, Name: "get_time"}},
		{"parallel false", nil, &f, engine.ToolChoice{ParallelDisabled: true}},
	}
	for _, c := range cases {
		if got := parseOpenAIToolChoice(c.tc, c.parallel); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestParseAnthropicToolChoice(t *testing.T) {
	cases := []struct {
		name string
		tc   map[string]any
		want engine.ToolChoice
	}{
		{"none", map[string]any{"type": "none"}, engine.ToolChoice{None: true}},
		{"any", map[string]any{"type": "any"}, engine.ToolChoice{Required: true}},
		{"tool", map[string]any{"type": "tool", "name": "get_weather"},
			engine.ToolChoice{Required: true, Name: "get_weather"}},
		{"auto + disable_parallel", map[string]any{"type": "auto", "disable_parallel_tool_use": true},
			engine.ToolChoice{ParallelDisabled: true}},
		{"absent", nil, engine.ToolChoice{}},
	}
	for _, c := range cases {
		if got := parseAnthropicToolChoice(c.tc); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestParseGeminiToolChoice(t *testing.T) {
	cfg := func(mode string, names ...string) *GeminiToolConfig {
		return &GeminiToolConfig{FunctionCallingConfig: &GeminiFunctionCallingConfig{
			Mode: mode, AllowedFunctionNames: names}}
	}
	cases := []struct {
		name string
		tc   *GeminiToolConfig
		want engine.ToolChoice
	}{
		{"NONE", cfg("NONE"), engine.ToolChoice{None: true}},
		{"ANY", cfg("ANY"), engine.ToolChoice{Required: true}},
		{"ANY single allowed forces it", cfg("ANY", "get_time"),
			engine.ToolChoice{Required: true, Name: "get_time", AllowedNames: []string{"get_time"}}},
		{"ANY multiple allowed", cfg("ANY", "a", "b"),
			engine.ToolChoice{Required: true, AllowedNames: []string{"a", "b"}}},
		{"AUTO", cfg("AUTO"), engine.ToolChoice{}},
		{"nil", nil, engine.ToolChoice{}},
	}
	for _, c := range cases {
		if got := parseGeminiToolChoice(c.tc); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestParseResponsesToolChoice(t *testing.T) {
	f := false
	cases := []struct {
		name     string
		raw      string
		parallel *bool
		want     engine.ToolChoice
	}{
		{"none", `"none"`, nil, engine.ToolChoice{None: true}},
		{"required", `"required"`, nil, engine.ToolChoice{Required: true}},
		{"flat named", `{"type":"function","name":"get_time"}`, nil,
			engine.ToolChoice{Required: true, Name: "get_time"}},
		{"nested named tolerated", `{"type":"function","function":{"name":"get_time"}}`, nil,
			engine.ToolChoice{Required: true, Name: "get_time"}},
		{"absent + parallel false", ``, &f, engine.ToolChoice{ParallelDisabled: true}},
	}
	for _, c := range cases {
		if got := parseResponsesToolChoice(json.RawMessage(c.raw), c.parallel); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

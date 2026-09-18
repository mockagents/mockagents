package config

import (
	"fmt"
	"testing"
)

func TestAgentBoundsThroughFileAndBytes(t *testing.T) {
	for _, tc := range []struct {
		rate  string
		turn  int
		valid bool
	}{
		{"0", 1, true}, {"1", 2, true}, {"0.5", 1, true},
		{"-0.01", 1, false}, {"1.01", 1, false}, {".nan", 1, false}, {".inf", 1, false},
		{"0", 0, false}, {"0", -1, false},
	} {
		t.Run(fmt.Sprintf("rate=%s/turn=%d", tc.rate, tc.turn), func(t *testing.T) {
			body := fmt.Sprintf("apiVersion: mockagents/v1\nkind: Agent\nmetadata: {name: bounds}\nspec:\n  protocol: openai-chat-completions\n  tools:\n    - name: lookup\n      error_rate: %s\n  behavior:\n    scenarios:\n      - name: first\n        match: {turn_number: %d}\n        response: {content: hi}\n", tc.rate, tc.turn)
			if got := loadAndValidate(t, body) == nil; got != tc.valid {
				t.Errorf("file valid=%v", got)
			}
			if got := len(ValidateBytes([]byte(body)).Errors) == 0; got != tc.valid {
				t.Errorf("bytes valid=%v", got)
			}
		})
	}
}

func TestSchemaParity_AgentBounds(t *testing.T) {
	doc := loadAgentSchema(t)
	if got := numOf(t, walk(t, doc, "$defs", "MatchRule", "properties", "turn_number"), "minimum"); got != 1 {
		t.Errorf("turn minimum=%v", got)
	}
	rate := walk(t, doc, "$defs", "ToolDefinition", "properties", "error_rate")
	if numOf(t, rate, "minimum") != 0 || numOf(t, rate, "maximum") != 1 {
		t.Error("tool rate bounds must be [0,1]")
	}
}

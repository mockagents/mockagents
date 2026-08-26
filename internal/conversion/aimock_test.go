package conversion

import (
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

func TestConvertAIMockPreservesMessagesTurnsAndTools(t *testing.T) {
	input := `{"fixtures":[
		{"match":{"userMessage":"hello"},"response":{"content":"Hi there"}},
		{"match":{"userMessage":"weather","turnIndex":1},"response":{"toolCalls":[{"name":"get_weather","arguments":{"city":"Austin"}}],"finishReason":"tool_calls"}}
	]}`
	data, result, err := ConvertAIMock(strings.NewReader(input), AIMockOptions{Name: "legacy-agent", Protocol: "openai-chat-completions", Model: "gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 2 || result.Skipped != 0 {
		t.Fatalf("result=%+v", result)
	}
	if report := config.ValidateBytes(data); len(report.Errors) != 0 {
		t.Fatalf("validation=%+v\n%s", report.Errors, data)
	}
	var agent types.AgentDefinition
	if err := yaml.Unmarshal(data, &agent); err != nil {
		t.Fatal(err)
	}
	if agent.Metadata.Name != "legacy-agent" || agent.Spec.Model != "gpt-4o" || len(agent.Spec.Behavior.Scenarios) != 2 {
		t.Fatalf("agent=%+v", agent)
	}
	toolScenario := agent.Spec.Behavior.Scenarios[1]
	if toolScenario.Match.ContentContains != "weather" || toolScenario.Match.TurnNumber == nil || *toolScenario.Match.TurnNumber != 1 {
		t.Fatalf("match=%+v", toolScenario.Match)
	}
	if got := toolScenario.Response.ToolCalls; len(got) != 1 || got[0].Name != "get_weather" || got[0].Arguments["city"] != "Austin" {
		t.Fatalf("tools=%+v", got)
	}
}

func TestConvertAIMockSkipsUnsupportedMatchers(t *testing.T) {
	input := `{"fixtures":[
		{"match":{"toolName":"get_weather"},"response":{"content":"unsafe catch-all"}},
		{"match":{"userMessage":"hello"},"response":{"content":"Hi"}}
	]}`
	_, result, err := ConvertAIMock(strings.NewReader(input), AIMockOptions{Name: "legacy-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 || result.Skipped != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "toolName") {
		t.Fatalf("result=%+v", result)
	}
}

func TestConvertAIMockStringifiesStructuredContent(t *testing.T) {
	input := `{"fixtures":[{"match":{"userMessage":"json"},"response":{"content":{"answer":42}}}]}`
	data, _, err := ConvertAIMock(strings.NewReader(input), AIMockOptions{Name: "legacy-agent"})
	if err != nil {
		t.Fatal(err)
	}
	var agent types.AgentDefinition
	if err := yaml.Unmarshal(data, &agent); err != nil {
		t.Fatal(err)
	}
	if got := agent.Spec.Behavior.Scenarios[0].Response.Content; got != `{"answer":42}` {
		t.Fatalf("content=%q", got)
	}
}

func TestConvertAIMockRejectsInvalidOrEmptyOutput(t *testing.T) {
	if _, _, err := ConvertAIMock(strings.NewReader(`{"fixtures":[]}`), AIMockOptions{Name: "agent"}); err == nil {
		t.Fatal("expected empty error")
	}
	if _, _, err := ConvertAIMock(strings.NewReader(`{"fixtures":[{"match":{"toolCallId":"call_1"},"response":{"content":"done"}}]}`), AIMockOptions{Name: "agent"}); err == nil || !strings.Contains(err.Error(), "no safely convertible") {
		t.Fatalf("err=%v", err)
	}
	if _, _, err := ConvertAIMock(strings.NewReader(`{"fixtures":[{"match":{"userMessage":"hi"},"response":{"content":"ok"}}]}`), AIMockOptions{Name: "INVALID NAME"}); err == nil || !strings.Contains(err.Error(), "converted agent is invalid") {
		t.Fatalf("err=%v", err)
	}
}

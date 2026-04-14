package engine

import (
	"log/slog"
	"os"
	"testing"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

func benchmarkAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "bench-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Tools: []types.ToolDefinition{
				{
					Name: "search",
					Responses: []types.ToolResponseRule{
						{Match: map[string]any{"q": "test"}, Response: map[string]any{"result": "found"}},
						{IsDefault: true, Response: map[string]any{"result": "default"}},
					},
				},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{Name: "greet", Match: &types.MatchRule{ContentContains: "hello"}, Response: types.ScenarioResponse{Content: "Hi!"}},
					{Name: "search", Match: &types.MatchRule{ContentContains: "search"}, Response: types.ScenarioResponse{
						Content:   "Searching...",
						ToolCalls: []types.ToolCallSpec{{Name: "search", Arguments: map[string]any{"q": "test"}}},
					}},
					{Name: "regex", Match: &types.MatchRule{ContentRegex: `order (?P<id>ORD-\d+)`}, Response: types.ScenarioResponse{Content: "Looking up order."}},
					{Name: "template", Match: &types.MatchRule{ContentContains: "dynamic"}, Response: types.ScenarioResponse{Content: "Turn {{ .TurnNumber }}, ID: {{ uuid }}"}},
					{Name: "default", Response: types.ScenarioResponse{Content: "Default response."}},
				},
			},
		},
	}
}

func benchEngine() *Engine {
	registry := NewAgentRegistry()
	registry.Register(benchmarkAgent())
	store := state.NewMemoryStore(0)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewEngine(registry, store, logger)
}

func BenchmarkProcessRequest_StaticResponse(b *testing.B) {
	eng := benchEngine()
	req := &InboundRequest{
		AgentName: "bench-agent",
		SessionID: "bench-static",
		Messages:  []RequestMessage{{Role: "user", Content: "hello there"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.SessionID = "bench-static-" + string(rune(i%26+'a'))
		eng.ProcessRequest(req)
	}
}

func BenchmarkProcessRequest_TemplateResponse(b *testing.B) {
	eng := benchEngine()
	req := &InboundRequest{
		AgentName: "bench-agent",
		SessionID: "bench-tmpl",
		Messages:  []RequestMessage{{Role: "user", Content: "give me dynamic data"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.SessionID = "bench-tmpl-" + string(rune(i%26+'a'))
		eng.ProcessRequest(req)
	}
}

func BenchmarkProcessRequest_WithToolCalls(b *testing.B) {
	eng := benchEngine()
	req := &InboundRequest{
		AgentName: "bench-agent",
		SessionID: "bench-tools",
		Messages:  []RequestMessage{{Role: "user", Content: "please search for it"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.SessionID = "bench-tools-" + string(rune(i%26+'a'))
		eng.ProcessRequest(req)
	}
}

func BenchmarkProcessRequest_RegexMatch(b *testing.B) {
	eng := benchEngine()
	req := &InboundRequest{
		AgentName: "bench-agent",
		SessionID: "bench-regex",
		Messages:  []RequestMessage{{Role: "user", Content: "check order ORD-12345"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.SessionID = "bench-regex-" + string(rune(i%26+'a'))
		eng.ProcessRequest(req)
	}
}

func BenchmarkProcessRequest_DefaultFallback(b *testing.B) {
	eng := benchEngine()
	req := &InboundRequest{
		AgentName: "bench-agent",
		SessionID: "bench-default",
		Messages:  []RequestMessage{{Role: "user", Content: "random unmatched message"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.SessionID = "bench-default-" + string(rune(i%26+'a'))
		eng.ProcessRequest(req)
	}
}

func BenchmarkScenarioMatcher_ContentContains(b *testing.B) {
	m := NewScenarioMatcher()
	scenarios := benchmarkAgent().Spec.Behavior.Scenarios
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match(scenarios, "hello world", 1)
	}
}

func BenchmarkScenarioMatcher_Regex(b *testing.B) {
	m := NewScenarioMatcher()
	scenarios := benchmarkAgent().Spec.Behavior.Scenarios
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match(scenarios, "check order ORD-99999", 1)
	}
}

func BenchmarkScenarioMatcher_Default(b *testing.B) {
	m := NewScenarioMatcher()
	scenarios := benchmarkAgent().Spec.Behavior.Scenarios
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match(scenarios, "no match at all", 1)
	}
}

func BenchmarkResponseGenerator_Static(b *testing.B) {
	g := NewResponseGenerator()
	agent := benchmarkAgent()
	scenario := &agent.Spec.Behavior.Scenarios[0]
	ctx := TemplateContext{Agent: agent, Message: "hello", TurnNumber: 1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Generate(agent, scenario, ctx)
	}
}

func BenchmarkResponseGenerator_Template(b *testing.B) {
	g := NewResponseGenerator()
	agent := benchmarkAgent()
	scenario := &agent.Spec.Behavior.Scenarios[3]
	ctx := TemplateContext{Agent: agent, Message: "dynamic", TurnNumber: 1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Generate(agent, scenario, ctx)
	}
}

func BenchmarkToolCallProcessor(b *testing.B) {
	p := NewToolCallProcessor()
	tools := benchmarkAgent().Spec.Tools
	calls := []types.ToolCallSpec{{Name: "search", Arguments: map[string]any{"q": "test"}}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.ProcessToolCalls(calls, tools)
	}
}

func BenchmarkAgentRegistry_Get(b *testing.B) {
	r := NewAgentRegistry()
	for i := 0; i < 100; i++ {
		r.Register(&types.AgentDefinition{
			Metadata: types.Metadata{Name: "agent-" + string(rune(i%26+'a')) + string(rune(i/26+'0'))},
			Spec:     types.AgentSpec{Model: "model-" + string(rune(i%26+'a'))},
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get("agent-m5")
	}
}

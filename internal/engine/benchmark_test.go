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

// multiToolAgent emits several tool calls from one scenario so the
// per-turn toolCallMsgs slice is appended to repeatedly. Exercises the
// F-EN-006 pre-size: a nil slice would reallocate (cap 1→2→4) on the way
// up, the pre-sized slice allocates its backing array once.
func multiToolAgent() *types.AgentDefinition {
	tools := make([]types.ToolDefinition, 4)
	calls := make([]types.ToolCallSpec, 4)
	for i, name := range []string{"alpha", "beta", "gamma", "delta"} {
		tools[i] = types.ToolDefinition{
			Name:      name,
			Responses: []types.ToolResponseRule{{IsDefault: true, Response: map[string]any{"ok": true}}},
		}
		calls[i] = types.ToolCallSpec{Name: name, Arguments: map[string]any{"i": i}}
	}
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "multi-tool-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Tools: tools,
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{Name: "fanout", Match: &types.MatchRule{ContentContains: "fan"}, Response: types.ScenarioResponse{
						Content:   "Fanning out.",
						ToolCalls: calls,
					}},
				},
			},
		},
	}
}

func BenchmarkProcessRequest_MultipleToolCalls(b *testing.B) {
	registry := NewAgentRegistry()
	registry.Register(multiToolAgent())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := NewEngine(registry, state.NewMemoryStore(0), logger)
	req := &InboundRequest{
		AgentName: "multi-tool-agent",
		SessionID: "bench-multitool",
		Messages:  []RequestMessage{{Role: "user", Content: "please fan out"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.SessionID = "bench-multitool-" + string(rune(i%26+'a'))
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

// BenchmarkScenarioMatcher_MixedCaseManyScenarios runs a mixed-case
// message past several non-matching content_contains rules before the
// hit. Exercises the F-SM-007 lower-once: the old code re-lowered the
// full message (a fresh allocation, since the case actually differs)
// once per scenario; the new code lowers it a single time per match.
func BenchmarkScenarioMatcher_MixedCaseManyScenarios(b *testing.B) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "s1", Match: &types.MatchRule{ContentContains: "invoice"}},
		{Name: "s2", Match: &types.MatchRule{ContentContains: "refund"}},
		{Name: "s3", Match: &types.MatchRule{ContentContains: "shipment"}},
		{Name: "s4", Match: &types.MatchRule{ContentContains: "warranty"}},
		{Name: "s5", Match: &types.MatchRule{ContentContains: "subscription"}},
		{Name: "s6", Match: &types.MatchRule{ContentContains: "catalog"}},
	}
	const msg = "Please Look For The SPECIAL Item In The CATALOG Today"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match(scenarios, msg, 1)
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

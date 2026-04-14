package mockagents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Protocol selects which endpoint a scenario targets.
type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
)

// ScenarioStep is one message in a scripted conversation.
type ScenarioStep struct {
	Role    string
	Content string
}

// Scenario is a declarative multi-turn test case.
type Scenario struct {
	Name      string
	Steps     []ScenarioStep
	Protocol  Protocol
	SessionID string
	Model     string
}

// NewScenario constructs a Scenario, defaulting to OpenAI protocol and
// a random session id.
func NewScenario(name string, steps []ScenarioStep) *Scenario {
	if name == "" {
		panic("mockagents: scenario name is required")
	}
	if len(steps) == 0 {
		panic("mockagents: scenario must have at least one step")
	}
	return &Scenario{
		Name:      name,
		Steps:     steps,
		Protocol:  ProtocolOpenAI,
		SessionID: randomSessionID(),
	}
}

// ScenarioResult aggregates every response produced by RunScenario.
type ScenarioResult struct {
	ScenarioName   string
	Responses      []*ChatResponse
	TotalLatencyMs float64
}

// Last returns the final response, or nil if the scenario produced none.
func (r *ScenarioResult) Last() *ChatResponse {
	if len(r.Responses) == 0 {
		return nil
	}
	return r.Responses[len(r.Responses)-1]
}

// LastContent is a convenience for pulling content off the final response.
func (r *ScenarioResult) LastContent() string {
	last := r.Last()
	if last == nil {
		return ""
	}
	return last.Content
}

// RunScenario walks the scenario steps against the given client. Each
// user step triggers a single request; assistant/system steps are
// recorded as prior context only. Assistant replies from the server are
// folded back into the conversation history so multi-turn scenarios see
// the same state the server does.
func RunScenario(ctx context.Context, client *Client, scenario *Scenario) (*ScenarioResult, error) {
	if client == nil {
		return nil, errors.New("mockagents: client is nil")
	}
	if scenario == nil {
		return nil, errors.New("mockagents: scenario is nil")
	}

	history := make([]ChatMessage, 0, len(scenario.Steps)*2)
	result := &ScenarioResult{ScenarioName: scenario.Name}
	start := time.Now()

	for _, step := range scenario.Steps {
		history = append(history, ChatMessage{Role: step.Role, Content: step.Content})
		if step.Role != "user" {
			continue
		}
		var (
			resp *ChatResponse
			err  error
		)
		switch scenario.Protocol {
		case ProtocolAnthropic:
			resp, err = client.Message(ctx, append([]ChatMessage(nil), history...), MessageOptions{
				Model:     scenario.Model,
				SessionID: scenario.SessionID,
			})
		default:
			resp, err = client.Chat(ctx, append([]ChatMessage(nil), history...), ChatOptions{
				Model:     scenario.Model,
				SessionID: scenario.SessionID,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("step %d (%q): %w", len(result.Responses)+1, step.Content, err)
		}
		result.Responses = append(result.Responses, resp)
		history = append(history, ChatMessage{Role: "assistant", Content: resp.Content})
	}

	result.TotalLatencyMs = float64(time.Since(start).Microseconds()) / 1000.0
	return result, nil
}

func randomSessionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "scenario-" + hex.EncodeToString(b[:])
}

package conversion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// AIMockOptions controls the MockAgents agent emitted from an AIMock fixture file.
type AIMockOptions struct {
	Name     string
	Protocol string
	Model    string
}

// AIMockResult reports lossy fixtures instead of silently changing their behavior.
type AIMockResult struct {
	Imported int
	Skipped  int
	Warnings []string
}

type aimockDocument struct {
	Fixtures []aimockFixture `json:"fixtures"`
}

type aimockFixture struct {
	Match    map[string]json.RawMessage `json:"match"`
	Response aimockResponse             `json:"response"`
}

type aimockResponse struct {
	Content      json.RawMessage  `json:"content"`
	ToolCalls    []aimockToolCall `json:"toolCalls"`
	FinishReason string           `json:"finishReason"`
	Refusal      string           `json:"refusal"`
}

type aimockToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ConvertAIMock converts the deterministic, provider-neutral subset of AIMock
// JSON fixtures into one MockAgents Agent YAML document. Unsupported matchers
// are skipped and reported because broadening them to catch-alls is unsafe.
func ConvertAIMock(r io.Reader, opts AIMockOptions) ([]byte, AIMockResult, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	var source aimockDocument
	if err := decoder.Decode(&source); err != nil {
		return nil, AIMockResult{}, fmt.Errorf("decode AIMock fixtures: %w", err)
	}
	if len(source.Fixtures) == 0 {
		return nil, AIMockResult{}, fmt.Errorf("AIMock fixture document has no fixtures")
	}
	result := AIMockResult{}
	scenarios := make([]types.Scenario, 0, len(source.Fixtures))
	for index, fixture := range source.Fixtures {
		scenario, reason, err := convertFixture(index, fixture)
		if err != nil {
			return nil, result, err
		}
		if reason != "" {
			result.Skipped++
			result.Warnings = append(result.Warnings, reason)
			continue
		}
		result.Imported++
		scenarios = append(scenarios, scenario)
	}
	if len(scenarios) == 0 {
		return nil, result, fmt.Errorf("no safely convertible AIMock fixtures (%d skipped)", result.Skipped)
	}
	toolNames := map[string]bool{}
	var tools []types.ToolDefinition
	for _, scenario := range scenarios {
		for _, call := range scenario.Response.ToolCalls {
			if !toolNames[call.Name] {
				toolNames[call.Name] = true
				tools = append(tools, types.ToolDefinition{Name: call.Name})
			}
		}
	}
	protocol := opts.Protocol
	if protocol == "" {
		protocol = "openai-chat-completions"
	}
	model := opts.Model
	if model == "" {
		model = types.DefaultModel
	}
	agent := types.AgentDefinition{
		APIVersion: types.AgentAPIVersion,
		Kind:       types.AgentKind,
		Metadata:   types.Metadata{Name: opts.Name, Description: "Converted from AIMock fixtures", Tags: []string{"aimock-migration"}},
		Spec:       types.AgentSpec{Protocol: protocol, Model: model, Tools: tools, Behavior: types.BehaviorConfig{Scenarios: scenarios}},
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(agent); err != nil {
		return nil, result, fmt.Errorf("encode MockAgents YAML: %w", err)
	}
	if report := config.ValidateBytes(out.Bytes()); len(report.Errors) > 0 {
		messages := make([]string, 0, len(report.Errors))
		for _, validationErr := range report.Errors {
			messages = append(messages, validationErr.Error())
		}
		return nil, result, fmt.Errorf("converted agent is invalid: %s", strings.Join(messages, "; "))
	}
	return out.Bytes(), result, nil
}

func convertFixture(index int, fixture aimockFixture) (types.Scenario, string, error) {
	match := &types.MatchRule{}
	if raw, ok := fixture.Match["userMessage"]; ok {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return types.Scenario{}, fmt.Sprintf("fixture %d: userMessage is not a string", index+1), nil
		}
		match.ContentContains = value
		delete(fixture.Match, "userMessage")
	}
	if raw, ok := fixture.Match["turnIndex"]; ok {
		var value int
		if err := json.Unmarshal(raw, &value); err != nil || value < 0 {
			return types.Scenario{}, fmt.Sprintf("fixture %d: turnIndex is not a non-negative integer", index+1), nil
		}
		match.TurnNumber = &value
		delete(fixture.Match, "turnIndex")
	}
	if len(fixture.Match) > 0 {
		keys := make([]string, 0, len(fixture.Match))
		for key := range fixture.Match {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return types.Scenario{}, fmt.Sprintf("fixture %d: unsupported AIMock match fields: %s", index+1, strings.Join(keys, ", ")), nil
	}
	response := types.ScenarioResponse{FinishReason: fixture.Response.FinishReason, Refusal: fixture.Response.Refusal}
	if len(fixture.Response.Content) > 0 && string(fixture.Response.Content) != "null" {
		if err := json.Unmarshal(fixture.Response.Content, &response.Content); err != nil {
			var structured any
			if decodeErr := json.Unmarshal(fixture.Response.Content, &structured); decodeErr != nil {
				return types.Scenario{}, "", fmt.Errorf("fixture %d: invalid response.content: %w", index+1, decodeErr)
			}
			encoded, encodeErr := json.Marshal(structured)
			if encodeErr != nil {
				return types.Scenario{}, "", fmt.Errorf("fixture %d: encode response.content: %w", index+1, encodeErr)
			}
			response.Content = string(encoded)
		}
	}
	for _, call := range fixture.Response.ToolCalls {
		if strings.TrimSpace(call.Name) == "" {
			return types.Scenario{}, "", fmt.Errorf("fixture %d: tool call name is required", index+1)
		}
		response.ToolCalls = append(response.ToolCalls, types.ToolCallSpec{Name: call.Name, Arguments: call.Arguments})
	}
	if response.Content == "" && response.Refusal == "" && len(response.ToolCalls) == 0 {
		return types.Scenario{}, fmt.Sprintf("fixture %d: response has no supported content, refusal, or toolCalls", index+1), nil
	}
	if match.ContentContains == "" && match.TurnNumber == nil {
		match = nil
	}
	return types.Scenario{Name: fmt.Sprintf("aimock-fixture-%d", index+1), Match: match, Response: response}, "", nil
}

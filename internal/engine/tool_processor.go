package engine

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

var (
	ErrToolNotFound        = errors.New("tool not found")
	ErrParameterValidation = errors.New("tool parameter validation failed")
)

// ToolCallResult holds the resolved result for a single tool call.
type ToolCallResult struct {
	ID       string           `json:"id"`
	ToolName string           `json:"tool_name"`
	Response any              `json:"response,omitempty"`
	Error    *types.ToolError `json:"error,omitempty"`
	IsError  bool             `json:"is_error"`
}

// ToolCallProcessor resolves tool calls against configured tool definitions.
type ToolCallProcessor struct {
	validator *ToolValidator
}

// NewToolCallProcessor creates a new ToolCallProcessor.
func NewToolCallProcessor() *ToolCallProcessor {
	return &ToolCallProcessor{
		validator: NewToolValidator(),
	}
}

// ProcessToolCalls resolves all tool calls in a response against the agent's
// tool definitions. Tool calls are processed concurrently.
func (p *ToolCallProcessor) ProcessToolCalls(
	toolCalls []types.ToolCallSpec,
	tools []types.ToolDefinition,
) ([]ToolCallResult, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	toolIndex := indexTools(tools)
	results := make([]ToolCallResult, len(toolCalls))
	errs := make([]error, len(toolCalls))

	var wg sync.WaitGroup
	for i, call := range toolCalls {
		wg.Add(1)
		go func(idx int, tc types.ToolCallSpec) {
			defer wg.Done()
			result, err := p.processOne(tc, toolIndex)
			results[idx] = result
			errs[idx] = err
		}(i, call)
	}
	wg.Wait()

	// Collect any fatal errors (tool not found).
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrParameterValidation) {
			return results, err
		}
	}
	return results, nil
}

// processOne resolves a single tool call.
func (p *ToolCallProcessor) processOne(
	call types.ToolCallSpec,
	toolIndex map[string]*types.ToolDefinition,
) (ToolCallResult, error) {
	result := ToolCallResult{
		ID:       generateToolCallID(),
		ToolName: call.Name,
	}

	tool, ok := toolIndex[call.Name]
	if !ok {
		result.IsError = true
		result.Error = &types.ToolError{
			Code:    "TOOL_NOT_FOUND",
			Message: fmt.Sprintf("tool %q is not defined in the agent", call.Name),
		}
		return result, fmt.Errorf("%w: %q", ErrToolNotFound, call.Name)
	}

	// Parameter validation (if enabled).
	if tool.Validate && len(tool.Parameters) > 0 {
		if valErrs := p.validator.ValidateParameters(tool.Parameters, call.Arguments); len(valErrs) > 0 {
			result.IsError = true
			result.Error = &types.ToolError{
				Code:    "INVALID_PARAMETERS",
				Message: formatValidationErrors(valErrs),
			}
			return result, ErrParameterValidation
		}
	}

	// Error injection (random failure rate).
	if tool.ErrorRate > 0 && shouldInjectError(tool.ErrorRate) {
		result.IsError = true
		result.Error = &types.ToolError{
			Code:    "INJECTED_ERROR",
			Message: fmt.Sprintf("chaos: random error injected at %.0f%% rate", tool.ErrorRate*100),
		}
		return result, nil
	}

	// Resolve tool response by matching arguments.
	resp, toolErr := resolveToolResponse(tool.Responses, call.Arguments)
	if toolErr != nil {
		result.IsError = true
		result.Error = toolErr
		return result, nil
	}
	if resp == nil {
		// No match and no default — return a global fallback.
		result.Response = map[string]string{"status": "ok"}
		return result, nil
	}

	result.Response = resp
	return result, nil
}

// resolveToolResponse finds the first matching response rule for the given arguments.
// Returns (response, nil) for success, (nil, error) for tool errors, or (nil, nil) for no match.
func resolveToolResponse(
	rules []types.ToolResponseRule,
	args map[string]any,
) (any, *types.ToolError) {
	var defaultRule *types.ToolResponseRule

	for i := range rules {
		rule := &rules[i]

		if rule.IsDefault {
			defaultRule = rule
			continue
		}

		if len(rule.Match) == 0 {
			// No match criteria (and not default — the default branch above
			// already continued) — nothing to match against, so skip.
			continue
		}

		if matchArgs(rule.Match, args) {
			if rule.Error != nil {
				return nil, rule.Error
			}
			return rule.Response, nil
		}
	}

	// Fallback to default.
	if defaultRule != nil {
		if defaultRule.Error != nil {
			return nil, defaultRule.Error
		}
		return defaultRule.Response, nil
	}
	return nil, nil
}

// matchArgs checks if all keys in the match criteria are present and equal
// in the arguments. Unspecified parameters in args are ignored.
func matchArgs(match, args map[string]any) bool {
	if len(match) == 0 {
		return false
	}
	for key, expected := range match {
		actual, ok := args[key]
		if !ok {
			return false
		}
		if !valuesEqual(expected, actual) {
			return false
		}
	}
	return true
}

// valuesEqual compares an expected match value against an actual tool-call
// argument. It coerces across numeric kinds (int vs float64) but does not
// conflate a number with its string form — see equalScalar (X-04).
func valuesEqual(expected, actual any) bool {
	return equalScalar(expected, actual)
}

func indexTools(tools []types.ToolDefinition) map[string]*types.ToolDefinition {
	m := make(map[string]*types.ToolDefinition, len(tools))
	for i := range tools {
		m[tools[i].Name] = &tools[i]
	}
	return m
}

func generateToolCallID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "call_000000000000"
	}
	return fmt.Sprintf("call_%x", b)
}

func shouldInjectError(rate float64) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return false
	}
	return float64(n.Int64())/10000.0 < rate
}

func formatValidationErrors(errs []string) string {
	if len(errs) == 1 {
		return errs[0]
	}
	msg := "multiple validation errors:"
	for _, e := range errs {
		msg += "\n  - " + e
	}
	return msg
}

// Package workflows contains the playground's workflow definitions. Each is
// a short, readable program written against workflow.Exec, and each
// demonstrates a set of agentic patterns (listed in its Info().Patterns).
package workflows

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
)

// All returns every workflow definition.
func All() []workflow.Definition {
	return []workflow.Definition{
		ResearchBrief{},
		SupportTriage{},
		DecisionReview{},
		ResilienceDrill{},
		SingleAgent{},
	}
}

func requireString(input map[string]any, key string, maxLen int) error {
	v, ok := input[key]
	if !ok {
		return fmt.Errorf("input.%s is required", key)
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("input.%s must be a string", key)
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("input.%s must not be empty", key)
	}
	if len([]rune(s)) > maxLen {
		return fmt.Errorf("input.%s must be at most %d characters", key, maxLen)
	}
	return nil
}

func optionalString(input map[string]any, key string, maxLen int) error {
	v, ok := input[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("input.%s must be a string", key)
	}
	if len([]rune(s)) > maxLen {
		return fmt.Errorf("input.%s must be at most %d characters", key, maxLen)
	}
	return nil
}

func optionalBool(input map[string]any, key string) error {
	v, ok := input[key]
	if !ok || v == nil {
		return nil
	}
	if _, ok := v.(bool); !ok {
		return fmt.Errorf("input.%s must be a boolean", key)
	}
	return nil
}

func rejectUnknown(input map[string]any, allowed ...string) error {
	var errs []error
	for k := range input {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("input.%s is not a recognized field (allowed: %s)", k, strings.Join(allowed, ", ")))
		}
	}
	return errors.Join(errs...)
}

func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

func objectSchema(required []string, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": props}
}

package engine

import (
	"fmt"
	"strings"

	"github.com/mockagents/mockagents/internal/types"
)

// ToolValidator validates tool call parameters against JSON Schema definitions.
type ToolValidator struct{}

// NewToolValidator creates a new ToolValidator.
func NewToolValidator() *ToolValidator {
	return &ToolValidator{}
}

// ValidateParameters checks that the given arguments conform to the tool's
// parameter schema. Returns a slice of human-readable error strings.
// An empty slice means validation passed.
func (v *ToolValidator) ValidateParameters(
	schema types.JSONSchemaObject,
	args map[string]any,
) []string {
	if len(schema) == 0 {
		return nil
	}

	var errs []string

	// Check type constraint.
	schemaType, _ := schema["type"].(string)
	if schemaType == "object" {
		errs = append(errs, v.validateObject(schema, args)...)
	}

	return errs
}

// validateObject checks an object-type schema: required fields and property types.
func (v *ToolValidator) validateObject(
	schema types.JSONSchemaObject,
	args map[string]any,
) []string {
	var errs []string

	// Check required fields.
	if required, ok := schema["required"]; ok {
		if reqList, ok := required.([]any); ok {
			for _, r := range reqList {
				name, ok := r.(string)
				if !ok {
					continue
				}
				if _, exists := args[name]; !exists {
					errs = append(errs, fmt.Sprintf("missing required parameter %q", name))
				}
			}
		}
	}

	// Check property types.
	if properties, ok := schema["properties"]; ok {
		propMap, ok := properties.(map[string]any)
		if !ok {
			return errs
		}
		for name, propSchema := range propMap {
			val, exists := args[name]
			if !exists {
				continue // Not required, skip.
			}
			propDef, ok := propSchema.(map[string]any)
			if !ok {
				continue
			}
			propType, _ := propDef["type"].(string)
			if propType != "" {
				if typeErr := v.checkType(name, val, propType); typeErr != "" {
					errs = append(errs, typeErr)
				}
			}

			// Check enum constraint.
			if enumVals, ok := propDef["enum"]; ok {
				if enumList, ok := enumVals.([]any); ok {
					if !v.inEnum(val, enumList) {
						errs = append(errs, fmt.Sprintf(
							"parameter %q value %v not in allowed values %v", name, val, enumList))
					}
				}
			}

			// Check string constraints.
			if propType == "string" {
				if str, ok := val.(string); ok {
					if minLen, ok := propDef["minLength"]; ok {
						if min, ok := toInt(minLen); ok && len(str) < min {
							errs = append(errs, fmt.Sprintf(
								"parameter %q length %d is less than minimum %d", name, len(str), min))
						}
					}
					if maxLen, ok := propDef["maxLength"]; ok {
						if max, ok := toInt(maxLen); ok && len(str) > max {
							errs = append(errs, fmt.Sprintf(
								"parameter %q length %d exceeds maximum %d", name, len(str), max))
						}
					}
				}
			}
		}
	}

	// Reject additional properties if additionalProperties is false.
	if addlProps, ok := schema["additionalProperties"]; ok {
		if addlBool, ok := addlProps.(bool); ok && !addlBool {
			propMap := getPropertyNames(schema)
			for name := range args {
				if !propMap[name] {
					errs = append(errs, fmt.Sprintf("unexpected parameter %q", name))
				}
			}
		}
	}

	return errs
}

func (v *ToolValidator) checkType(name string, val any, expectedType string) string {
	switch expectedType {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Sprintf("parameter %q: expected string, got %T", name, val)
		}
	case "integer":
		switch val.(type) {
		case int, int32, int64, float64:
			if f, ok := val.(float64); ok && f != float64(int64(f)) {
				return fmt.Sprintf("parameter %q: expected integer, got float", name)
			}
		default:
			return fmt.Sprintf("parameter %q: expected integer, got %T", name, val)
		}
	case "number":
		switch val.(type) {
		case int, int32, int64, float64:
		default:
			return fmt.Sprintf("parameter %q: expected number, got %T", name, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Sprintf("parameter %q: expected boolean, got %T", name, val)
		}
	case "array":
		if _, ok := val.([]any); !ok {
			return fmt.Sprintf("parameter %q: expected array, got %T", name, val)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Sprintf("parameter %q: expected object, got %T", name, val)
		}
	}
	return ""
}

func (v *ToolValidator) inEnum(val any, allowed []any) bool {
	valStr := fmt.Sprintf("%v", val)
	for _, a := range allowed {
		if fmt.Sprintf("%v", a) == valStr {
			return true
		}
	}
	return false
}

func getPropertyNames(schema types.JSONSchemaObject) map[string]bool {
	names := make(map[string]bool)
	if props, ok := schema["properties"]; ok {
		if propMap, ok := props.(map[string]any); ok {
			for name := range propMap {
				names[name] = true
			}
		}
	}
	return names
}

func toInt(val any) (int, bool) {
	switch v := val.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

// FormatValidationError creates a formatted error message for 422 responses.
func FormatValidationError(toolName string, errs []string) string {
	return fmt.Sprintf("tool %q parameter validation failed: %s", toolName, strings.Join(errs, "; "))
}

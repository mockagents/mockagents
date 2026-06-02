package engine

import (
	"fmt"
	"reflect"

	"github.com/mockagents/mockagents/internal/types"
)

// ToolValidator validates tool call parameters against JSON Schema definitions.
type ToolValidator struct{}

// NewToolValidator creates a new ToolValidator.
func NewToolValidator() *ToolValidator {
	return &ToolValidator{}
}

// ValidateParameters checks that the given arguments conform to the tool's
// parameter schema. Returns a slice of human-readable error strings;
// an empty slice means validation passed.
//
// Object schemas are validated recursively (F-TV-001): nested object
// properties and array `items` are checked against their own subschemas,
// with error messages path-qualified (e.g. "address.zip", "tags[2]").
// Known limit (F-TV-006): `additionalProperties` is enforced only in its
// boolean `false` form; the schema-object form is treated as permissive.
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

// validateObject checks an object-type schema: required fields, property
// constraints, and additionalProperties. It delegates to validateObjectAt
// with an empty path so top-level error messages stay unprefixed.
func (v *ToolValidator) validateObject(
	schema types.JSONSchemaObject,
	args map[string]any,
) []string {
	return v.validateObjectAt(schema, args, "")
}

// validateObjectAt is the recursive worker. `path` prefixes the names in
// error messages so nested fields read as "address.zip" / "tags[2]"; it is
// "" at the top level.
func (v *ToolValidator) validateObjectAt(
	schema map[string]any,
	args map[string]any,
	path string,
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
					errs = append(errs, fmt.Sprintf("missing required parameter %q", path+name))
				}
			}
		}
	}

	// Check property constraints. A malformed `properties` value skips the
	// per-property loop but must NOT skip the additionalProperties check
	// below (F-TV-002) — so this is a guarded block, not an early return.
	if properties, ok := schema["properties"]; ok {
		if propMap, ok := properties.(map[string]any); ok {
			for name, propSchema := range propMap {
				val, exists := args[name]
				if !exists {
					continue // absent optional property (required handled above)
				}
				propDef, ok := propSchema.(map[string]any)
				if !ok {
					continue
				}
				errs = append(errs, v.validateValue(propDef, val, path+name)...)
			}
		}
	}

	// Reject additional properties when additionalProperties is exactly
	// false. The schema-object form is treated as permissive (F-TV-006).
	if addlProps, ok := schema["additionalProperties"]; ok {
		if addlBool, ok := addlProps.(bool); ok && !addlBool {
			allowed := getPropertyNames(schema)
			for name := range args {
				if !allowed[name] {
					errs = append(errs, fmt.Sprintf("unexpected parameter %q", path+name))
				}
			}
		}
	}

	return errs
}

// validateValue checks a single value against a property schema: its type,
// enum, string-length constraints, and — for object/array types — its
// nested subschema (F-TV-001).
func (v *ToolValidator) validateValue(propDef map[string]any, val any, name string) []string {
	var errs []string

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

	switch propType {
	case "string":
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
	case "object":
		// Recurse into the nested object schema (F-TV-001).
		if nested, ok := val.(map[string]any); ok {
			errs = append(errs, v.validateObjectAt(propDef, nested, name+".")...)
		}
	case "array":
		errs = append(errs, v.validateArray(propDef, val, name)...)
	}

	return errs
}

// validateArray validates each element of an array against the schema's
// `items` subschema (F-TV-001). With no `items` schema the elements are
// unconstrained. A non-array value is left to the type check in
// validateValue to report.
func (v *ToolValidator) validateArray(propDef map[string]any, val any, name string) []string {
	arr, ok := val.([]any)
	if !ok {
		return nil
	}
	itemsSchema, ok := propDef["items"].(map[string]any)
	if !ok {
		return nil
	}
	var errs []string
	for i, item := range arr {
		errs = append(errs, v.validateValue(itemsSchema, item, fmt.Sprintf("%s[%d]", name, i))...)
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
	for _, a := range allowed {
		if equalScalar(a, val) {
			return true
		}
	}
	return false
}

// equalScalar reports whether two decoded JSON/YAML values are equal.
// All numeric kinds (int, int32, int64, float32, float64) compare by
// value, so 1 == 1.0 — the legitimate int-vs-float coercion. But kinds
// are never conflated: a number never equals a string, and a bool never
// equals the string "true". This replaces the previous
// fmt.Sprintf("%v") comparison, which matched 1 == "1" and true ==
// "true" (review finding X-04).
func equalScalar(a, b any) bool {
	if af, ok := toFloat(a); ok {
		bf, ok := toFloat(b)
		return ok && af == bf
	}
	if _, ok := toFloat(b); ok {
		return false // non-number vs number
	}
	// Neither is numeric: DeepEqual handles strings, bools, and nested
	// arrays/objects, and returns false across mismatched dynamic types.
	return reflect.DeepEqual(a, b)
}

// toFloat returns the float value of any numeric kind decoded from
// JSON/YAML, and false for non-numeric values.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
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

package engine

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
)

func objectSchema(props map[string]any, required []any) types.JSONSchemaObject {
	schema := types.JSONSchemaObject{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func TestToolValidator_ValidParams(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"name":  map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer"},
		},
		[]any{"name"},
	)
	args := map[string]any{"name": "test", "count": 5}

	errs := v.ValidateParameters(schema, args)
	assert.Empty(t, errs)
}

func TestToolValidator_MissingRequired(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"name": map[string]any{"type": "string"},
		},
		[]any{"name"},
	)
	args := map[string]any{}

	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "missing required")
	assert.Contains(t, errs[0], "name")
}

func TestToolValidator_MultipleRequired(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "string"},
		},
		[]any{"a", "b"},
	)
	args := map[string]any{}

	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 2)
}

func TestToolValidator_WrongType_String(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"name": map[string]any{"type": "string"},
		},
		nil,
	)
	args := map[string]any{"name": 123}

	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "expected string")
}

func TestToolValidator_WrongType_Integer(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"count": map[string]any{"type": "integer"},
		},
		nil,
	)
	args := map[string]any{"count": "not-a-number"}

	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "expected integer")
}

func TestToolValidator_WrongType_Boolean(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"flag": map[string]any{"type": "boolean"},
		},
		nil,
	)
	args := map[string]any{"flag": "yes"}

	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "expected boolean")
}

func TestToolValidator_WrongType_Number(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"value": map[string]any{"type": "number"},
		},
		nil,
	)
	args := map[string]any{"value": "not-a-number"}

	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "expected number")
}

func TestToolValidator_WrongType_Array(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"items": map[string]any{"type": "array"},
		},
		nil,
	)
	args := map[string]any{"items": "not-an-array"}

	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "expected array")
}

func TestToolValidator_WrongType_Object(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"nested": map[string]any{"type": "object"},
		},
		nil,
	)
	args := map[string]any{"nested": "not-an-object"}

	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "expected object")
}

func TestToolValidator_EnumConstraint(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"color": map[string]any{
				"type": "string",
				"enum": []any{"red", "green", "blue"},
			},
		},
		nil,
	)

	// Valid value.
	errs := v.ValidateParameters(schema, map[string]any{"color": "red"})
	assert.Empty(t, errs)

	// Invalid value.
	errs = v.ValidateParameters(schema, map[string]any{"color": "purple"})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "not in allowed values")
}

func TestEqualScalar_TypeAware(t *testing.T) {
	// Numeric kinds coerce by value (YAML int vs JSON float64).
	assert.True(t, equalScalar(1, 1.0))
	assert.True(t, equalScalar(int64(7), 7.0))
	assert.True(t, equalScalar(float64(3), int32(3)))
	// Cross-kind must never match (review finding X-04).
	assert.False(t, equalScalar(1, "1"))
	assert.False(t, equalScalar(true, "true"))
	assert.False(t, equalScalar("red", 0))
	// Same kind compares normally.
	assert.True(t, equalScalar("red", "red"))
	assert.False(t, equalScalar("red", "blue"))
}

func TestToolValidator_inEnum_TypeAware(t *testing.T) {
	v := NewToolValidator()
	// JSON-decoded float64 arg matches an int enum entry by value.
	assert.True(t, v.inEnum(2.0, []any{1, 2, 3}))
	// But the string "2" must NOT match a numeric enum (was a bug).
	assert.False(t, v.inEnum("2", []any{1, 2, 3}))
	// String enums are unaffected.
	assert.True(t, v.inEnum("green", []any{"red", "green", "blue"}))
	assert.False(t, v.inEnum("purple", []any{"red", "green", "blue"}))
}

func TestToolValidator_StringMinMaxLength(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"code": map[string]any{
				"type":      "string",
				"minLength": 3,
				"maxLength": 10,
			},
		},
		nil,
	)

	// Too short.
	errs := v.ValidateParameters(schema, map[string]any{"code": "ab"})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "less than minimum")

	// Too long.
	errs = v.ValidateParameters(schema, map[string]any{"code": "12345678901"})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "exceeds maximum")

	// Just right.
	errs = v.ValidateParameters(schema, map[string]any{"code": "abc123"})
	assert.Empty(t, errs)
}

func TestToolValidator_AdditionalPropertiesFalse(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"name": map[string]any{"type": "string"},
		},
		nil,
	)
	schema["additionalProperties"] = false

	args := map[string]any{"name": "test", "extra": "not allowed"}
	errs := v.ValidateParameters(schema, args)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "unexpected parameter")
}

func TestToolValidator_EmptySchema(t *testing.T) {
	v := NewToolValidator()
	errs := v.ValidateParameters(nil, map[string]any{"key": "val"})
	assert.Empty(t, errs)
}

func TestToolValidator_EmptyArgs(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{"name": map[string]any{"type": "string"}},
		nil,
	)
	errs := v.ValidateParameters(schema, map[string]any{})
	assert.Empty(t, errs)
}

func TestToolValidator_IntegerFromFloat64(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"count": map[string]any{"type": "integer"},
		},
		nil,
	)
	// JSON unmarshals numbers as float64.
	errs := v.ValidateParameters(schema, map[string]any{"count": float64(42)})
	assert.Empty(t, errs)

	// Non-integer float should fail.
	errs = v.ValidateParameters(schema, map[string]any{"count": float64(3.14)})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "expected integer")
}

func TestToolValidator_MultipleErrors(t *testing.T) {
	v := NewToolValidator()
	schema := objectSchema(
		map[string]any{
			"name":  map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer"},
		},
		[]any{"name", "count"},
	)
	// Missing both required params AND wrong types for supplied params.
	args := map[string]any{"name": 123, "count": "abc"}

	errs := v.ValidateParameters(schema, args)
	assert.GreaterOrEqual(t, len(errs), 2)
}

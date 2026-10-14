package toolschema

import (
	"encoding/json"
	"math"
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

func TestValidator_ValidParams(t *testing.T) {
	v := NewValidator()
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

func TestValidator_MissingRequired(t *testing.T) {
	v := NewValidator()
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

func TestValidator_MultipleRequired(t *testing.T) {
	v := NewValidator()
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

func TestValidator_WrongType_String(t *testing.T) {
	v := NewValidator()
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

func TestValidator_WrongType_Integer(t *testing.T) {
	v := NewValidator()
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

func TestValidator_WrongType_Boolean(t *testing.T) {
	v := NewValidator()
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

func TestValidator_WrongType_Number(t *testing.T) {
	v := NewValidator()
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

func TestValidator_WrongType_Array(t *testing.T) {
	v := NewValidator()
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

func TestValidator_WrongType_Object(t *testing.T) {
	v := NewValidator()
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

func TestValidator_EnumConstraint(t *testing.T) {
	v := NewValidator()
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
	assert.True(t, EqualScalar(1, 1.0))
	assert.True(t, EqualScalar(int64(7), 7.0))
	assert.True(t, EqualScalar(float64(3), int32(3)))
	// Cross-kind must never match (review finding X-04).
	assert.False(t, EqualScalar(1, "1"))
	assert.False(t, EqualScalar(true, "true"))
	assert.False(t, EqualScalar("red", 0))
	// Same kind compares normally.
	assert.True(t, EqualScalar("red", "red"))
	assert.False(t, EqualScalar("red", "blue"))
}

// Numeric coercion applies at every depth: a YAML int nested in an array or
// object equals the JSON float64 in the same position. Previously nested
// values fell to reflect.DeepEqual, so int(1) never equalled float64(1).
func TestEqualScalar_NestedNumericCoercion(t *testing.T) {
	assert.True(t, EqualScalar([]any{1, 2}, []any{1.0, 2.0}))
	assert.True(t, EqualScalar(
		map[string]any{"x": 1, "tags": []any{"a", int64(2)}},
		map[string]any{"x": 1.0, "tags": []any{"a", 2.0}}))
	assert.True(t, EqualScalar([]any{map[string]any{"n": 3}}, []any{map[string]any{"n": 3.0}}))
	assert.True(t, EqualScalar([]any{}, []any{}))
	assert.True(t, EqualScalar(map[string]any{}, map[string]any{}))

	// Kinds are still never conflated, at any depth.
	assert.False(t, EqualScalar([]any{1}, []any{"1"}))
	assert.False(t, EqualScalar(map[string]any{"b": true}, map[string]any{"b": "true"}))
	// Shape must match: length, keys, and container kind.
	assert.False(t, EqualScalar([]any{1, 2}, []any{1}))
	assert.False(t, EqualScalar([]any{1, 2}, []any{2, 1}))
	assert.False(t, EqualScalar(map[string]any{"x": 1}, map[string]any{"y": 1.0}))
	assert.False(t, EqualScalar(map[string]any{"x": 1}, map[string]any{"x": 1.0, "y": 2.0}))
	assert.False(t, EqualScalar([]any{1}, map[string]any{"0": 1.0}))
	assert.False(t, EqualScalar([]any{1}, 1.0))
}

// The enum check is where nested coercion matters in practice: a YAML enum
// of arrays holds ints while the JSON argument holds float64s.
func TestValidator_EnumOfArrays(t *testing.T) {
	v := NewValidator()
	schema := objectSchema(
		map[string]any{
			"pair": map[string]any{
				"type": "array",
				"enum": []any{[]any{1, 2}, []any{3, 4}},
			},
		},
		nil,
	)
	var args map[string]any
	if err := json.Unmarshal([]byte(`{"pair": [1, 2]}`), &args); err != nil {
		t.Fatal(err)
	}
	assert.Empty(t, v.ValidateParameters(schema, args))

	if err := json.Unmarshal([]byte(`{"pair": [2, 1]}`), &args); err != nil {
		t.Fatal(err)
	}
	errs := v.ValidateParameters(schema, args)
	if assert.Len(t, errs, 1) {
		assert.Contains(t, errs[0], "not in allowed values")
	}
}

func TestValidator_inEnum_TypeAware(t *testing.T) {
	v := NewValidator()
	// JSON-decoded float64 arg matches an int enum entry by value.
	assert.True(t, v.inEnum(2.0, []any{1, 2, 3}))
	// But the string "2" must NOT match a numeric enum (was a bug).
	assert.False(t, v.inEnum("2", []any{1, 2, 3}))
	// String enums are unaffected.
	assert.True(t, v.inEnum("green", []any{"red", "green", "blue"}))
	assert.False(t, v.inEnum("purple", []any{"red", "green", "blue"}))
}

func TestValidator_StringMinMaxLength(t *testing.T) {
	v := NewValidator()
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

func TestValidator_AdditionalPropertiesFalse(t *testing.T) {
	v := NewValidator()
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

func TestValidator_EmptySchema(t *testing.T) {
	v := NewValidator()
	errs := v.ValidateParameters(nil, map[string]any{"key": "val"})
	assert.Empty(t, errs)
}

func TestValidator_EmptyArgs(t *testing.T) {
	v := NewValidator()
	schema := objectSchema(
		map[string]any{"name": map[string]any{"type": "string"}},
		nil,
	)
	errs := v.ValidateParameters(schema, map[string]any{})
	assert.Empty(t, errs)
}

func TestValidator_IntegerFromFloat64(t *testing.T) {
	v := NewValidator()
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

// Integral numbers beyond the int64 range are still integers. The old check
// round-tripped through int64, an implementation-defined conversion out of
// range, so 1e19 was rejected as "expected integer".
func TestValidator_IntegerBeyondInt64Range(t *testing.T) {
	v := NewValidator()
	schema := objectSchema(
		map[string]any{
			"count": map[string]any{"type": "integer"},
		},
		nil,
	)
	for _, raw := range []string{`{"count": 1e19}`, `{"count": -1e19}`, `{"count": 9223372036854775808}`} {
		var args map[string]any
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		assert.Empty(t, v.ValidateParameters(schema, args), "%s is an integer", raw)
	}

	// Non-finite values are not integers (they cannot come from JSON, but
	// an in-process caller can pass them).
	for _, f := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		errs := v.ValidateParameters(schema, map[string]any{"count": f})
		if assert.Len(t, errs, 1, "%v must be rejected", f) {
			assert.Contains(t, errs[0], "expected integer")
		}
	}
}

func TestValidator_MultipleErrors(t *testing.T) {
	v := NewValidator()
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

// --- F-TV-009: nested schemas, fall-through, malformed inputs ---

func TestValidator_NestedObject(t *testing.T) {
	// F-TV-001: a nested object property is validated against its own
	// required + property schema, with path-qualified messages.
	v := NewValidator()
	schema := objectSchema(
		map[string]any{
			"address": map[string]any{
				"type":     "object",
				"required": []any{"zip"},
				"properties": map[string]any{
					"zip": map[string]any{"type": "string"},
				},
			},
		},
		[]any{"address"},
	)

	// Valid nested object.
	assert.Empty(t, v.ValidateParameters(schema, map[string]any{
		"address": map[string]any{"zip": "94016"},
	}))

	// Missing nested required field.
	errs := v.ValidateParameters(schema, map[string]any{"address": map[string]any{}})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], `"address.zip"`)
	assert.Contains(t, errs[0], "missing required")

	// Wrong nested type.
	errs = v.ValidateParameters(schema, map[string]any{
		"address": map[string]any{"zip": 94016},
	})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], `"address.zip"`)
	assert.Contains(t, errs[0], "expected string")
}

func TestValidator_ArrayItems(t *testing.T) {
	// F-TV-001: array elements are validated against the items subschema.
	v := NewValidator()
	schema := objectSchema(
		map[string]any{
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		nil,
	)

	errs := v.ValidateParameters(schema, map[string]any{"tags": []any{"a", 2, "c"}})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "tags[1]")
	assert.Contains(t, errs[0], "expected string")

	assert.Empty(t, v.ValidateParameters(schema, map[string]any{"tags": []any{"a", "b"}}))
}

func TestValidator_MalformedProperties_StillChecksAdditional(t *testing.T) {
	// F-TV-002: a malformed `properties` value must not skip the
	// additionalProperties:false check (it used to early-return).
	v := NewValidator()
	schema := types.JSONSchemaObject{
		"type":                 "object",
		"properties":           "not-a-map",
		"additionalProperties": false,
	}
	errs := v.ValidateParameters(schema, map[string]any{"foo": 1})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "unexpected parameter")
	assert.Contains(t, errs[0], "foo")
}

func TestValidator_MalformedRequired_NoPanic(t *testing.T) {
	v := NewValidator()
	// `required` of the wrong type is ignored, not enforced or panicked on.
	assert.NotPanics(t, func() {
		errs := v.ValidateParameters(types.JSONSchemaObject{
			"type":     "object",
			"required": "name", // should be a list
		}, map[string]any{})
		assert.Empty(t, errs)
	})

	// A non-string entry inside `required` is skipped; valid ones still apply.
	errs := v.ValidateParameters(types.JSONSchemaObject{
		"type":       "object",
		"required":   []any{"name", 42},
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
	}, map[string]any{})
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "name")
}

func TestValidator_AdditionalPropertiesSchemaFormPermissive(t *testing.T) {
	// F-TV-006: only the boolean `false` form is enforced; the schema-object
	// form is treated as permissive, so extra properties are allowed.
	v := NewValidator()
	schema := objectSchema(
		map[string]any{"name": map[string]any{"type": "string"}},
		nil,
	)
	schema["additionalProperties"] = map[string]any{"type": "string"}
	assert.Empty(t, v.ValidateParameters(schema, map[string]any{"name": "x", "extra": "ok"}))
}

// String lengths count characters, not bytes (review E-17).
func TestValidator_StringLengthCountsCharacters(t *testing.T) {
	v := NewValidator()
	schema := objectSchema(map[string]any{
		"word": map[string]any{"type": "string", "minLength": 3, "maxLength": 3},
	}, nil)
	assert.Empty(t, v.ValidateParameters(schema, map[string]any{"word": "äöü"}))
	assert.Empty(t, v.ValidateParameters(schema, map[string]any{"word": "日本語"}))
	assert.NotEmpty(t, v.ValidateParameters(schema, map[string]any{"word": "äöüß"}))
}

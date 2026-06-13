package adapter

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// synth synthesizes from a schema and unmarshals the result, asserting it is
// always well-formed JSON.
func synth(t *testing.T, schema map[string]any) (any, string) {
	t.Helper()
	s := SynthesizeFromSchema(schema)
	var v any
	require.NoErrorf(t, json.Unmarshal([]byte(s), &v), "synth output not valid JSON: %s", s)
	return v, s
}

func TestSynth_EmptySchema(t *testing.T) {
	assert.Equal(t, "{}", SynthesizeFromSchema(nil))
	assert.Equal(t, "{}", SynthesizeFromSchema(map[string]any{}))
}

func TestSynth_FlatObject(t *testing.T) {
	v, _ := synth(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name", "age"},
	})
	obj := v.(map[string]any)
	assert.Equal(t, "", obj["name"])
	assert.Equal(t, float64(0), obj["age"])
}

func TestSynth_NestedObject(t *testing.T) {
	v, _ := synth(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"address": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
					"zip":  map[string]any{"type": "string"},
				},
			},
		},
	})
	addr := v.(map[string]any)["address"].(map[string]any)
	assert.Contains(t, addr, "city")
	assert.Contains(t, addr, "zip")
}

func TestSynth_StringFormats(t *testing.T) {
	cases := map[string]string{
		"date-time": "2024-01-01T00:00:00Z",
		"date":      "2024-01-01",
		"email":     "user@example.com",
		"uri":       "https://example.com",
	}
	for format, want := range cases {
		v, _ := synth(t, map[string]any{"type": "string", "format": format})
		assert.Equal(t, want, v, "format %s", format)
	}
}

func TestSynth_EnumAndConst(t *testing.T) {
	v, _ := synth(t, map[string]any{"enum": []any{"red", "green", "blue"}})
	assert.Equal(t, "red", v)

	v2, _ := synth(t, map[string]any{"const": float64(42)})
	assert.Equal(t, float64(42), v2)
}

func TestSynth_Scalars(t *testing.T) {
	b, _ := synth(t, map[string]any{"type": "boolean"})
	assert.Equal(t, false, b)
	n, _ := synth(t, map[string]any{"type": "null"})
	assert.Nil(t, n)
}

func TestSynth_Array(t *testing.T) {
	v, _ := synth(t, map[string]any{"type": "array", "items": map[string]any{"type": "integer"}})
	assert.Equal(t, []any{float64(0)}, v)

	// minItems honored
	v2, _ := synth(t, map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 3})
	assert.Len(t, v2, 3)

	// minItems capped at maxSynthArrayItems
	v3, _ := synth(t, map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 100})
	assert.Len(t, v3, maxSynthArrayItems)
}

func TestSynth_NullableType(t *testing.T) {
	v, _ := synth(t, map[string]any{"type": []any{"string", "null"}})
	assert.Equal(t, "", v)

	v2, _ := synth(t, map[string]any{"type": []any{"null"}})
	assert.Nil(t, v2)
}

func TestSynth_RefResolution(t *testing.T) {
	v, _ := synth(t, map[string]any{
		"$defs": map[string]any{
			"Address": map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		},
		"type": "object",
		"properties": map[string]any{
			"home": map[string]any{"$ref": "#/$defs/Address"},
		},
	})
	home := v.(map[string]any)["home"].(map[string]any)
	assert.Contains(t, home, "city")
}

func TestSynth_RefCycleTerminates(t *testing.T) {
	// A -> B -> A. The depth guard must terminate this without a panic.
	schema := map[string]any{
		"$defs": map[string]any{
			"A": map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"$ref": "#/$defs/B"}}},
			"B": map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"$ref": "#/$defs/A"}}},
		},
		"$ref": "#/$defs/A",
	}
	out := SynthesizeFromSchema(schema)
	assert.True(t, json.Valid([]byte(out)), "cyclic schema must still yield valid JSON")
}

// TestSynth_BreadthBlowupBounded pins the node-budget fix: a wide AND recursive
// schema (every node $ref-ing back N times) would multiply node count by N at
// each depth level — N^12 work — if only the depth cap bounded it. The shared
// node budget must keep total work (and time) bounded.
func TestSynth_BreadthBlowupBounded(t *testing.T) {
	const N = 40
	props := map[string]any{}
	for i := 0; i < N; i++ {
		props[string(rune('a'+i%26))+string(rune('0'+i/26))] = map[string]any{"$ref": "#/$defs/Node"}
	}
	schema := map[string]any{
		"$defs": map[string]any{"Node": map[string]any{"type": "object", "properties": props}},
		"$ref":  "#/$defs/Node",
	}
	done := make(chan string, 1)
	go func() { done <- SynthesizeFromSchema(schema) }()
	select {
	case s := <-done:
		assert.True(t, json.Valid([]byte(s)), "must still be valid JSON")
	case <-time.After(3 * time.Second):
		t.Fatal("synthesis did not complete within 3s — node budget not bounding breadth*depth")
	}
}

func TestSynth_DeepNestingTerminates(t *testing.T) {
	inner := map[string]any{"type": "string"}
	for i := 0; i < 30; i++ {
		inner = map[string]any{"type": "object", "properties": map[string]any{"child": inner}}
	}
	out := SynthesizeFromSchema(inner)
	assert.True(t, json.Valid([]byte(out)), "deeply nested schema must terminate with valid JSON")
}

func TestSynth_AnyOfOneOfAllOf(t *testing.T) {
	v, _ := synth(t, map[string]any{"anyOf": []any{
		map[string]any{"type": "string"},
		map[string]any{"type": "integer"},
	}})
	assert.Equal(t, "", v)

	v2, _ := synth(t, map[string]any{"allOf": []any{
		map[string]any{"properties": map[string]any{"a": map[string]any{"type": "string"}}},
		map[string]any{"properties": map[string]any{"b": map[string]any{"type": "integer"}}},
	}})
	obj := v2.(map[string]any)
	assert.Contains(t, obj, "a")
	assert.Contains(t, obj, "b")
}

func TestSynth_NumericMinimum(t *testing.T) {
	v, _ := synth(t, map[string]any{"type": "integer", "minimum": 5})
	assert.Equal(t, float64(5), v)
	v2, _ := synth(t, map[string]any{"type": "number", "minimum": 1.5})
	assert.Equal(t, 1.5, v2)
}

func TestSynth_ExclusiveBounds(t *testing.T) {
	// Pydantic Field(gt=0) -> exclusiveMinimum:0; must NOT emit the violating 0.
	v, _ := synth(t, map[string]any{"type": "integer", "exclusiveMinimum": 0})
	assert.Equal(t, float64(1), v)
	v2, _ := synth(t, map[string]any{"type": "number", "exclusiveMinimum": 2.5})
	assert.Greater(t, v2.(float64), 2.5)
	// non-finite minimum is ignored (no overflow / no NaN-marshal failure).
	out := SynthesizeFromSchema(map[string]any{"type": "integer", "minimum": math.Inf(1)})
	assert.True(t, json.Valid([]byte(out)))
}

// TestSynth_AllOfWithRef pins the Pydantic-inheritance shape:
// allOf:[{$ref:Base},{properties:{extra}}] must merge Base's fields, not drop them.
func TestSynth_AllOfWithRef(t *testing.T) {
	v, _ := synth(t, map[string]any{
		"$defs": map[string]any{
			"Base": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
			},
		},
		"allOf": []any{
			map[string]any{"$ref": "#/$defs/Base"},
			map[string]any{"properties": map[string]any{"extra": map[string]any{"type": "integer"}}},
		},
	})
	obj := v.(map[string]any)
	assert.Contains(t, obj, "id", "Base's $ref'd field must survive the allOf merge")
	assert.Contains(t, obj, "extra")
}

// TestSynth_RecursiveRefTypeCorrect verifies a recursive linked-list schema
// terminates with a type-correct value ({} object) at the cutoff, never a
// contract-violating null.
func TestSynth_RecursiveRefTypeCorrect(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"Node": map[string]any{
				"type":       "object",
				"properties": map[string]any{"next": map[string]any{"$ref": "#/$defs/Node"}},
			},
		},
		"$ref": "#/$defs/Node",
	}
	out := SynthesizeFromSchema(schema)
	var v any
	require.NoError(t, json.Unmarshal([]byte(out), &v))
	// Every "next" that is PRESENT must be an object (map) — never an explicit
	// null (the pre-fix bug). The chain terminates at the bound with a `{}` that
	// simply omits "next", which is type-correct.
	cur, ok := v.(map[string]any)
	require.True(t, ok, "root must be an object")
	for {
		nxt, present := cur["next"]
		if !present {
			break // type-correct empty object at the depth bound
		}
		m, isMap := nxt.(map[string]any)
		require.Truef(t, isMap, "next must be an object, got %T (%v)", nxt, nxt)
		cur = m
	}
}

func TestSynth_PrefixItemsTuple(t *testing.T) {
	v, _ := synth(t, map[string]any{
		"type":        "array",
		"prefixItems": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}},
	})
	assert.Equal(t, []any{"", float64(0)}, v)

	// draft-07 array-valued items form
	v2, _ := synth(t, map[string]any{
		"type":  "array",
		"items": []any{map[string]any{"type": "boolean"}},
	})
	assert.Equal(t, []any{false}, v2)
}

func TestSynth_UnknownTypeNoPanic(t *testing.T) {
	v, _ := synth(t, map[string]any{"type": "exotic"})
	assert.Nil(t, v)
}

func TestSynth_RealWorldOpenAISchema(t *testing.T) {
	// The canonical CalendarEvent example from the OpenAI structured-outputs docs.
	v, _ := synth(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"date": map[string]any{"type": "string"},
			"participants": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required":             []any{"name", "date", "participants"},
		"additionalProperties": false,
	})
	obj := v.(map[string]any)
	assert.Equal(t, "", obj["name"])
	assert.Equal(t, "", obj["date"])
	assert.Equal(t, []any{""}, obj["participants"])
}

func TestIsValidJSONAndObject(t *testing.T) {
	assert.True(t, IsValidJSON(`{"a":1}`))
	assert.True(t, IsValidJSON(`123`))
	assert.False(t, IsValidJSON(``))
	assert.False(t, IsValidJSON(`not json`))

	assert.True(t, isJSONObject(`  {"a":1}`))
	assert.False(t, isJSONObject(`123`))
	assert.False(t, isJSONObject(`[1,2]`))
}

// --- applyResponseFormat policy (unit) ---

func TestApplyResponseFormat_Policy(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}
	jsonSchemaRF := &ResponseFormat{Type: "json_schema", JSONSchema: &ResponseFormatJSON{Schema: schema}}

	t.Run("nil is no-op", func(t *testing.T) {
		r := &engine.Response{Content: "plain"}
		applyResponseFormat(nil, r)
		assert.Equal(t, "plain", r.Content)
	})

	t.Run("text is no-op", func(t *testing.T) {
		r := &engine.Response{Content: "plain"}
		applyResponseFormat(&ResponseFormat{Type: "text"}, r)
		assert.Equal(t, "plain", r.Content)
	})

	t.Run("json_schema synthesizes from plain text", func(t *testing.T) {
		r := &engine.Response{Content: "How can I help?"}
		applyResponseFormat(jsonSchemaRF, r)
		assert.True(t, IsValidJSON(r.Content))
		var v map[string]any
		require.NoError(t, json.Unmarshal([]byte(r.Content), &v))
		assert.Contains(t, v, "x")
	})

	t.Run("json_schema keeps author-provided JSON", func(t *testing.T) {
		r := &engine.Response{Content: `{"x":"hi"}`}
		applyResponseFormat(jsonSchemaRF, r)
		assert.JSONEq(t, `{"x":"hi"}`, r.Content)
	})

	t.Run("json_schema refusal wins, clears content + sets content_filter", func(t *testing.T) {
		r := &engine.Response{Content: "ignored", Refusal: "I can't"}
		applyResponseFormat(jsonSchemaRF, r)
		assert.Equal(t, "I can't", r.Refusal)
		assert.Equal(t, "content_filter", r.FinishReason)
		assert.Empty(t, r.Content, "a refusal must carry content:null")
	})

	t.Run("json_schema rejects bare-scalar content (root-type mismatch)", func(t *testing.T) {
		r := &engine.Response{Content: "42"} // valid JSON, but not an object
		applyResponseFormat(jsonSchemaRF, r)
		assert.True(t, isJSONObject(r.Content), "scalar content under an object schema must be synthesized: %s", r.Content)
	})

	t.Run("json_object refusal clears content + content_filter", func(t *testing.T) {
		r := &engine.Response{Content: "ignored", Refusal: "no"}
		applyResponseFormat(&ResponseFormat{Type: "json_object"}, r)
		assert.Empty(t, r.Content)
		assert.Equal(t, "content_filter", r.FinishReason)
	})

	t.Run("json_schema does not clobber tool calls", func(t *testing.T) {
		r := &engine.Response{ToolCalls: []types.ToolCallSpec{{Name: "f"}}}
		applyResponseFormat(jsonSchemaRF, r)
		assert.Empty(t, r.Content)
	})

	t.Run("json_object replaces invalid content", func(t *testing.T) {
		r := &engine.Response{Content: "not json"}
		applyResponseFormat(&ResponseFormat{Type: "json_object"}, r)
		assert.Equal(t, "{}", r.Content)
	})

	t.Run("json_object keeps valid object", func(t *testing.T) {
		r := &engine.Response{Content: `{"ok":true}`}
		applyResponseFormat(&ResponseFormat{Type: "json_object"}, r)
		assert.JSONEq(t, `{"ok":true}`, r.Content)
	})

	t.Run("json_schema nil schema degrades to empty object", func(t *testing.T) {
		r := &engine.Response{Content: "plain"}
		applyResponseFormat(&ResponseFormat{Type: "json_schema"}, r)
		assert.Equal(t, "{}", r.Content)
	})
}

package toolschema

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// FuzzValidate drives the tool-argument validator and the strict:true
// subset checker with a fuzzed JSON schema and a fuzzed JSON instance. Both
// inputs are attacker-controlled on the wire: the schema arrives in a
// request's tools[].function.parameters (and MCP tool definitions), the
// instance is a tool call's arguments.
//
// Invariants:
//   - ValidateParameters and ValidateStrictSubset never panic, whatever
//     shape the decoded schema has (wrong-typed keywords, deep nesting);
//   - validating the same pair twice yields the same set of errors. Error
//     ORDER may vary because properties are a Go map, so the comparison is
//     on the sorted list;
//   - a nil/empty schema accepts everything.
func FuzzValidate(f *testing.F) {
	seeds := []struct{ schema, instance string }{
		{`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":5},"count":{"type":"integer"}},"required":["name"]}`, `{"name":"test","count":5}`},
		{`{"type":"object","properties":{"name":{"type":"string"}},"required":["name","missing"],"additionalProperties":false}`, `{"extra":1}`},
		{`{"type":"object","properties":{"unit":{"type":"string","enum":["c","f"]},"n":{"type":"number","enum":[1,2.5]}}}`, `{"unit":"k","n":1.0}`},
		{`{"type":"object","properties":{"address":{"type":"object","properties":{"zip":{"type":"string"}},"required":["zip"],"additionalProperties":false}}}`, `{"address":{"zip":12345,"x":1}}`},
		{`{"type":"object","properties":{"tags":{"type":"array","items":{"type":"object","properties":{"k":{"type":"boolean"}}}}}}`, `{"tags":[{"k":true},{"k":"no"},3]}`},
		{`{"type":"object","properties":{"i":{"type":"integer"}}}`, `{"i":1.5}`},
		{`{"type":"object","properties":{"i":{"type":"integer","minLength":1e300}}}`, `{"i":1e300}`},
		{`{"type":"object","properties":"oops","required":"also oops","additionalProperties":false}`, `{"a":1}`},
		{`{"type":"object","properties":{"a":{"anyOf":[{"type":"object","properties":{"b":{}}}]}},"required":["a"],"additionalProperties":false}`, `{"a":{}}`},
		{`{"properties":{"x":{"items":{"properties":{}}}}}`, `{}`},
		{`{}`, `{"anything":true}`},
		{`null`, `null`},
	}
	for _, s := range seeds {
		f.Add([]byte(s.schema), []byte(s.instance))
	}
	v := NewValidator()
	f.Fuzz(func(t *testing.T, schemaJSON, instanceJSON []byte) {
		var schema types.JSONSchemaObject
		if err := json.Unmarshal(schemaJSON, &schema); err != nil {
			return
		}
		var args map[string]any
		if err := json.Unmarshal(instanceJSON, &args); err != nil {
			return
		}

		first := v.ValidateParameters(schema, args)
		second := v.ValidateParameters(schema, args)
		if !sameErrors(first, second) {
			t.Fatalf("ValidateParameters not deterministic:\nfirst:  %q\nsecond: %q", first, second)
		}
		if len(schema) == 0 && len(first) != 0 {
			t.Fatalf("empty schema rejected input: %q", first)
		}

		s1 := ValidateStrictSubset(schema)
		s2 := ValidateStrictSubset(schema)
		if !sameErrors(s1, s2) {
			t.Fatalf("ValidateStrictSubset not deterministic:\nfirst:  %q\nsecond: %q", s1, s2)
		}
	})
}

func sameErrors(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return reflect.DeepEqual(a, b)
}

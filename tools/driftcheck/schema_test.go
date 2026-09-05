package main

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A checker that cannot fail is worse than no checker: it reports green while
// the thing it guards rots. These drive checkSchemaFields with deliberately
// broken input so the failure paths are exercised, not assumed.

func specWith(t *testing.T, body string) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := yaml.Unmarshal([]byte(body), &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

type sample struct {
	Kept    string `json:"kept"`
	Dropped string `json:"dropped,omitempty"`
	Hidden  string `json:"-"`
	noTag   string //nolint:unused // proves unexported fields are skipped
}

type embedded struct {
	sample
	Extra string `json:"extra"`
}

func TestJSONFields(t *testing.T) {
	got := jsonFields(reflect.TypeOf(sample{}))
	for _, want := range []string{"kept", "dropped"} {
		if !got[want] {
			t.Errorf("missing %q", want)
		}
	}
	// json:"-" is not on the wire, so documenting it would be wrong.
	if got["Hidden"] || got["-"] {
		t.Error("a json:\"-\" field must not be treated as a wire field")
	}
	if got["noTag"] {
		t.Error("an unexported field is never marshalled")
	}
}

func TestJSONFields_FlattensEmbedded(t *testing.T) {
	// encoding/json flattens an anonymous struct into its parent, so the
	// spec sees the parent's fields — a checker that did not would demand
	// documentation for a property that never appears.
	got := jsonFields(reflect.TypeOf(embedded{}))
	for _, want := range []string{"kept", "dropped", "extra"} {
		if !got[want] {
			t.Errorf("missing %q from the flattened set", want)
		}
	}
}

const twoSchemas = `
components:
  schemas:
    Checked:
      type: object
      properties:
        kept: { type: string }
        dropped: { type: string }
    OtherThing:
      type: object
      properties:
        whatever: { type: string }
`

func run(t *testing.T, spec map[string]any, checked []checkedSchema, docOnly map[string]string) []string {
	t.Helper()
	origChecked, origDoc := checkedSchemas, documentationOnly
	checkedSchemas, documentationOnly = checked, docOnly
	t.Cleanup(func() { checkedSchemas, documentationOnly = origChecked, origDoc })
	return checkSchemaFields(spec)
}

func TestCheckSchemaFields_AgreementIsSilent(t *testing.T) {
	problems := run(t,
		specWith(t, twoSchemas),
		[]checkedSchema{{name: "Checked", sample: sample{}}},
		map[string]string{"OtherThing": "hand-written"},
	)
	if len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
}

func TestCheckSchemaFields_CatchesAnUndocumentedGoField(t *testing.T) {
	// The case that let `source` and `persistence` ship undocumented.
	spec := specWith(t, strings.Replace(twoSchemas, "        dropped: { type: string }\n", "", 1))
	problems := run(t, spec,
		[]checkedSchema{{name: "Checked", sample: sample{}}},
		map[string]string{"OtherThing": "hand-written"})

	if !containsMatch(problems, `Go emits "dropped"`) {
		t.Errorf("undocumented Go field not reported: %v", problems)
	}
}

func TestCheckSchemaFields_CatchesAPhantomSpecProperty(t *testing.T) {
	// The case that left `request_summary` in the spec for months.
	spec := specWith(t, strings.Replace(twoSchemas,
		"        kept: { type: string }",
		"        kept: { type: string }\n        request_summary: { type: string }", 1))
	problems := run(t, spec,
		[]checkedSchema{{name: "Checked", sample: sample{}}},
		map[string]string{"OtherThing": "hand-written"})

	if !containsMatch(problems, `documents "request_summary"`) {
		t.Errorf("phantom spec property not reported: %v", problems)
	}
}

func TestCheckSchemaFields_CatchesAnUnregisteredSchema(t *testing.T) {
	// Adding a schema and forgetting to decide whether it is checkable is how
	// coverage silently shrinks.
	problems := run(t, specWith(t, twoSchemas),
		[]checkedSchema{{name: "Checked", sample: sample{}}},
		map[string]string{})

	if !containsMatch(problems, `"OtherThing" is neither checked`) {
		t.Errorf("unregistered schema not reported: %v", problems)
	}
}

func TestCheckSchemaFields_AllowlistsNeedAReasonAndMustStayTrue(t *testing.T) {
	// An allowlist entry that no longer describes a real difference will
	// quietly excuse a future one, so a stale entry is itself drift.
	problems := run(t, specWith(t, twoSchemas),
		[]checkedSchema{{
			name:   "Checked",
			sample: sample{},
			goOnly: map[string]string{"dropped": "but it IS documented, so this is stale"},
		}},
		map[string]string{"OtherThing": "hand-written"})

	if !containsMatch(problems, `goOnly entry "dropped" no longer applies`) {
		t.Errorf("stale allowlist entry not reported: %v", problems)
	}
}

func TestCheckSchemaFields_CatchesACheckedSchemaThatVanished(t *testing.T) {
	problems := run(t, specWith(t, twoSchemas),
		[]checkedSchema{{name: "Gone", sample: sample{}}},
		map[string]string{"Checked": "x", "OtherThing": "x"})

	if !containsMatch(problems, `"Gone" is checked against Go but is not in the document`) {
		t.Errorf("a checked schema missing from the document was not reported: %v", problems)
	}
}

func containsMatch(problems []string, want string) bool {
	for _, p := range problems {
		if strings.Contains(p, want) {
			return true
		}
	}
	return false
}

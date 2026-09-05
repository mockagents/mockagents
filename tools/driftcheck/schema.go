package main

// Schema-field drift: does docs/api-spec.yaml still describe the fields the Go
// wire types actually carry?
//
// This exists because the spec rotted silently three times in one week and
// nothing caught it. `AgentSummary` documented `tools_count`, `scenarios_count`
// and `status` — none of which exist — while omitting `model`. `InteractionLog`
// typed `id` as a uuid string and documented `request_summary` /
// `response_summary`, which have never existed. A `$ref` check cannot see any
// of that: every reference resolved fine, pointing at a schema describing a
// different API.
//
// Scope, deliberately narrow: the SET of top-level property names, compared in
// both directions. Not types, not nesting, not descriptions. Field sets are
// what drift when someone adds a struct field, and they are checkable without
// the tool becoming a second, worse OpenAPI validator that everyone learns to
// silence.
//
// Reflection rather than AST parsing, so what is compared is what encoding/json
// will actually emit — embedded structs flattened, `json:"-"` skipped, tags
// honoured — rather than a re-implementation of those rules that can disagree
// with the marshaller.

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/server"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// checkedSchema pairs a schema in the OpenAPI document with the Go value that
// produces it.
type checkedSchema struct {
	// name is the key under components.schemas.
	name string
	// sample is any value of the wire type; only its type is used.
	sample any
	// specOnly are properties the spec documents that the Go type does not
	// carry, with the reason. Every entry is a claim that the difference is
	// intentional — an empty map is the healthy state.
	specOnly map[string]string
	// goOnly are JSON fields the Go type emits that the spec deliberately does
	// not document, with the reason.
	goOnly map[string]string
}

// checkedSchemas is the set of schemas machine-checked against Go.
//
// Explicit rather than discovered, and that is the point: a schema listed here
// cannot drift, and one that is not is visibly unchecked rather than silently
// assumed correct. Every schema in the document must appear either here or in
// documentationOnly, so adding one is a decision rather than an omission.
var checkedSchemas = []checkedSchema{
	{name: "AgentSummary", sample: server.AgentSummary{}},
	{name: "InteractionLog", sample: server.LogWithCost{}},
	{name: "IdentityResponse", sample: server.IdentityResponse{}},
	{name: "CostReport", sample: server.CostsResponse{}},
	{name: "CostGroup", sample: server.CostGroup{}},
	{name: "AuditEvent", sample: audit.Event{}},
	{name: "AuditActor", sample: audit.Actor{}},
	{name: "Tenant", sample: tenancy.Tenant{}},
	{name: "APIKey", sample: tenancy.APIKey{}},
}

// documentationOnly names schemas with no single Go wire type behind them, and
// says why. These are not silently skipped: leaving one out of both lists fails
// the check, so "we forgot" and "there is nothing to compare" stay distinct.
var documentationOnly = map[string]string{
	"AgentDefinition":              "describes the agent YAML, whose authority is schema/mockagents-v1-agent.json",
	"Pipeline":                     "describes the pipeline YAML, not a Go response type",
	"HealthResponse":               "assembled inline in the handler from a map literal",
	"ReadinessResponse":            "assembled by an unexported type",
	"ReadinessCheck":               "the wire payload is an unexported type; the exported ReadinessCheck is the probe CONFIG, not the response",
	"AgentWriteResult":             "assembled by an unexported type",
	"ValidateResult":               "assembled by an unexported type",
	"ReloadResponse":               "assembled inline in the handler",
	"InteractionDetail":            "documentation view of a single log; the row shape is checked as InteractionLog",
	"Session":                      "engine-internal conversation state, not a response type",
	"Error":                        "shared error envelope, written inline by writeJSON callers",
	"NewKeyResult":                 "assembled by an unexported type",
	"QuotaConfig":                  "assembled by an unexported type",
	"OpenAIChatCompletionRequest":  "mirrors the provider's wire format, whose authority is the provider",
	"OpenAIChatMessage":            "mirrors the provider's wire format",
	"OpenAITool":                   "mirrors the provider's wire format",
	"OpenAIToolCall":               "mirrors the provider's wire format",
	"OpenAIChatCompletionResponse": "mirrors the provider's wire format",
	"OpenAIUsage":                  "mirrors the provider's wire format",
	"OpenAIChatCompletionChunk":    "mirrors the provider's wire format",
	"AnthropicMessagesRequest":     "mirrors the provider's wire format",
	"AnthropicMessage":             "mirrors the provider's wire format",
	"AnthropicContentBlock":        "mirrors the provider's wire format",
	"AnthropicTool":                "mirrors the provider's wire format",
	"AnthropicMessagesResponse":    "mirrors the provider's wire format",
	"AnthropicUsage":               "mirrors the provider's wire format",
	"AnthropicStreamEvent":         "mirrors the provider's wire format",
}

// checkSchemaFields compares each registered schema's documented properties
// against the JSON fields its Go type emits.
func checkSchemaFields(spec map[string]any) []string {
	schemas, err := specSchemas(spec)
	if err != nil {
		return []string{err.Error()}
	}

	var problems []string
	seen := make(map[string]bool, len(checkedSchemas))

	for _, cs := range checkedSchemas {
		seen[cs.name] = true
		node, ok := schemas[cs.name]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"schema %q is checked against Go but is not in the document — add it, or drop it from checkedSchemas",
				cs.name))
			continue
		}
		documented, err := schemaProperties(node)
		if err != nil {
			problems = append(problems, fmt.Sprintf("schema %q: %v", cs.name, err))
			continue
		}
		problems = append(problems, compareFields(cs, documented, jsonFields(reflect.TypeOf(cs.sample)))...)
	}

	// Anything in the document that is neither checked nor declared
	// documentation-only. This is what stops a new schema from quietly joining
	// the ones nobody verifies.
	for name := range schemas {
		if seen[name] {
			continue
		}
		if _, ok := documentationOnly[name]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"schema %q is neither checked against a Go type nor listed in documentationOnly — "+
				"add it to one, so an unchecked schema is a decision rather than an oversight",
			name))
	}

	sort.Strings(problems)
	return problems
}

func compareFields(cs checkedSchema, documented, actual map[string]bool) []string {
	var problems []string

	for field := range actual {
		if documented[field] {
			continue
		}
		if _, ok := cs.goOnly[field]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"schema %s: Go emits %q but the spec does not document it", cs.name, field))
	}

	for field := range documented {
		if actual[field] {
			continue
		}
		if _, ok := cs.specOnly[field]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"schema %s: the spec documents %q but no Go field emits it", cs.name, field))
	}

	// A stale allowlist is its own kind of drift: an entry that no longer
	// describes a real difference will quietly excuse a future one.
	for field := range cs.goOnly {
		if !actual[field] || documented[field] {
			problems = append(problems, fmt.Sprintf(
				"schema %s: goOnly entry %q no longer applies — remove it", cs.name, field))
		}
	}
	for field := range cs.specOnly {
		if !documented[field] || actual[field] {
			problems = append(problems, fmt.Sprintf(
				"schema %s: specOnly entry %q no longer applies — remove it", cs.name, field))
		}
	}
	return problems
}

// specSchemas pulls components.schemas out of a parsed OpenAPI document.
func specSchemas(spec map[string]any) (map[string]any, error) {
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("api-spec has no components block")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("api-spec has no components.schemas block")
	}
	return schemas, nil
}

// schemaProperties returns the top-level property names a schema declares.
func schemaProperties(node any) (map[string]bool, error) {
	obj, ok := node.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("not an object")
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("has no properties block")
	}
	out := make(map[string]bool, len(props))
	for name := range props {
		out[name] = true
	}
	return out, nil
}

// jsonFields returns the JSON names a struct type marshals to, flattening
// embedded structs the way encoding/json does.
func jsonFields(t reflect.Type) map[string]bool {
	out := map[string]bool{}
	collectJSONFields(t, out)
	return out
}

func collectJSONFields(t reflect.Type, out map[string]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		// An embedded struct with no name of its own is flattened into its
		// parent, so its fields are the parent's fields on the wire.
		if f.Anonymous && name == "" {
			collectJSONFields(f.Type, out)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = true
	}
}

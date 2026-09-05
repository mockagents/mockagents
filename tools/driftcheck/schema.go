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

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/server"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
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
	{name: "ReadinessResponse", sample: server.ReadinessResponse{}},
	{name: "ReadinessCheck", sample: server.ReadinessCheckResult{}},
	{name: "AgentWriteResult", sample: server.AgentWriteResponse{}},
	{name: "ValidateResult", sample: server.ValidateResponse{}},
	{name: "CostReport", sample: server.CostsResponse{}},
	{name: "CostGroup", sample: server.CostGroup{}},
	{name: "AuditEvent", sample: audit.Event{}},
	{name: "AuditActor", sample: audit.Actor{}},
	{name: "Tenant", sample: tenancy.Tenant{}},
	{name: "APIKey", sample: tenancy.APIKey{}},
	{name: "NewKeyResult", sample: tenancy.NewAPIKeyResult{}},
	{name: "QuotaConfig", sample: quota.Config{}},

	// GET /logs/{id} returns the stored row directly, without the derived cost
	// fields the LISTING adds — so this is a different shape from
	// InteractionLog, and documenting them as one would be wrong in the other
	// direction.
	{name: "InteractionDetail", sample: storage.InteractionLog{}},

	// The YAML documents, as they are marshalled back over the wire by
	// GET /agents/{name} and GET /pipelines/{name}. The authority for what is
	// VALID remains schema/mockagents-v1-agent.json; this only checks that the
	// OpenAPI document describes the envelope the API actually returns.
	{name: "AgentDefinition", sample: types.AgentDefinition{}},
	{name: "Pipeline", sample: types.PipelineDefinition{}},

	// Provider wire formats. These mirror OpenAI and Anthropic rather than
	// being ours to define, which makes them MORE worth checking, not less: the
	// whole product is the claim that a client cannot tell the difference, and
	// the spec is where someone looks to see what we accept.
	{
		name:   "OpenAIChatCompletionRequest",
		sample: adapter.ChatCompletionRequest{},
		specOnly: map[string]string{
			"top_p":             "accepted for wire compatibility and ignored — this mock does not sample",
			"n":                 "accepted for wire compatibility and ignored — this mock does not sample",
			"stop":              "accepted for wire compatibility and ignored — this mock does not sample",
			"user":              "accepted for wire compatibility and ignored — this mock does not sample",
			"frequency_penalty": "accepted for wire compatibility and ignored — this mock does not sample",
			"presence_penalty":  "accepted for wire compatibility and ignored — this mock does not sample",
		},
	},
	{
		name:     "OpenAIChatMessage",
		sample:   adapter.OpenAIMessage{},
		specOnly: map[string]string{"name": "accepted for wire compatibility and ignored — this mock does not sample"},
	},
	{name: "OpenAITool", sample: adapter.OpenAITool{}},
	{name: "OpenAIToolCall", sample: adapter.OpenAIToolCall{}},
	{name: "OpenAIChatCompletionResponse", sample: adapter.ChatCompletionResponse{}},
	{name: "OpenAIUsage", sample: adapter.OpenAIUsage{}},
	{name: "OpenAIChatCompletionChunk", sample: streaming.ChatCompletionChunk{}},
	{
		name:   "AnthropicMessagesRequest",
		sample: adapter.AnthropicRequest{},
		specOnly: map[string]string{
			"temperature":    "accepted for wire compatibility and ignored — this mock does not sample",
			"top_p":          "accepted for wire compatibility and ignored — this mock does not sample",
			"top_k":          "accepted for wire compatibility and ignored — this mock does not sample",
			"stop_sequences": "accepted for wire compatibility and ignored — this mock does not sample",
			"metadata":       "accepted for wire compatibility and ignored — this mock does not sample",
		},
	},
	{name: "AnthropicMessage", sample: adapter.AnthropicMessage{}},
	{name: "AnthropicTool", sample: adapter.AnthropicTool{}},
	{name: "AnthropicMessagesResponse", sample: adapter.AnthropicResponse{}},
	{name: "AnthropicContentBlock", sample: adapter.AnthropicContent{}},
	{name: "AnthropicUsage", sample: adapter.AnthropicUsage{}},
}

// documentationOnly names schemas with no single Go wire type behind them, and
// says why. These are not silently skipped: leaving one out of both lists fails
// the check, so "we forgot" and "there is nothing to compare" stay distinct.
var documentationOnly = map[string]string{
	"AnthropicRequestContentBlock": "request-side blocks are accepted as free-form; AnthropicMessage.Content is `any`",
	"HealthResponse":               "assembled inline in the handler from a map literal",
	"ReloadResponse":               "assembled inline in the handler",
	"Error":                        "shared error envelope, written inline by writeJSON callers",
	"AnthropicStreamEvent":         "a union over six event types, each an unexported struct in internal/streaming — no single Go type to compare against",
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
		documented, err := schemaProperties(node, schemas)
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

// schemaProperties returns the property names a schema declares, following
// allOf composition.
//
// allOf is how OpenAPI says "these fields plus those" — the natural way to
// describe two endpoints that return the same row with and without a derived
// annotation. Without following it, the checker would force every such schema
// to duplicate its base fields, and a checker that pushes a document toward a
// worse shape is not worth having.
func schemaProperties(node any, all map[string]any) (map[string]bool, error) {
	out := map[string]bool{}
	if err := collectSchemaProperties(node, all, out, 0); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("declares no properties")
	}
	return out, nil
}

// maxSchemaDepth bounds allOf/$ref following. A spec that nests this deep is
// either generated or cyclic, and either way the checker should say so rather
// than recurse forever.
const maxSchemaDepth = 8

func collectSchemaProperties(node any, all map[string]any, out map[string]bool, depth int) error {
	if depth > maxSchemaDepth {
		return fmt.Errorf("allOf/$ref nesting deeper than %d — cyclic?", maxSchemaDepth)
	}
	obj, ok := node.(map[string]any)
	if !ok {
		return fmt.Errorf("not an object")
	}

	if ref, ok := obj["$ref"].(string); ok {
		name, found := strings.CutPrefix(ref, "#/components/schemas/")
		if !found {
			return fmt.Errorf("cannot follow non-local $ref %q", ref)
		}
		target, ok := all[name]
		if !ok {
			return fmt.Errorf("$ref %q does not resolve", ref)
		}
		return collectSchemaProperties(target, all, out, depth+1)
	}

	if props, ok := obj["properties"].(map[string]any); ok {
		for name := range props {
			out[name] = true
		}
	}
	if composed, ok := obj["allOf"].([]any); ok {
		for _, part := range composed {
			if err := collectSchemaProperties(part, all, out, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
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

package adapter

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
)

// --- response_format wire types ---

// ResponseFormat is the OpenAI `response_format` request block. Type is one of
// "text" (default no-op), "json_object" (legacy JSON mode), or "json_schema"
// (structured outputs).
type ResponseFormat struct {
	Type       string              `json:"type"`
	JSONSchema *ResponseFormatJSON `json:"json_schema,omitempty"`
}

// ResponseFormatJSON is the `json_schema` sub-object of a response_format.
type ResponseFormatJSON struct {
	Name   string         `json:"name,omitempty"`
	Schema map[string]any `json:"schema,omitempty"`
	Strict *bool          `json:"strict,omitempty"`
}

// Synthesis bounds. JSON Schemas in strict mode use $ref/$defs and can be
// recursive; maxSynthDepth terminates $ref cycles and pathological nesting,
// maxSynthArrayItems bounds output size against a hostile `minItems`, and
// maxSynthNodes bounds TOTAL work so a wide-and-recursive schema (every node
// $ref-ing back N times) cannot blow up exponentially before hitting the depth
// limit — the depth cap alone does not bound breadth.
const (
	maxSynthDepth      = 12
	maxSynthArrayItems = 5
	maxSynthNodes      = 5000
)

// synthCtx threads the $defs table and a shared work budget through the
// recursive walk. budget is decremented per visited node and, once exhausted,
// every further node short-circuits to a non-recursing zero value — bounding
// total synthesis cost regardless of schema breadth or recursion.
type synthCtx struct {
	defs   map[string]any
	budget int
}

// applyResponseFormat enforces an OpenAI response_format on the engine result
// in place, so both the streaming and non-streaming paths see the adjusted
// content. It is a no-op for the default/"text" format and for tool-call or
// refusal responses (those take precedence over structured content).
func applyResponseFormat(rf *ResponseFormat, resp *engine.Response) {
	if rf == nil {
		return
	}
	switch rf.Type {
	case "", "text":
		return

	case "json_object":
		if len(resp.ToolCalls) > 0 {
			return
		}
		if resp.Refusal != "" {
			refuse(resp)
			return
		}
		// Legacy JSON mode only guarantees a valid JSON object.
		if !isJSONObject(resp.Content) {
			resp.Content = "{}"
		}

	case "json_schema":
		if len(resp.ToolCalls) > 0 {
			return
		}
		// A planted refusal wins: it carries content:null + a content_filter
		// finish reason (mirroring the real API), so clear any canned content
		// the scenario also set.
		if resp.Refusal != "" {
			refuse(resp)
			return
		}
		var schema map[string]any
		if rf.JSONSchema != nil {
			schema = rf.JSONSchema.Schema
		}
		// Trust author-supplied content only if it already matches the schema's
		// root type (an object schema needs a JSON object, not a bare scalar or
		// a fields-missing literal); otherwise synthesize a conforming instance.
		if contentMatchesRoot(resp.Content, schema) {
			return
		}
		resp.Content = SynthesizeFromSchema(schema)
	}
}

// refuse normalizes a refusal to the OpenAI wire shape: content null +
// content_filter finish reason, with only message.refusal populated.
func refuse(resp *engine.Response) {
	resp.Content = ""
	if resp.FinishReason == "" {
		resp.FinishReason = "content_filter"
	}
}

// contentMatchesRoot reports whether s is valid JSON whose top-level shape
// matches the schema's declared root type (object/array). An unknown root type
// accepts any valid JSON.
func contentMatchesRoot(s string, schema map[string]any) bool {
	if !IsValidJSON(s) {
		return false
	}
	switch typeOf(schema) {
	case "object":
		return isJSONObject(s)
	case "array":
		return isJSONArray(s)
	default:
		return true
	}
}

// isJSONArray reports whether s is a well-formed JSON array (`[...]`).
func isJSONArray(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "[") && json.Valid([]byte(s))
}

// IsValidJSON reports whether s is a non-empty, well-formed JSON value.
func IsValidJSON(s string) bool {
	return s != "" && json.Valid([]byte(s))
}

// isJSONObject reports whether s is a well-formed JSON object (`{...}`).
func isJSONObject(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") && json.Valid([]byte(s))
}

// --- JSON Schema -> minimal conforming instance ---

// SynthesizeFromSchema returns a JSON string that conforms to the given JSON
// Schema. It produces a MINIMAL valid instance (empty strings, zero numbers,
// single-element arrays), not a realistic one — enough for an SDK's
// `.parse()` (Pydantic / Zod) to round-trip. A nil/empty schema yields "{}".
func SynthesizeFromSchema(schema map[string]any) string {
	if len(schema) == 0 {
		return "{}"
	}
	ctx := &synthCtx{defs: extractDefs(schema), budget: maxSynthNodes}
	b, err := json.Marshal(ctx.synth(schema, 0))
	if err != nil {
		return "{}"
	}
	return string(b)
}

// extractDefs pulls the root-level shared sub-schema table ($defs or the older
// definitions) so $ref targets can be resolved during the walk.
func extractDefs(schema map[string]any) map[string]any {
	if d, ok := schema["$defs"].(map[string]any); ok {
		return d
	}
	if d, ok := schema["definitions"].(map[string]any); ok {
		return d
	}
	return map[string]any{}
}

// synth recursively synthesizes one value for a schema node. Both the depth
// limit (terminating $ref cycles and deep nesting) and the shared node budget
// (bounding total breadth*depth work) fall back to a non-recursing zero value.
func (c *synthCtx) synth(schema map[string]any, depth int) any {
	if schema == nil {
		return nil
	}
	c.budget--
	if c.budget <= 0 || depth >= maxSynthDepth {
		return c.zeroForSchema(schema)
	}

	// const / enum win regardless of type.
	if cv, ok := schema["const"]; ok {
		return cv
	}
	if e, ok := schema["enum"].([]any); ok && len(e) > 0 {
		return e[0]
	}

	// $ref resolution.
	if ref, ok := schema["$ref"].(string); ok {
		if resolved := resolveRef(ref, c.defs); resolved != nil {
			return c.synth(resolved, depth+1)
		}
		return nil
	}

	// anyOf / oneOf — take the first branch.
	for _, key := range []string{"anyOf", "oneOf"} {
		if arr, ok := schema[key].([]any); ok && len(arr) > 0 {
			if first, ok := arr[0].(map[string]any); ok {
				return c.synth(first, depth+1)
			}
		}
	}
	// allOf — merge the properties of every branch (resolving $ref and nested
	// allOf), plus the node's own properties. This is the shape Pydantic emits
	// for an inherited/embedded model: allOf:[{$ref:Base},{properties:{...}}].
	if arr, ok := schema["allOf"].([]any); ok && len(arr) > 0 {
		props := map[string]any{}
		if p, ok := schema["properties"].(map[string]any); ok {
			for k, v := range p {
				props[k] = v
			}
		}
		c.collectAllOfProps(arr, props, depth)
		if len(props) > 0 {
			return c.synthObject(props, depth+1)
		}
		// No object properties anywhere — allOf constrains a non-object; fall
		// back to synthesizing from the first branch.
		if first, ok := arr[0].(map[string]any); ok {
			return c.synth(first, depth+1)
		}
		return nil
	}

	switch typeOf(schema) {
	case "object":
		props, _ := schema["properties"].(map[string]any)
		return c.synthObject(props, depth+1)
	case "array":
		return c.synthArray(schema, depth+1)
	case "string":
		return synthString(schema)
	case "integer":
		if v, ok := lowerBoundInt(schema); ok {
			return v
		}
		return int64(0)
	case "number":
		if v, ok := lowerBoundNum(schema); ok {
			return v
		}
		return float64(0)
	case "boolean":
		return false
	default: // "null" or unknown
		return nil
	}
}

// collectAllOfProps merges object properties from every allOf branch into dst,
// following $ref and nested allOf. depth-bounded against cyclic $ref.
func (c *synthCtx) collectAllOfProps(branches []any, dst map[string]any, depth int) {
	if depth > maxSynthDepth {
		return
	}
	for _, b := range branches {
		sub, ok := b.(map[string]any)
		if !ok {
			continue
		}
		if ref, ok := sub["$ref"].(string); ok {
			if r := resolveRef(ref, c.defs); r != nil {
				sub = r
			} else {
				continue
			}
		}
		if p, ok := sub["properties"].(map[string]any); ok {
			for k, v := range p {
				dst[k] = v
			}
		}
		if nested, ok := sub["allOf"].([]any); ok {
			c.collectAllOfProps(nested, dst, depth+1)
		}
	}
}

// lowerBoundInt returns a schema-satisfying integer: minimum (ceil) if present,
// else exclusiveMinimum+1. Non-finite bounds are ignored.
func lowerBoundInt(schema map[string]any) (int64, bool) {
	if m, ok := toFloat64(schema["minimum"]); ok && isFinite(m) {
		return clampMaxInt(int64(math.Ceil(m)), schema), true
	}
	if m, ok := toFloat64(schema["exclusiveMinimum"]); ok && isFinite(m) {
		return clampMaxInt(int64(math.Floor(m))+1, schema), true
	}
	return 0, false
}

// lowerBoundNum is lowerBoundInt's float counterpart (exclusiveMinimum+1 keeps
// the value strictly above the bound while staying simple/deterministic).
func lowerBoundNum(schema map[string]any) (float64, bool) {
	if m, ok := toFloat64(schema["minimum"]); ok && isFinite(m) {
		return m, true
	}
	if m, ok := toFloat64(schema["exclusiveMinimum"]); ok && isFinite(m) {
		return m + 1, true
	}
	return 0, false
}

// clampMaxInt keeps v at or below a finite maximum/exclusiveMaximum.
func clampMaxInt(v int64, schema map[string]any) int64 {
	if mx, ok := toFloat64(schema["maximum"]); ok && isFinite(mx) && float64(v) > mx {
		return int64(math.Floor(mx))
	}
	if mx, ok := toFloat64(schema["exclusiveMaximum"]); ok && isFinite(mx) && float64(v) >= mx {
		return int64(math.Ceil(mx)) - 1
	}
	return v
}

func isFinite(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) }

// synthObject emits one value for each declared property. Only declared
// properties are emitted, satisfying additionalProperties:false; json.Marshal
// sorts the keys, so the output bytes are deterministic. Property expansion
// stops early once the shared node budget is exhausted.
func (c *synthCtx) synthObject(props map[string]any, depth int) any {
	obj := make(map[string]any, len(props))
	for k, v := range props {
		if c.budget <= 0 {
			break
		}
		if vm, ok := v.(map[string]any); ok {
			obj[k] = c.synth(vm, depth)
		} else {
			obj[k] = nil
		}
	}
	return obj
}

func (c *synthCtx) synthArray(schema map[string]any, depth int) any {
	// Tuple forms: 2020-12 prefixItems, or draft-07 array-valued items. Emit
	// one value per positional sub-schema.
	if tuple, ok := tupleItems(schema); ok {
		arr := make([]any, 0, len(tuple))
		for _, p := range tuple {
			if c.budget <= 0 {
				break
			}
			if pm, ok := p.(map[string]any); ok {
				arr = append(arr, c.synth(pm, depth))
			} else {
				arr = append(arr, nil)
			}
		}
		return arr
	}

	items, _ := schema["items"].(map[string]any)
	if items == nil {
		return []any{}
	}
	n := 1
	if mi, ok := toFloat64(schema["minItems"]); ok && int(mi) > n {
		n = int(mi)
	}
	if n > maxSynthArrayItems {
		n = maxSynthArrayItems
	}
	arr := make([]any, 0, n)
	for i := 0; i < n; i++ {
		if c.budget <= 0 {
			break
		}
		arr = append(arr, c.synth(items, depth))
	}
	return arr
}

// tupleItems returns the positional sub-schemas of a tuple-typed array
// (prefixItems, or draft-07 array-valued items), if any.
func tupleItems(schema map[string]any) ([]any, bool) {
	if pi, ok := schema["prefixItems"].([]any); ok && len(pi) > 0 {
		return pi, true
	}
	if it, ok := schema["items"].([]any); ok && len(it) > 0 {
		return it, true
	}
	return nil, false
}

func synthString(schema map[string]any) string {
	if f, ok := schema["format"].(string); ok {
		switch f {
		case "date-time":
			return "2024-01-01T00:00:00Z"
		case "date":
			return "2024-01-01"
		case "time":
			return "00:00:00"
		case "email":
			return "user@example.com"
		case "uri", "url":
			return "https://example.com"
		case "uuid":
			return "00000000-0000-4000-8000-000000000000"
		}
	}
	return ""
}

// zeroForSchema returns a non-recursing zero value for a schema's type, used
// at the recursion-depth/budget limit so a deeply nested or cyclic schema still
// terminates with a TYPE-APPROPRIATE value. A $ref node is resolved one level
// first, so a recursive schema (linked list / tree) terminates with {} / [] /
// "" matching the referenced type instead of a contract-violating null.
func (c *synthCtx) zeroForSchema(schema map[string]any) any {
	if ref, ok := schema["$ref"].(string); ok {
		if r := resolveRef(ref, c.defs); r != nil {
			schema = r
		}
	}
	switch typeOf(schema) {
	case "object":
		return map[string]any{}
	case "array":
		return []any{}
	case "string":
		return ""
	case "integer":
		return int64(0)
	case "number":
		return float64(0)
	case "boolean":
		return false
	default:
		return nil
	}
}

// typeOf resolves a schema's type. `type` may be a string or an array like
// ["string","null"] (nullable) — the first non-null entry is used. When absent,
// the presence of properties/items infers object/array.
func typeOf(schema map[string]any) string {
	switch t := schema["type"].(type) {
	case string:
		return t
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && s != "null" {
				return s
			}
		}
		return "null"
	}
	if _, ok := schema["properties"]; ok {
		return "object"
	}
	if _, ok := schema["items"]; ok {
		return "array"
	}
	return ""
}

// resolveRef looks up a local $ref ("#/$defs/Name" or "#/definitions/Name")
// in the defs table.
func resolveRef(ref string, defs map[string]any) map[string]any {
	name := ref
	for _, p := range []string{"#/$defs/", "#/definitions/"} {
		if strings.HasPrefix(ref, p) {
			name = strings.TrimPrefix(ref, p)
			break
		}
	}
	if d, ok := defs[name].(map[string]any); ok {
		return d
	}
	return nil
}

// toFloat64 reads a JSON-decoded numeric schema value (float64 from
// encoding/json, or int when a schema is built in Go for tests).
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

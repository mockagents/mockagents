package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// loadAgentSchema parses schema/mockagents-v1-agent.json into a generic tree.
func loadAgentSchema(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "mockagents-v1-agent.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return doc
}

// walk descends a JSON tree by keys, failing the test if a key is missing.
func walk(t *testing.T, node any, keys ...string) any {
	t.Helper()
	for _, k := range keys {
		m, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("schema: expected object at %q, got %T", k, node)
		}
		node, ok = m[k]
		if !ok {
			t.Fatalf("schema: missing key %q", k)
		}
	}
	return node
}

func enumOf(t *testing.T, node any) []string {
	t.Helper()
	raw, ok := walk(t, node, "enum").([]any)
	if !ok {
		t.Fatalf("schema: enum is not an array")
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

func numOf(t *testing.T, node any, key string) float64 {
	t.Helper()
	v, ok := walk(t, node, key).(float64)
	if !ok {
		t.Fatalf("schema: %q is not a number", key)
	}
	return v
}

// TestSchemaParity_ChaosAndStrictTools asserts the JSON schema and the Go
// validator agree on every enum and numeric bound in the chaos block and the
// strict-tools level. The schema is documentation for editors; the Go
// validator is what actually runs — so they must not drift (audit M-01 found
// the schema declaring 400..599 for status_code while the validator enforced
// nothing).
func TestSchemaParity_ChaosAndStrictTools(t *testing.T) {
	doc := loadAgentSchema(t)
	chaos := walk(t, doc, "$defs", "ChaosConfig", "properties")

	if got := enumOf(t, walk(t, chaos, "preset")); !reflect.DeepEqual(got, types.ChaosPresets) {
		t.Errorf("chaos.preset enum = %v, want types.ChaosPresets %v", got, types.ChaosPresets)
	}
	if got := enumOf(t, walk(t, chaos, "latency", "properties", "distribution")); !reflect.DeepEqual(got, latencyDistributions) {
		t.Errorf("latency.distribution enum = %v, want %v", got, latencyDistributions)
	}
	// connectionModeNames is sorted for its error message; the schema lists
	// modes in documentation order, so compare as sets.
	if got, want := sorted(enumOf(t, walk(t, chaos, "connection", "properties", "mode"))), sorted(connectionModeNames()); !reflect.DeepEqual(got, want) {
		t.Errorf("connection.mode enum = %v, want %v", got, want)
	}

	errs := walk(t, chaos, "errors", "properties")
	if lo, hi := numOf(t, walk(t, errs, "status_code"), "minimum"), numOf(t, walk(t, errs, "status_code"), "maximum"); lo != 400 || hi != 599 {
		t.Errorf("status_code bounds = [%v, %v], validator enforces [400, 599]", lo, hi)
	}
	if lo, hi := numOf(t, walk(t, errs, "status_codes", "items"), "minimum"), numOf(t, walk(t, errs, "status_codes", "items"), "maximum"); lo != 400 || hi != 599 {
		t.Errorf("status_codes item bounds = [%v, %v], validator enforces [400, 599]", lo, hi)
	}
	if lo, hi := numOf(t, walk(t, errs, "rate"), "minimum"), numOf(t, walk(t, errs, "rate"), "maximum"); lo != 0 || hi != 1 {
		t.Errorf("errors.rate bounds = [%v, %v], validator enforces [0, 1]", lo, hi)
	}
	if lo := numOf(t, walk(t, errs, "timeout_ms"), "minimum"); lo != 0 {
		t.Errorf("timeout_ms minimum = %v, want 0", lo)
	}
	if hi, ok := walk(t, errs, "timeout_ms").(map[string]any)["maximum"]; ok && hi.(float64) != maxChaosMs {
		t.Errorf("timeout_ms maximum = %v, validator enforces %d", hi, maxChaosMs)
	}
	for _, f := range []string{"min_ms", "max_ms", "mean_ms", "stddev_ms"} {
		if lo := numOf(t, walk(t, chaos, "latency", "properties", f), "minimum"); lo != 0 {
			t.Errorf("latency.%s minimum = %v, want 0", f, lo)
		}
	}
	rl := walk(t, chaos, "rate_limit", "properties")
	if lo := numOf(t, walk(t, rl, "requests"), "minimum"); lo != 1 {
		t.Errorf("rate_limit.requests minimum = %v, validator enforces 1", lo)
	}
	if lo := numOf(t, walk(t, rl, "window_ms"), "minimum"); lo != 1 {
		t.Errorf("rate_limit.window_ms minimum = %v, validator enforces 1", lo)
	}

	if got := enumOf(t, walk(t, doc, "$defs", "StrictToolsConfig", "properties", "level")); !reflect.DeepEqual(got, types.StrictToolLevels) {
		t.Errorf("strict_tools.level enum = %v, want types.StrictToolLevels %v", got, types.StrictToolLevels)
	}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

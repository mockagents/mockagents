package toolschema

import (
	"strings"
	"testing"
)

// The OpenAI structured-outputs subset (round-11 R9-16b): every object needs
// additionalProperties:false and required covering every property key.
func TestValidateStrictSubset(t *testing.T) {
	valid := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"city", "days"},
		"properties": map[string]any{
			"city": map[string]any{"type": "string"},
			"days": map[string]any{"type": []any{"integer", "null"}},
		},
	}
	if errs := ValidateStrictSubset(valid); len(errs) != 0 {
		t.Errorf("valid strict schema rejected: %v", errs)
	}

	missingAddl := map[string]any{
		"type":     "object",
		"required": []any{"city"},
		"properties": map[string]any{
			"city": map[string]any{"type": "string"},
		},
	}
	errs := ValidateStrictSubset(missingAddl)
	if len(errs) == 0 || !strings.Contains(errs[0], "In context=(), 'additionalProperties' is required to be supplied and to be false.") {
		t.Errorf("missing additionalProperties: got %v", errs)
	}

	missingRequired := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"city"},
		"properties": map[string]any{
			"city": map[string]any{"type": "string"},
			"days": map[string]any{"type": "integer"},
		},
	}
	errs = ValidateStrictSubset(missingRequired)
	if len(errs) == 0 || !strings.Contains(errs[0], "Missing 'days'") {
		t.Errorf("required not covering all keys: got %v", errs)
	}

	// Nested object violations carry the tuple-style context path.
	nested := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"body"},
		"properties": map[string]any{
			"body": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required": []any{"text"},
			},
		},
	}
	errs = ValidateStrictSubset(nested)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "In context=('properties', 'body'), 'additionalProperties'") {
			found = true
		}
	}
	if !found {
		t.Errorf("nested violation missing context path: %v", errs)
	}

	// Array items recurse.
	arr := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"tags"},
		"properties": map[string]any{
			"tags": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"k": map[string]any{"type": "string"}},
					"required":   []any{"k"},
				},
			},
		},
	}
	if errs := ValidateStrictSubset(arr); len(errs) == 0 {
		t.Error("array items object missing additionalProperties not caught")
	}

	// Empty/absent schema: nothing to validate.
	if errs := ValidateStrictSubset(nil); len(errs) != 0 {
		t.Errorf("nil schema produced errors: %v", errs)
	}
}

package toolschema

import (
	"fmt"
	"strings"
)

// ValidateStrictSubset checks a function-parameters schema against OpenAI's
// structured-outputs (`strict: true`) subset, which the real API enforces AT
// REQUEST TIME (round-11 R9-16b): every object must set
// `additionalProperties: false`, and `required` must list every key in
// `properties` (optionality is expressed via union types like
// ["string","null"], not by omitting the key from required). Object
// properties and array `items` are checked recursively; `anyOf` branches
// recurse too. Error strings mirror the real API's
// "In context=('properties', 'x'), …" format so the wrapped
// "Invalid schema for function 'name': …" message is byte-faithful.
func ValidateStrictSubset(schema map[string]any) []string {
	var errs []string
	validateStrictAt(schema, nil, &errs)
	return errs
}

func validateStrictAt(schema map[string]any, path []string, errs *[]string) {
	if schema == nil {
		return
	}
	ctx := strictContext(path)

	if t, _ := schema["type"].(string); t == "object" || (t == "" && schema["properties"] != nil) {
		// additionalProperties must be supplied and be exactly false.
		if b, ok := schema["additionalProperties"].(bool); !ok || b {
			*errs = append(*errs, fmt.Sprintf("In context=%s, 'additionalProperties' is required to be supplied and to be false.", ctx))
		}
		// required must include every key in properties.
		requiredSet := map[string]bool{}
		if reqList, ok := schema["required"].([]any); ok {
			for _, r := range reqList {
				if s, ok := r.(string); ok {
					requiredSet[s] = true
				}
			}
		}
		if props, ok := schema["properties"].(map[string]any); ok {
			for name, sub := range props {
				if !requiredSet[name] {
					*errs = append(*errs, fmt.Sprintf("In context=%s, 'required' is required to be supplied and to be an array including every key in properties. Missing '%s'.", ctx, name))
				}
				if subSchema, ok := sub.(map[string]any); ok {
					validateStrictAt(subSchema, append(path, "properties", name), errs)
				}
			}
		}
	}

	if items, ok := schema["items"].(map[string]any); ok {
		validateStrictAt(items, append(path, "items"), errs)
	}
	if branches, ok := schema["anyOf"].([]any); ok {
		for i, b := range branches {
			if sub, ok := b.(map[string]any); ok {
				validateStrictAt(sub, append(path, "anyOf", fmt.Sprint(i)), errs)
			}
		}
	}
}

// strictContext renders the real API's tuple-style path: "()" at the root,
// "('properties', 'body')" nested.
func strictContext(path []string) string {
	if len(path) == 0 {
		return "()"
	}
	quoted := make([]string, len(path))
	for i, p := range path {
		quoted[i] = "'" + p + "'"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

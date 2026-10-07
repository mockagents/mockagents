package config

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// Strict field checking.
//
// The typed decode silently discards any key it does not recognise. For an
// authoring tool that is the worst possible failure mode: a misspelled
// `arguments:` in a TestSuite left the assertion matching anything, a
// misspelled `assertions:` left a case with no assertions at all, and a
// misspelled `streaming:` or `errors:` turned the feature off — and every one
// of them validated clean and reported PASS. The JSON schemas already declare
// additionalProperties:false; this enforces the same contract on the Go side,
// with the real line number from the node tree.
//
// The check walks the yaml.Node tree against the Go type the document decodes
// into, so it needs no second decode and works on the same node the
// validators already receive. Free-form fields (map[string]any, interfaces,
// types with a custom UnmarshalYAML) accept any key, as they do at runtime.

// unknownFieldErrors reports every mapping key in node that the Go type of
// target does not declare. target is a pointer to (or value of) the type the
// document decodes into. A nil node reports nothing.
func unknownFieldErrors(file string, node *yaml.Node, target any) []*ValidationError {
	if node == nil || target == nil {
		return nil
	}
	var errs []*ValidationError
	walkUnknownFields(file, node, reflect.TypeOf(target), "", &errs)
	return errs
}

var yamlUnmarshalerType = reflect.TypeOf((*yaml.Unmarshaler)(nil)).Elem()

func walkUnknownFields(file string, node *yaml.Node, t reflect.Type, path string, errs *[]*ValidationError) {
	if node == nil || t == nil {
		return
	}
	for node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	if node.Kind == yaml.DocumentNode {
		for _, c := range node.Content {
			walkUnknownFields(file, c, t, path, errs)
		}
		return
	}
	for t.Kind() == reflect.Pointer {
		if t.Implements(yamlUnmarshalerType) {
			return
		}
		t = t.Elem()
	}
	if t.Implements(yamlUnmarshalerType) || reflect.PointerTo(t).Implements(yamlUnmarshalerType) {
		return // custom decoding owns its own shape
	}

	switch t.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return // a type mismatch is the decoder's error to report
		}
		fields := yamlFieldsOf(t)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, val := node.Content[i], node.Content[i+1]
			name := key.Value
			if name == "<<" { // merge key: check the merged mapping instead
				walkUnknownFields(file, val, t, path, errs)
				continue
			}
			ft, ok := fields[name]
			if !ok {
				*errs = append(*errs, unknownFieldError(file, key, joinPath(path, name), fields))
				continue
			}
			walkUnknownFields(file, val, ft, joinPath(path, name), errs)
		}
	case reflect.Map:
		if node.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			walkUnknownFields(file, node.Content[i+1], t.Elem(), joinPath(path, node.Content[i].Value), errs)
		}
	case reflect.Slice, reflect.Array:
		if node.Kind != yaml.SequenceNode {
			return
		}
		for i, c := range node.Content {
			walkUnknownFields(file, c, t.Elem(), fmt.Sprintf("%s[%d]", path, i), errs)
		}
	}
	// Interfaces and scalars accept whatever the decoder accepts.
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func unknownFieldError(file string, key *yaml.Node, path string, fields map[string]reflect.Type) *ValidationError {
	known := make([]string, 0, len(fields))
	for k := range fields {
		known = append(known, k)
	}
	sort.Strings(known)
	suggestion := "Remove the field, or check its spelling against the schema."
	if best := closestName(key.Value, known); best != "" {
		suggestion = fmt.Sprintf("Did you mean %q?", best)
	} else if len(known) > 0 && len(known) <= 12 {
		suggestion = "Supported fields here: " + strings.Join(known, ", ") + "."
	}
	return &ValidationError{
		File:       file,
		Line:       key.Line,
		Column:     key.Column,
		Field:      path,
		Message:    fmt.Sprintf("unknown field %q", key.Value),
		Suggestion: suggestion,
	}
}

var yamlFieldCache sync.Map // reflect.Type -> map[string]reflect.Type

// yamlFieldsOf returns the YAML key → field type map for a struct type,
// following yaml.v3's naming rules: the tag name, else the lower-cased field
// name; `-` excluded; `,inline` structs flattened into the parent.
func yamlFieldsOf(t reflect.Type) map[string]reflect.Type {
	if cached, ok := yamlFieldCache.Load(t); ok {
		return cached.(map[string]reflect.Type)
	}
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue // unexported
		}
		tag := f.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		inline := strings.Contains(","+opts+",", ",inline,")
		if inline {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k, v := range yamlFieldsOf(ft) {
					fields[k] = v
				}
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		fields[name] = f.Type
	}
	yamlFieldCache.Store(t, fields)
	return fields
}

// closestName returns the candidate within edit distance 2 of name (or a
// case-insensitive match), preferring the smallest distance; "" if none.
func closestName(name string, candidates []string) string {
	best, bestDist := "", 3
	for _, c := range candidates {
		if strings.EqualFold(c, name) {
			return c
		}
		if d := editDistance(strings.ToLower(name), strings.ToLower(c)); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// UnknownAgentFields parses an Agent document (YAML or JSON) and reports every
// key the Agent type does not declare. Write paths call it on the body exactly
// as the client sent it: they re-marshal the typed definition before running
// ValidateBytes, and by then an unknown key has already been dropped. A body
// that does not parse reports nothing here, so the caller's ordinary decode
// can report the syntax error in its usual shape.
func UnknownAgentFields(data []byte) []*ValidationError {
	if looksLikeJSON(data) {
		converted, err := jsonToYAML(data)
		if err != nil {
			return nil
		}
		data = converted
	}
	doc, err := parseSingleDocument(data)
	if err != nil {
		var multi errMultipleDocuments
		if errors.As(err, &multi) {
			return []*ValidationError{{Line: multi.line, Field: "document", Message: multi.Error()}}
		}
		return nil
	}
	return unknownFieldErrors("", doc, &types.AgentDefinition{})
}

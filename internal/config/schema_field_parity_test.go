package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// Field parity between every published JSON schema and the Go type the loader
// decodes into (2026-10-06 review C-20). The validator now rejects unknown
// keys, and the schemas declare additionalProperties: false, so a field the Go
// type has but the schema lacks makes editors flag valid configuration, and a
// field the schema has but Go lacks is now a validation error for anyone who
// trusts the schema. Only the two generated schemas were checked before.
func TestSchemaFieldParity_EveryKind(t *testing.T) {
	kinds := []struct {
		file   string
		goType any
	}{
		{"mockagents-v1-agent.json", types.AgentDefinition{}},
		{"mockagents-v1-pipeline.json", types.PipelineDefinition{}},
		{"mockagents-v1-testsuite.json", types.TestSuiteDefinition{}},
		{"mockagents-v1-mcpserver.json", types.MCPServerDefinition{}},
		{"mockagents-v1-a2aserver.json", types.A2AServerDefinition{}},
		{"mockagents-v1-vectorcollection.json", types.VectorCollectionDefinition{}},
		{"mockagents-v1-searchservice.json", types.SearchServiceDefinition{}},
	}
	for _, k := range kinds {
		t.Run(k.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "schema", k.file))
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]any
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatal(err)
			}
			p := &parityWalker{root: root, seen: map[string]bool{}}
			p.compare(root, reflect.TypeOf(k.goType), "")
			sort.Strings(p.problems)
			for _, problem := range p.problems {
				t.Error(problem)
			}
		})
	}
}

type parityWalker struct {
	root     map[string]any
	problems []string
	seen     map[string]bool
}

// resolve follows a local "#/$defs/..." reference.
func (p *parityWalker) resolve(node map[string]any) map[string]any {
	for i := 0; i < 16; i++ {
		ref, ok := node["$ref"].(string)
		if !ok || !strings.HasPrefix(ref, "#/") {
			return node
		}
		var cur any = p.root
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			m, ok := cur.(map[string]any)
			if !ok {
				return node
			}
			cur = m[part]
		}
		next, ok := cur.(map[string]any)
		if !ok {
			return node
		}
		node = next
	}
	return node
}

// properties collects an object schema's declared properties, including those
// of allOf/anyOf/oneOf branches.
func (p *parityWalker) properties(node map[string]any) (map[string]any, bool) {
	node = p.resolve(node)
	props := map[string]any{}
	found := false
	if m, ok := node["properties"].(map[string]any); ok {
		found = true
		for k, v := range m {
			props[k] = v
		}
	}
	for _, combo := range []string{"allOf", "anyOf", "oneOf"} {
		branches, _ := node[combo].([]any)
		for _, b := range branches {
			if bm, ok := b.(map[string]any); ok {
				if sub, ok := p.properties(bm); ok {
					found = true
					for k, v := range sub {
						props[k] = v
					}
				}
			}
		}
	}
	return props, found
}

func (p *parityWalker) compare(schema map[string]any, t reflect.Type, path string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	schema = p.resolve(schema)
	switch t.Kind() {
	case reflect.Struct:
		key := t.String() + "@" + path
		if p.seen[key] {
			return
		}
		p.seen[key] = true
		props, ok := p.properties(schema)
		if !ok {
			return // free-form in the schema; nothing to compare
		}
		fields := yamlFieldsOf(t)
		for name := range fields {
			if _, ok := props[name]; !ok {
				p.problems = append(p.problems, "Go field missing from schema: "+joinPath(path, name))
			}
		}
		for name, sub := range props {
			ft, ok := fields[name]
			if !ok {
				p.problems = append(p.problems, "schema property the Go type does not have: "+joinPath(path, name))
				continue
			}
			if sm, ok := sub.(map[string]any); ok {
				p.compare(sm, ft, joinPath(path, name))
			}
		}
	case reflect.Slice, reflect.Array:
		if items, ok := schema["items"].(map[string]any); ok {
			p.compare(items, t.Elem(), path+"[]")
		}
	case reflect.Map:
		if extra, ok := schema["additionalProperties"].(map[string]any); ok {
			p.compare(extra, t.Elem(), path+"{}")
		}
	}
}

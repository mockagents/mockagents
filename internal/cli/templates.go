package cli

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// templatesFS holds the embedded starter packs surfaced by
// `mockagents init --template <name>`. Each pack lives under
// templates/<name>/ with agents/*.yaml and tests/*.yaml subtrees.
//
//go:embed all:templates
var templatesFS embed.FS

// DefaultTemplate is used when `init` is run without --template.
const DefaultTemplate = "basic"

// Template describes a named starter pack.
type Template struct {
	Name        string
	Description string
}

// templateOrder fixes the display order (and is the source of truth for which
// template names are valid). Descriptions double as the gallery one-liners.
var templateOrder = []Template{
	{"basic", "Minimal single agent: greeting, a tool call, and a default fallback."},
	{"customer-support", "First-line support flow: greeting, order lookup tool, refunds, escalation."},
	{"rag", "Retrieval agent with a grounded answer and an ungrounded hallucination fixture."},
	{"coding-agent", "Coding assistant with multi-step tool use (read a file, then edit it)."},
	{"planner", "Multi-step planner that decomposes a goal and runs one step at a time."},
}

// ListTemplates returns the available starter packs in display order.
func ListTemplates() []Template {
	out := make([]Template, len(templateOrder))
	copy(out, templateOrder)
	return out
}

// templateExists reports whether name is a known template.
func templateExists(name string) bool {
	for _, t := range templateOrder {
		if t.Name == name {
			return true
		}
	}
	return false
}

// templateFile is one file copied out of a template tree, with its path
// relative to the project root (e.g. "agents/support-agent.yaml").
type templateFile struct {
	RelPath string
	Content string
}

// loadTemplateFiles reads every file under templates/<name>/, returning them
// with project-relative paths (sorted for deterministic output).
func loadTemplateFiles(name string) ([]templateFile, error) {
	root := "templates/" + name
	var files []templateFile
	err := fs.WalkDir(templatesFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := templatesFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path, root), "/")
		files = append(files, templateFile{RelPath: rel, Content: string(data)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading template %q: %w", name, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	return files, nil
}

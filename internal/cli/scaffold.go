package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScaffoldOptions configures the init scaffolding behavior.
type ScaffoldOptions struct {
	ProjectName string
	TargetDir   string
	Force       bool
	// Template selects a starter pack (see ListTemplates). Empty = DefaultTemplate.
	Template string
}

// ScaffoldResult reports what a Scaffold call produced.
type ScaffoldResult struct {
	Dir      string
	Template string
	Agents   []string // project-relative paths, e.g. agents/support-agent.yaml
	Tests    []string // project-relative paths, e.g. tests/support-suite.yaml
}

// Scaffold creates a new MockAgents project directory from a starter template.
// Returns the path to the created project.
func Scaffold(opts ScaffoldOptions) (string, error) {
	res, err := ScaffoldWithResult(opts)
	if err != nil {
		return "", err
	}
	return res.Dir, nil
}

// ScaffoldWithResult is like Scaffold but reports the template and files written.
func ScaffoldWithResult(opts ScaffoldOptions) (*ScaffoldResult, error) {
	tmpl := opts.Template
	if tmpl == "" {
		tmpl = DefaultTemplate
	}
	if !templateExists(tmpl) {
		return nil, fmt.Errorf("unknown template %q (run `mockagents init --list-templates`)", tmpl)
	}

	projectDir := opts.TargetDir
	if projectDir == "" {
		projectDir = opts.ProjectName
	}

	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	// Refuse to scaffold over a path that exists but isn't a directory.
	if info, err := os.Stat(absDir); err == nil && !info.IsDir() {
		return nil, fmt.Errorf("%q exists and is not a directory", absDir)
	}

	// Initializing "in place" (the current working directory) is allowed even
	// when the directory is non-empty (e.g. an existing repo with .git/). We
	// only guard against clobbering the specific files the scaffold writes.
	inPlace := false
	if cwd, err := os.Getwd(); err == nil && filepath.Clean(cwd) == filepath.Clean(absDir) {
		inPlace = true
	}

	tmplFiles, err := loadTemplateFiles(tmpl)
	if err != nil {
		return nil, err
	}

	// Assemble the full file set: the template tree plus the generated
	// project config and README.
	files := make(map[string]string, len(tmplFiles)+2)
	var agents, tests []string
	for _, f := range tmplFiles {
		files[f.RelPath] = f.Content
		switch {
		case strings.HasPrefix(f.RelPath, "agents/"):
			agents = append(agents, f.RelPath)
		case strings.HasPrefix(f.RelPath, "tests/"):
			tests = append(tests, f.RelPath)
		}
	}
	sort.Strings(agents)
	sort.Strings(tests)
	files[".mockagents.yaml"] = projectConfig(opts.ProjectName)
	files["README.md"] = readmeTemplate(opts.ProjectName, tmpl, agents, tests)

	// Pre-flight: without --force, refuse to clobber any file the scaffold
	// would write. This lets `init` run inside a dir of unrelated files while
	// still protecting an existing project's managed files.
	if !opts.Force {
		conflicts := make([]string, 0)
		for rel := range files {
			if _, err := os.Stat(filepath.Join(absDir, rel)); err == nil {
				conflicts = append(conflicts, rel)
			}
		}
		if len(conflicts) > 0 {
			sort.Strings(conflicts)
			return nil, fmt.Errorf("file %q already exists (use --force to overwrite)", conflicts[0])
		}
	}

	// With --force on a named project directory, replace the scaffold-owned
	// agents/ and tests/ subtrees outright so re-scaffolding (e.g. switching
	// templates) does not leave stale files from a previous template. We skip
	// this when initializing in place, to never delete a user's own files.
	if opts.Force && !inPlace {
		for _, sub := range []string{"agents", "tests"} {
			if err := os.RemoveAll(filepath.Join(absDir, sub)); err != nil {
				return nil, fmt.Errorf("clearing %s/ for overwrite: %w", sub, err)
			}
		}
	}

	for rel, content := range files {
		full := filepath.Join(absDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return nil, fmt.Errorf("creating directory for %q: %w", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("writing %q: %w", rel, err)
		}
	}

	return &ScaffoldResult{Dir: absDir, Template: tmpl, Agents: agents, Tests: tests}, nil
}

func projectConfig(name string) string {
	_ = name
	return `# MockAgents project configuration
version: "1"
server:
  port: 8080
  host: "127.0.0.1"
agents_dir: "./agents"
logging:
  level: info
  format: text
`
}

func readmeTemplate(name, template string, agents, tests []string) string {
	title := name
	if title == "" || title == "." {
		title = "my-project"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "A MockAgents project scaffolded from the `%s` template.\n\n", template)

	b.WriteString("## Quick Start\n\n")
	b.WriteString("1. **Validate** your agent definitions:\n\n   ```bash\n   mockagents validate agents\n   ```\n\n")
	b.WriteString("2. **Start** the mock server:\n\n   ```bash\n   mockagents start --agents-dir agents\n   ```\n\n")
	b.WriteString("3. **Run the test suite** against it:\n\n   ```bash\n")
	for _, t := range tests {
		fmt.Fprintf(&b, "   mockagents test --agents-dir agents %s\n", t)
	}
	b.WriteString("   ```\n\n")
	b.WriteString("4. **Point your SDK** at `http://localhost:8080`:\n\n")
	b.WriteString("   ```python\n   import openai\n   client = openai.OpenAI(base_url=\"http://localhost:8080/v1\", api_key=\"mock\")\n")
	b.WriteString("   response = client.chat.completions.create(\n       model=\"gpt-4o\",\n       messages=[{\"role\": \"user\", \"content\": \"hello\"}],\n   )\n   print(response.choices[0].message.content)\n   ```\n\n")

	b.WriteString("## What's in this project\n\n")
	b.WriteString("Agents:\n\n")
	for _, a := range agents {
		fmt.Fprintf(&b, "- `%s`\n", a)
	}
	b.WriteString("\nTest suites:\n\n")
	for _, t := range tests {
		fmt.Fprintf(&b, "- `%s`\n", t)
	}
	b.WriteString("\n## Try another template\n\n```bash\nmockagents init --list-templates\nmockagents init my-next-project --template customer-support\n```\n\n")
	b.WriteString("## Learn More\n\n")
	b.WriteString("- [MockAgents Documentation](https://github.com/mockagents/mockagents)\n")
	b.WriteString("- [Scenario Packs gallery](https://github.com/mockagents/mockagents/blob/main/site/docs/guides/scenario-packs.md)\n")
	b.WriteString("- [Agent Definition Reference](https://github.com/mockagents/mockagents/blob/main/schema/mockagents-v1-agent.json)\n")
	return b.String()
}

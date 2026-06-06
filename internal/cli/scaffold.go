package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// ScaffoldOptions configures the init scaffolding behavior.
type ScaffoldOptions struct {
	ProjectName string
	TargetDir   string
	Force       bool
}

// Scaffold creates a new MockAgents project directory with starter files.
// Returns the path to the created project.
func Scaffold(opts ScaffoldOptions) (string, error) {
	projectDir := opts.TargetDir
	if projectDir == "" {
		projectDir = opts.ProjectName
	}

	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}

	// Check if directory exists and has contents.
	if info, err := os.Stat(absDir); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%q exists and is not a directory", absDir)
		}
		entries, _ := os.ReadDir(absDir)
		if len(entries) > 0 && !opts.Force {
			return "", fmt.Errorf("directory %q is not empty (use --force to overwrite)", absDir)
		}
	}

	// Create directory structure.
	dirs := []string{
		absDir,
		filepath.Join(absDir, "agents"),
		filepath.Join(absDir, "tests"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return "", fmt.Errorf("creating directory %q: %w", d, err)
		}
	}

	// Write template files.
	files := map[string]string{
		filepath.Join(absDir, ".mockagents.yaml"):              projectConfig(opts.ProjectName),
		filepath.Join(absDir, "agents", "example-agent.yaml"):  exampleAgentYAML,
		filepath.Join(absDir, "tests", "example-test.yaml"):    exampleTestYAML,
		filepath.Join(absDir, "README.md"):                     readmeTemplate(opts.ProjectName),
	}

	for path, content := range files {
		if !opts.Force {
			if _, err := os.Stat(path); err == nil {
				return "", fmt.Errorf("file %q already exists (use --force to overwrite)", path)
			}
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("writing %q: %w", path, err)
		}
	}

	return absDir, nil
}

func projectConfig(name string) string {
	return fmt.Sprintf(`# MockAgents project configuration
version: "1"
server:
  port: 8080
  host: "127.0.0.1"
agents_dir: "./agents"
logging:
  level: info
  format: text
`)
}

const exampleAgentYAML = `# Example mock agent definition
# Docs: https://github.com/mockagents/mockagents
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: example-agent
  description: A simple example mock agent
  tags: [example]

spec:
  protocol: openai-chat-completions
  model: gpt-4o
  systemPrompt: |
    You are a helpful assistant.

  tools:
    - name: get_info
      description: Retrieve information by key
      parameters:
        type: object
        properties:
          key:
            type: string
            description: The lookup key
        required: [key]
      responses:
        - match:
            key: "greeting"
          response:
            value: "Hello, world!"
        - default: true
          response:
            value: "No information found."

  behavior:
    scenarios:
      - name: greeting
        match:
          content_contains: "hello"
        response:
          content: "Hello! I'm your mock assistant. How can I help?"

      - name: info-request
        match:
          content_contains: "info"
        response:
          content: "Let me look that up for you."
          tool_calls:
            - name: get_info
              arguments:
                key: "greeting"

      - name: default
        response:
          content: "I'm a mock agent. Try saying 'hello' or asking for 'info'."

    streaming:
      enabled: true
      chunk_size: 4
      chunk_delay_ms: 50
`

const exampleTestYAML = `# Example test scenario
# Run with: mockagents test tests/example-test.yaml
apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: example-tests
  description: Basic tests for the example agent

tests:
  - name: greeting-test
    agent: example-agent
    steps:
      - send:
          content: "hello"
        expect:
          content_contains: "Hello"
          status: 200

  - name: default-response-test
    agent: example-agent
    steps:
      - send:
          content: "random message"
        expect:
          content_contains: "mock agent"
          status: 200
`

func readmeTemplate(name string) string {
	title := name
	if title == "" {
		title = "my-project"
	}
	return fmt.Sprintf(`# %s

A MockAgents project for testing AI agent integrations.

## Quick Start

1. **Validate** your agent definitions:

   `+"`"+`bash
   mockagents validate
   `+"`"+`

2. **Start** the mock server:

   `+"`"+`bash
   mockagents start
   `+"`"+`

3. **Test** with your application:

   Point your OpenAI or Anthropic SDK at `+"`"+`http://localhost:8080`+"`"+`:

   `+"`"+`python
   import openai
   client = openai.OpenAI(base_url="http://localhost:8080/v1", api_key="mock")
   response = client.chat.completions.create(
       model="gpt-4o",
       messages=[{"role": "user", "content": "hello"}]
   )
   print(response.choices[0].message.content)
   `+"`"+`

## Project Structure

`+"`"+``+"`"+``+"`"+`
%s/
├── .mockagents.yaml     # Project configuration
├── agents/              # Agent definitions
│   └── example-agent.yaml
├── tests/               # Test scenarios
│   └── example-test.yaml
└── README.md
`+"`"+``+"`"+``+"`"+`

## Learn More

- [MockAgents Documentation](https://github.com/mockagents/mockagents)
- [Agent Definition Reference](https://github.com/mockagents/mockagents/blob/main/schema/mockagents-v1-agent.json)
`, title, title)
}

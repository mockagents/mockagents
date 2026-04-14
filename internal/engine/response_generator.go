package engine

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// Response represents the engine's output for a processed request.
type Response struct {
	AgentName    string              `json:"agent_name"`
	Model        string              `json:"model"`
	Content      string              `json:"content"`
	ToolCalls    []types.ToolCallSpec `json:"tool_calls,omitempty"`
	ToolResults  []ToolCallResult    `json:"tool_results,omitempty"`
	ScenarioName string              `json:"scenario_name"`
	SystemPrompt string              `json:"system_prompt,omitempty"`
	Metadata     map[string]any      `json:"metadata,omitempty"`
}

// TemplateContext provides data available to Go templates in response content.
type TemplateContext struct {
	Agent      *types.AgentDefinition
	Message    string
	TurnNumber int
	SessionID  string
	Timestamp  string
	Vars       map[string]any // Session variables
	Match      map[string]string // Regex capture groups from scenario matching
}

// ResponseGenerator renders scenario responses into final Response objects.
type ResponseGenerator struct {
	funcMap template.FuncMap
	cache   sync.Map // template cache: string -> *template.Template
}

// NewResponseGenerator creates a ResponseGenerator with built-in template functions.
func NewResponseGenerator() *ResponseGenerator {
	g := &ResponseGenerator{}
	g.funcMap = template.FuncMap{
		// Time functions
		"now":         func() string { return time.Now().UTC().Format(time.RFC3339) },
		"timestamp":   func() string { return fmt.Sprintf("%d", time.Now().Unix()) },
		"date_offset": dateOffset,
		"date_format": func(layout string) string { return time.Now().UTC().Format(layout) },

		// Random functions
		"uuid":          generateUUID,
		"random_int":    randomInt,
		"random_float":  randomFloat,
		"random_string": randomString,
		"random_choice": randomChoice,

		// String functions
		"upper": strings.ToUpper,
		"lower": strings.ToLower,

		// Data functions
		"to_json": toJSON,

		// Fake data functions
		"fake_name":  fakeName,
		"fake_email": fakeEmail,
	}
	return g
}

// Generate renders a matched scenario into a Response.
func (g *ResponseGenerator) Generate(agent *types.AgentDefinition, scenario *types.Scenario, ctx TemplateContext) (*Response, error) {
	content, err := g.renderContent(scenario.Response.Content, ctx)
	if err != nil {
		return nil, fmt.Errorf("rendering scenario %q content: %w", scenario.Name, err)
	}

	return &Response{
		AgentName:    agent.Metadata.Name,
		Model:        agent.Spec.Model,
		Content:      content,
		ToolCalls:    scenario.Response.ToolCalls,
		ScenarioName: scenario.Name,
		SystemPrompt: agent.Spec.SystemPrompt,
		Metadata:     scenario.Response.Metadata,
	}, nil
}

// renderContent processes the content string. If it contains {{ }}, it uses
// Go text/template rendering with caching. Otherwise, returns the content as-is.
func (g *ResponseGenerator) renderContent(content string, ctx TemplateContext) (string, error) {
	if !strings.Contains(content, "{{") {
		return content, nil
	}

	// Check template cache.
	var tmpl *template.Template
	if cached, ok := g.cache.Load(content); ok {
		tmpl = cached.(*template.Template)
	} else {
		var err error
		tmpl, err = template.New("response").Funcs(g.funcMap).Parse(content)
		if err != nil {
			return "", fmt.Errorf("parsing template: %w", err)
		}
		g.cache.Store(content, tmpl)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return buf.String(), nil
}

func dateOffset(days int, unit string) string {
	var d time.Duration
	switch strings.ToLower(unit) {
	case "days", "day":
		d = time.Duration(days) * 24 * time.Hour
	case "hours", "hour":
		d = time.Duration(days) * time.Hour
	case "minutes", "minute":
		d = time.Duration(days) * time.Minute
	default:
		d = time.Duration(days) * 24 * time.Hour
	}
	return time.Now().UTC().Add(d).Format("2006-01-02")
}

func generateUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func randomInt(min, max int) int {
	if min >= max {
		return min
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return min
	}
	return min + int(n.Int64())
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			b[i] = 'x'
			continue
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

func randomFloat(min, max float64) float64 {
	if min >= max {
		return min
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return min
	}
	return min + (max-min)*float64(n.Int64())/1000000.0
}

func randomChoice(args ...any) any {
	if len(args) == 0 {
		return ""
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(args))))
	if err != nil {
		return args[0]
	}
	return args[n.Int64()]
}

func toJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

var fakeFirstNames = []string{
	"Alice", "Bob", "Carol", "David", "Emma", "Frank",
	"Grace", "Henry", "Iris", "Jack", "Karen", "Leo",
}

var fakeLastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Davis",
	"Miller", "Wilson", "Moore", "Taylor", "Anderson", "Thomas",
}

func fakeName() string {
	first := fakeFirstNames[randomInt(0, len(fakeFirstNames)-1)]
	last := fakeLastNames[randomInt(0, len(fakeLastNames)-1)]
	return first + " " + last
}

func fakeEmail() string {
	first := strings.ToLower(fakeFirstNames[randomInt(0, len(fakeFirstNames)-1)])
	last := strings.ToLower(fakeLastNames[randomInt(0, len(fakeLastNames)-1)])
	domains := []string{"example.com", "test.com", "mock.dev", "acme.org"}
	domain := domains[randomInt(0, len(domains)-1)]
	return fmt.Sprintf("%s.%s@%s", first, last, domain)
}

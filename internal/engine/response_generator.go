package engine

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mockagents/mockagents/internal/types"
)

// Response represents the engine's output for a processed request.
type Response struct {
	AgentName    string               `json:"agent_name"`
	Model        string               `json:"model"`
	Content      string               `json:"content"`
	ToolCalls    []types.ToolCallSpec `json:"tool_calls,omitempty"`
	ToolResults  []ToolCallResult     `json:"tool_results,omitempty"`
	ScenarioName string               `json:"scenario_name"`
	SystemPrompt string               `json:"system_prompt,omitempty"`
	Metadata     map[string]any       `json:"metadata,omitempty"`
}

// TemplateContext provides data available to Go templates in response content.
type TemplateContext struct {
	Agent      *types.AgentDefinition
	Message    string
	TurnNumber int
	SessionID  string
	Timestamp  string
	Vars       map[string]any    // Session variables
	Match      map[string]string // Regex capture groups from scenario matching
}

// renderBufPool recycles bytes.Buffer instances used during template
// execution. Templates are the hottest allocation site after scenario
// matching, and Execute writes a short string we immediately copy into
// a Go string, so the buffer lifetime is trivially bounded to one call.
var renderBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
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
		"title": title,

		// Data functions
		"to_json": toJSON,

		// Fake data functions
		"fake_name":     fakeName,
		"fake_email":    fakeEmail,
		"fake_phone":    fakePhone,
		"fake_company":  fakeCompany,
		"fake_username": fakeUsername,
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

	// Template cache (F-RG-001): keyed on the full content string and never
	// evicted. This is safe because `content` is always a scenario's
	// authored response body — a finite, static set loaded from YAML, never
	// per-request data — so the cache is bounded by the number of distinct
	// scenario templates. If a future caller ever passes content that
	// interpolates request data, the key space becomes unbounded and this
	// sync.Map must be swapped for a bounded/LRU cache.
	var tmpl *template.Template
	if cached, ok := g.cache.Load(content); ok {
		tmpl = cached.(*template.Template)
	} else {
		// Missing-key policy (F-RG-004): intentionally lenient — no
		// Option("missingkey=error"), so {{ .Vars.absent }} renders the
		// stdlib default "<no value>" instead of failing the turn. A mock
		// server favors fast author iteration over strict templates;
		// switching to strict would be a project-wide behavior change, not a
		// local tweak.
		var err error
		tmpl, err = template.New("response").Funcs(g.funcMap).Parse(content)
		if err != nil {
			return "", fmt.Errorf("parsing template: %w", err)
		}
		g.cache.Store(content, tmpl)
	}

	buf := renderBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer renderBufPool.Put(buf)
	if err := tmpl.Execute(buf, ctx); err != nil {
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
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	// Hand-roll the canonical 8-4-4-4-12 hex layout instead of fmt.Sprintf:
	// `{{ uuid }}` is the hottest template builtin, and Sprintf's reflect +
	// per-arg []byte boxing dominated its cost (PERF-17). hex.Encode writes
	// straight into the fixed buffer, so the only allocation is the result
	// string.
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}

// randomInt returns a uniformly random int in the INCLUSIVE range
// [min, max]. A degenerate min >= max returns min with no error or
// diagnostic (F-RG-008) — e.g. {{ random_int 10 5 }} renders 10 — so a
// mis-ordered template renders a stable value instead of panicking; swap
// the arguments to get a real range.
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

// maxRandomStringLen bounds the length a template author can request from
// {{ random_string n }} so a hostile or fat-fingered value (e.g. 1e9) can't
// allocate gigabytes on the response hot path. Non-positive lengths yield "".
const maxRandomStringLen = 4096

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if length <= 0 {
		return ""
	}
	if length > maxRandomStringLen {
		length = maxRandomStringLen
	}
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

// randomFloat returns a random float64 in the HALF-OPEN range [min, max),
// quantized to 10^6 evenly spaced steps (F-RG-003): the draw is
// min + (max-min)*k/1e6 for k in [0, 1e6), so max itself is never returned
// and results land on a 1e-6*(max-min) grid rather than the full float
// continuum. A degenerate min >= max returns min.
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

// toJSON marshals any value to a JSON string, falling back to fmt %v on a
// marshal error. It serializes whatever it is handed (F-RG-007), so
// {{ to_json . }} dumps the entire TemplateContext — including the agent's
// SystemPrompt and the session Vars. Templates are author-controlled, so
// this is a "pass explicit data" guideline rather than a sandbox boundary:
// prefer {{ to_json .Vars }} or a specific field over the whole context.
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

// title upper-cases the first letter of each whitespace-separated word,
// lower-casing the rest. A non-deprecated stand-in for strings.Title.
// Whitespace is normalized: runs of spaces collapse and leading/trailing
// space is trimmed (via strings.Fields). Rune-safe for non-ASCII input.
func title(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		r, sz := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToUpper(r)) + strings.ToLower(w[sz:])
	}
	return strings.Join(words, " ")
}

// fakePhone returns a fake US-style phone number using a reserved 555 exchange.
func fakePhone() string {
	return fmt.Sprintf("(%03d) 555-%04d", randomInt(200, 999), randomInt(0, 9999))
}

var fakeCompanyRoots = []string{
	"Acme", "Globex", "Initech", "Umbrella", "Hooli",
	"Soylent", "Stark", "Wayne", "Wonka", "Cyberdyne",
}

var fakeCompanySuffixes = []string{
	"Inc", "LLC", "Corp", "Group", "Labs", "Systems",
}

func fakeCompany() string {
	root := fakeCompanyRoots[randomInt(0, len(fakeCompanyRoots)-1)]
	suffix := fakeCompanySuffixes[randomInt(0, len(fakeCompanySuffixes)-1)]
	return root + " " + suffix
}

// fakeUsername returns a fake username like "alice_smith42".
func fakeUsername() string {
	first := strings.ToLower(fakeFirstNames[randomInt(0, len(fakeFirstNames)-1)])
	last := strings.ToLower(fakeLastNames[randomInt(0, len(fakeLastNames)-1)])
	return fmt.Sprintf("%s_%s%d", first, last, randomInt(0, 99))
}

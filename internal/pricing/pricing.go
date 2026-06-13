// Package pricing implements a lightweight cost estimation engine.
//
// It maps (model, prompt tokens, completion tokens) → estimated USD
// cost using a per-model price table. The default table ships
// conservative public list prices for the most-deployed OpenAI and
// Anthropic models at the time of writing. Operators can override the
// table at runtime via a YAML file pointed at by MOCKAGENTS_PRICING,
// which makes it trivial to keep the estimates in sync with whatever
// contract the customer actually negotiated.
//
// The package is deliberately import-cycle-free of internal/storage
// and internal/server: it only handles math and lookups. Call sites
// parse interaction logs themselves (see extract.go) and feed counts
// in.
package pricing

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Price models the per-token price for one named model. Prices are
// expressed per 1,000 tokens to match the units every provider uses
// in its public docs — avoiding micro-USD floats that round badly.
type Price struct {
	Model                 string  `yaml:"model" json:"model"`
	PromptPer1KUSD        float64 `yaml:"prompt_per_1k_usd" json:"prompt_per_1k_usd"`
	CompletionPer1KUSD    float64 `yaml:"completion_per_1k_usd" json:"completion_per_1k_usd"`
}

// Estimate returns the estimated USD cost of a (prompt, completion)
// token pair at this price point.
func (p Price) Estimate(promptTokens, completionTokens int) float64 {
	if promptTokens < 0 {
		promptTokens = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	return (float64(promptTokens)/1000.0)*p.PromptPer1KUSD +
		(float64(completionTokens)/1000.0)*p.CompletionPer1KUSD
}

// Table is a thread-safe map of model name → Price. Lookups are
// case-insensitive and support a Fallback price for unknown models
// so cost totals never silently drop data.
type Table struct {
	mu        sync.RWMutex
	prices    map[string]Price // lower-cased model name → Price
	Fallback  Price
}

// NewDefaultTable returns a Table seeded with built-in prices for
// common OpenAI and Anthropic models plus a zero-cost fallback so
// unknown models don't crash queries. Prices are deliberately
// round-number approximations of public list prices and should be
// overridden with MOCKAGENTS_PRICING in environments that care about
// accuracy.
func NewDefaultTable() *Table {
	t := &Table{
		prices: make(map[string]Price),
		Fallback: Price{
			Model:              "(unknown)",
			PromptPer1KUSD:     0,
			CompletionPer1KUSD: 0,
		},
	}
	for _, p := range defaultPrices {
		t.prices[strings.ToLower(p.Model)] = p
	}
	return t
}

// defaultPrices is the seed catalog. Kept small on purpose — users
// can extend via MOCKAGENTS_PRICING rather than editing this list.
var defaultPrices = []Price{
	// OpenAI
	{Model: "gpt-4o", PromptPer1KUSD: 0.0025, CompletionPer1KUSD: 0.010},
	{Model: "gpt-4o-mini", PromptPer1KUSD: 0.00015, CompletionPer1KUSD: 0.00060},
	{Model: "gpt-4-turbo", PromptPer1KUSD: 0.010, CompletionPer1KUSD: 0.030},
	{Model: "gpt-3.5-turbo", PromptPer1KUSD: 0.0005, CompletionPer1KUSD: 0.0015},
	// OpenAI embeddings (input-only; no completion tokens).
	{Model: "text-embedding-3-small", PromptPer1KUSD: 0.00002, CompletionPer1KUSD: 0},
	{Model: "text-embedding-3-large", PromptPer1KUSD: 0.00013, CompletionPer1KUSD: 0},
	{Model: "text-embedding-ada-002", PromptPer1KUSD: 0.00010, CompletionPer1KUSD: 0},
	// Anthropic
	{Model: "claude-3-5-sonnet-latest", PromptPer1KUSD: 0.003, CompletionPer1KUSD: 0.015},
	{Model: "claude-3-5-sonnet-20241022", PromptPer1KUSD: 0.003, CompletionPer1KUSD: 0.015},
	{Model: "claude-3-5-haiku-latest", PromptPer1KUSD: 0.0008, CompletionPer1KUSD: 0.004},
	{Model: "claude-3-opus-20240229", PromptPer1KUSD: 0.015, CompletionPer1KUSD: 0.075},
	{Model: "claude-3-sonnet-20240229", PromptPer1KUSD: 0.003, CompletionPer1KUSD: 0.015},
	{Model: "claude-3-haiku-20240307", PromptPer1KUSD: 0.00025, CompletionPer1KUSD: 0.00125},
	// Google Gemini (approximate public list prices, per 1K tokens).
	{Model: "gemini-2.5-pro", PromptPer1KUSD: 0.00125, CompletionPer1KUSD: 0.010},
	{Model: "gemini-2.5-flash", PromptPer1KUSD: 0.0003, CompletionPer1KUSD: 0.0025},
	{Model: "gemini-2.0-flash", PromptPer1KUSD: 0.0001, CompletionPer1KUSD: 0.0004},
	{Model: "gemini-1.5-pro", PromptPer1KUSD: 0.00125, CompletionPer1KUSD: 0.005},
	{Model: "gemini-1.5-flash", PromptPer1KUSD: 0.000075, CompletionPer1KUSD: 0.0003},
}

// Lookup returns the price for a model name. The match is
// case-insensitive; unknown models get the Fallback price (zero cost
// by default) and the ok return is false.
func (t *Table) Lookup(model string) (Price, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if p, ok := t.prices[strings.ToLower(model)]; ok {
		return p, true
	}
	return t.Fallback, false
}

// Set inserts or updates a price. Intended for test code and the
// YAML loader.
func (t *Table) Set(p Price) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prices[strings.ToLower(p.Model)] = p
}

// Estimate is a convenience that looks up the model and delegates to
// Price.Estimate.
func (t *Table) Estimate(model string, promptTokens, completionTokens int) float64 {
	p, _ := t.Lookup(model)
	return p.Estimate(promptTokens, completionTokens)
}

// fileDoc is the YAML envelope accepted by LoadYAML.
type fileDoc struct {
	Prices []Price `yaml:"prices"`
	// Fallback is optional; when omitted, the zero-cost default is
	// preserved from NewDefaultTable.
	Fallback *Price `yaml:"fallback,omitempty"`
}

// LoadYAML reads a pricing override file and merges it into t. Every
// entry in the file overrides a built-in with the same name; entries
// with new model names are appended. Returns an error on I/O failure
// or malformed YAML.
func (t *Table) LoadYAML(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading pricing file %s: %w", path, err)
	}
	var doc fileDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing pricing file %s: %w", path, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, p := range doc.Prices {
		if p.Model == "" {
			continue
		}
		t.prices[strings.ToLower(p.Model)] = p
	}
	if doc.Fallback != nil {
		t.Fallback = *doc.Fallback
	}
	return nil
}

// FromEnv builds a Table seeded with defaults and, if
// MOCKAGENTS_PRICING points at a readable YAML file, merges those
// overrides on top. Missing env var or unreadable file is treated as
// "use defaults" — operators see the reason in the returned error
// but the table is still usable.
func FromEnv() (*Table, error) {
	t := NewDefaultTable()
	path := os.Getenv("MOCKAGENTS_PRICING")
	if path == "" {
		return t, nil
	}
	if err := t.LoadYAML(path); err != nil {
		return t, err
	}
	return t, nil
}

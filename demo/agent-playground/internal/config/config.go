// Package config is the playground's user-editable configuration: the agent
// catalog (role, tier, models, prompts, tools, fallbacks, retry policy), the
// SLM/LLM routing policy, retry and review defaults, workflow thresholds and
// the price table. It loads from JSON (config/playground.json) and can be
// changed at runtime through the API or the UI. Every change is validated,
// and an invalid patch leaves the previous configuration untouched.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/retry"
)

// Tiers.
const (
	TierSLM  = "slm"
	TierLLM  = "llm"
	TierAuto = "auto"
)

// Review modes.
const (
	ReviewManual = "manual" // blocking gates wait for a human
	ReviewAuto   = "auto"   // gates are approved by policy (recorded as auto_approved)
)

// Roles an agent can declare. The router maps each to a tier.
var Roles = []string{"plan", "reason", "verify", "judge", "generate", "summarize", "classify", "extract", "chat"}

// Providers the playground can talk to.
var Providers = []string{"openai", "anthropic", "gemini"}

// ModelRef names a model on a provider.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (m ModelRef) String() string { return m.Provider + "/" + m.Model }

// Agent is one configurable agent.
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Role drives tier selection when Tier is "auto".
	Role string `json:"role"`
	// Tier is "auto" (route by role), "slm" or "llm".
	Tier string `json:"tier"`
	// Models maps tier -> model. An agent with both tiers can be routed or
	// escalated; one with a single tier always uses it.
	Models       map[string]ModelRef `json:"models"`
	SystemPrompt string              `json:"system_prompt"`
	Temperature  *float64            `json:"temperature,omitempty"`
	MaxTokens    int                 `json:"max_tokens,omitempty"`
	Tools        []string            `json:"tools,omitempty"`
	// Fallback models are tried, in order, once the primary model's retries
	// are exhausted.
	Fallback []ModelRef `json:"fallback,omitempty"`
	// Retry overrides the default retry policy for this agent.
	Retry *retry.Policy `json:"retry,omitempty"`
	// AttemptTimeoutMS overrides the default per-attempt timeout.
	AttemptTimeoutMS int `json:"attempt_timeout_ms,omitempty"`
	// Stream asks the provider for SSE delivery by default.
	Stream bool `json:"stream,omitempty"`
	// Mock names the mockagents fixture(s) behind this agent (documentation only).
	Mock []string `json:"mock,omitempty"`
}

// Review configures human review checkpoints.
type Review struct {
	Mode         string `json:"mode"`
	TimeoutMS    int    `json:"timeout_ms"`
	OnTimeout    string `json:"on_timeout"` // fail | approve
	MaxRevisions int    `json:"max_revisions"`
}

// Defaults applies to every agent and workflow unless overridden.
type Defaults struct {
	Retry            retry.Policy `json:"retry"`
	AttemptTimeoutMS int          `json:"attempt_timeout_ms"`
	RunTimeoutMS     int          `json:"run_timeout_ms"`
	MaxToolTurns     int          `json:"max_tool_turns"`
	Review           Review       `json:"review"`
}

// Router is the SLM/LLM routing policy.
type Router struct {
	// Policy maps role -> tier for agents whose tier is "auto".
	Policy map[string]string `json:"policy"`
	// EscalationThreshold: an SLM answer reporting confidence below this is
	// re-run on the LLM tier.
	EscalationThreshold float64 `json:"escalation_threshold"`
	// EscalateOnGuardFailure re-runs an SLM output that fails a guard on the LLM tier.
	EscalateOnGuardFailure bool `json:"escalate_on_guard_failure"`
}

// Workflows holds per-workflow tunables.
type Workflows struct {
	// ArbitrationThreshold: an arbiter verdict below this confidence is
	// escalated to a human decision.
	ArbitrationThreshold float64 `json:"arbitration_threshold"`
	// MaxGroundingRegenerations bounds hallucination regeneration attempts.
	MaxGroundingRegenerations int `json:"max_grounding_regenerations"`
	// WatchdogStallMS fails a run that makes no progress for this long while
	// not waiting on a human.
	WatchdogStallMS int `json:"watchdog_stall_ms"`
}

// Price is a per-model token price in USD per 1K tokens.
type Price struct {
	InputPer1K  float64 `json:"input_per_1k"`
	OutputPer1K float64 `json:"output_per_1k"`
}

// Config is the whole playground configuration.
type Config struct {
	Defaults  Defaults         `json:"defaults"`
	Router    Router           `json:"router"`
	Workflows Workflows        `json:"workflows"`
	Agents    []Agent          `json:"agents"`
	Pricing   map[string]Price `json:"pricing"`
}

// Clone deep-copies the configuration.
func (c *Config) Clone() *Config {
	raw, _ := json.Marshal(c)
	var out Config
	_ = json.Unmarshal(raw, &out)
	return &out
}

// Agent returns the agent by name.
func (c *Config) Agent(name string) (Agent, bool) {
	for _, a := range c.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// RetryFor resolves the effective retry policy for an agent.
func (c *Config) RetryFor(a Agent) retry.Policy {
	if a.Retry != nil {
		return *a.Retry
	}
	return c.Defaults.Retry
}

// StallMarginMS is the minimum headroom between the longest silent wait and
// the watchdog stall limit.
const StallMarginMS = 5_000

// retryAfterCapMS mirrors retry.MaxRetryAfter.
const retryAfterCapMS = 30_000

// LongestSilentWait is the longest time a healthy run can go without a
// heartbeat: one model attempt, or one backoff sleep (bounded by
// max_backoff_ms, or by the Retry-After cap).
func (c *Config) LongestSilentWait() int {
	longest := max(c.Defaults.AttemptTimeoutMS, c.Defaults.Retry.MaxBackoffMS, retryAfterCapMS)
	for _, a := range c.Agents {
		longest = max(longest, a.AttemptTimeoutMS)
		if a.Retry != nil {
			longest = max(longest, a.Retry.MaxBackoffMS)
		}
	}
	return longest
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// FieldError is a validation failure on one config path.
type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationError aggregates FieldErrors.
type ValidationError struct{ Fields []FieldError }

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Path + ": " + f.Message
	}
	return "invalid configuration: " + strings.Join(parts, "; ")
}

// Validate checks the whole configuration. knownTools, when non-nil, is the
// set of tool names agents may reference.
func (c *Config) Validate(knownTools []string) error {
	var fe []FieldError
	add := func(path, format string, args ...any) {
		fe = append(fe, FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if err := c.Defaults.Retry.Validate(); err != nil {
		add("defaults.retry", "%v", err)
	}
	if c.Defaults.AttemptTimeoutMS < 100 || c.Defaults.AttemptTimeoutMS > 300_000 {
		add("defaults.attempt_timeout_ms", "must be between 100 and 300000")
	}
	if c.Defaults.RunTimeoutMS < 1_000 || c.Defaults.RunTimeoutMS > 24*3600*1000 {
		add("defaults.run_timeout_ms", "must be between 1000 and 86400000")
	}
	if c.Defaults.MaxToolTurns < 1 || c.Defaults.MaxToolTurns > 20 {
		add("defaults.max_tool_turns", "must be between 1 and 20")
	}
	validateReview(c.Defaults.Review, "defaults.review", add)
	if c.Defaults.Review.TimeoutMS >= c.Defaults.RunTimeoutMS {
		add("defaults.review.timeout_ms", "must be shorter than defaults.run_timeout_ms so a pending review can never outlive its run")
	}
	for role, tier := range c.Router.Policy {
		if !slices.Contains(Roles, role) {
			add("router.policy."+role, "unknown role (want one of %s)", strings.Join(Roles, ", "))
		}
		if tier != TierSLM && tier != TierLLM {
			add("router.policy."+role, "tier must be slm or llm")
		}
	}
	if c.Router.EscalationThreshold < 0 || c.Router.EscalationThreshold > 1 {
		add("router.escalation_threshold", "must be between 0 and 1")
	}
	if c.Workflows.ArbitrationThreshold < 0 || c.Workflows.ArbitrationThreshold > 1 {
		add("workflows.arbitration_threshold", "must be between 0 and 1")
	}
	if c.Workflows.MaxGroundingRegenerations < 0 || c.Workflows.MaxGroundingRegenerations > 3 {
		add("workflows.max_grounding_regenerations", "must be between 0 and 3")
	}
	if c.Workflows.WatchdogStallMS < 1_000 {
		add("workflows.watchdog_stall_ms", "must be at least 1000")
	}
	// The watchdog fails a run with no heartbeat for watchdog_stall_ms. A run
	// blocked in one model attempt or one backoff sleep emits no heartbeat,
	// so the stall limit must exceed the longest single wait, or the watchdog
	// would kill healthy runs.
	if longest := c.LongestSilentWait(); c.Workflows.WatchdogStallMS > 0 && c.Workflows.WatchdogStallMS <= longest+StallMarginMS {
		add("workflows.watchdog_stall_ms", "must exceed the longest silent wait (%d ms: the largest attempt timeout, max_backoff_ms or 30000 ms Retry-After cap) by at least %d ms", longest, StallMarginMS)
	}
	seen := map[string]bool{}
	for i, a := range c.Agents {
		p := fmt.Sprintf("agents[%d]", i)
		if !namePattern.MatchString(a.Name) {
			add(p+".name", "must be lowercase kebab-case (got %q)", a.Name)
		}
		if seen[a.Name] {
			add(p+".name", "duplicate agent %q", a.Name)
		}
		seen[a.Name] = true
		fe = append(fe, a.validate(p, knownTools)...)
	}
	if len(fe) > 0 {
		return &ValidationError{Fields: fe}
	}
	return nil
}

func validateReview(r Review, path string, add func(string, string, ...any)) {
	if r.Mode != ReviewManual && r.Mode != ReviewAuto {
		add(path+".mode", "must be manual or auto")
	}
	if r.TimeoutMS < 1_000 {
		add(path+".timeout_ms", "must be at least 1000")
	}
	if r.OnTimeout != "fail" && r.OnTimeout != "approve" {
		add(path+".on_timeout", "must be fail or approve")
	}
	if r.MaxRevisions < 0 || r.MaxRevisions > 5 {
		add(path+".max_revisions", "must be between 0 and 5")
	}
}

func (a Agent) validate(p string, knownTools []string) []FieldError {
	var fe []FieldError
	add := func(path, format string, args ...any) {
		fe = append(fe, FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if !slices.Contains(Roles, a.Role) {
		add(p+".role", "unknown role %q (want one of %s)", a.Role, strings.Join(Roles, ", "))
	}
	if a.Tier != TierAuto && a.Tier != TierSLM && a.Tier != TierLLM {
		add(p+".tier", "must be auto, slm or llm")
	}
	if len(a.Models) == 0 {
		add(p+".models", "at least one tier model is required")
	}
	for tier, m := range a.Models {
		if tier != TierSLM && tier != TierLLM {
			add(p+".models."+tier, "tier key must be slm or llm")
		}
		validateModel(m, p+".models."+tier, add)
	}
	if (a.Tier == TierSLM || a.Tier == TierLLM) && len(a.Models) > 0 {
		if _, ok := a.Models[a.Tier]; !ok {
			add(p+".tier", "pinned tier %q has no model in models", a.Tier)
		}
	}
	for i, m := range a.Fallback {
		validateModel(m, fmt.Sprintf("%s.fallback[%d]", p, i), add)
	}
	if a.Temperature != nil && (*a.Temperature < 0 || *a.Temperature > 2) {
		add(p+".temperature", "must be between 0 and 2")
	}
	if a.MaxTokens < 0 || a.MaxTokens > 32_000 {
		add(p+".max_tokens", "must be between 0 and 32000")
	}
	if a.Retry != nil {
		if err := a.Retry.Validate(); err != nil {
			add(p+".retry", "%v", err)
		}
	}
	if a.AttemptTimeoutMS != 0 && (a.AttemptTimeoutMS < 100 || a.AttemptTimeoutMS > 300_000) {
		add(p+".attempt_timeout_ms", "must be between 100 and 300000")
	}
	if len(a.SystemPrompt) > 8_000 {
		add(p+".system_prompt", "must be at most 8000 characters")
	}
	if knownTools != nil {
		for _, t := range a.Tools {
			if !slices.Contains(knownTools, t) {
				add(p+".tools", "unknown tool %q", t)
			}
		}
	}
	if len(a.Tools) > 0 {
		for tier, m := range a.Models {
			if m.Provider == "gemini" {
				add(p+".models."+tier, "the playground's gemini client is text-only; agents with tools need openai or anthropic")
			}
		}
	}
	return fe
}

func validateModel(m ModelRef, path string, add func(string, string, ...any)) {
	if !slices.Contains(Providers, m.Provider) {
		add(path+".provider", "must be one of %s", strings.Join(Providers, ", "))
	}
	if strings.TrimSpace(m.Model) == "" {
		add(path+".model", "is required")
	}
}

// Load reads and validates a JSON config file.
func Load(path string, knownTools []string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return Parse(raw, knownTools)
}

// Parse decodes and validates JSON config bytes. Unknown fields are rejected
// so a typo can't silently fall back to a default.
func Parse(raw []byte, knownTools []string) (*Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := c.Validate(knownTools); err != nil {
		return nil, err
	}
	return &c, nil
}

// Store is the live, concurrency-safe configuration with a version counter.
type Store struct {
	mu         sync.RWMutex
	cfg        *Config
	initial    *Config
	version    int
	knownTools []string
}

// NewStore wraps a validated config.
func NewStore(c *Config, knownTools []string) *Store {
	return &Store{cfg: c.Clone(), initial: c.Clone(), version: 1, knownTools: knownTools}
}

// Get returns a copy of the current configuration and its version.
func (s *Store) Get() (*Config, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone(), s.version
}

// Current returns the current configuration without copying. Callers must
// treat it as read-only.
func (s *Store) Current() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update applies fn to a copy, validates it, and swaps it in atomically.
func (s *Store) Update(fn func(c *Config) error) (*Config, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.Clone()
	if err := fn(next); err != nil {
		return nil, s.version, err
	}
	if err := next.Validate(s.knownTools); err != nil {
		return nil, s.version, err
	}
	s.cfg = next
	s.version++
	return next.Clone(), s.version, nil
}

// Reset restores the configuration loaded at startup.
func (s *Store) Reset() (*Config, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = s.initial.Clone()
	s.version++
	return s.cfg.Clone(), s.version
}

// ErrUnknownAgent is returned for an agent name that is not configured.
var ErrUnknownAgent = errors.New("unknown agent")

// AgentNames lists configured agents, sorted.
func (c *Config) AgentNames() []string {
	out := make([]string, len(c.Agents))
	for i, a := range c.Agents {
		out[i] = a.Name
	}
	sort.Strings(out)
	return out
}

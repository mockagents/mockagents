package types

// BehaviorConfig defines how an agent responds to messages.
type BehaviorConfig struct {
	Scenarios []Scenario       `yaml:"scenarios" json:"scenarios"`
	Streaming *StreamingConfig `yaml:"streaming,omitempty" json:"streaming,omitempty"`
	Chaos     *ChaosConfig     `yaml:"chaos,omitempty" json:"chaos,omitempty"`
}

// Scenario defines a pattern-matched response rule.
type Scenario struct {
	Name     string           `yaml:"name" json:"name"`
	Match    *MatchRule       `yaml:"match,omitempty" json:"match,omitempty"`
	Response ScenarioResponse `yaml:"response" json:"response"`
}

// MatchRule defines conditions under which a scenario activates.
type MatchRule struct {
	ContentContains string `yaml:"content_contains,omitempty" json:"content_contains,omitempty"`
	ContentRegex    string `yaml:"content_regex,omitempty" json:"content_regex,omitempty"`
	TurnNumber      *int   `yaml:"turn_number,omitempty" json:"turn_number,omitempty"`
}

// ScenarioResponse is the response produced when a scenario matches.
type ScenarioResponse struct {
	Content   string         `yaml:"content" json:"content"`
	ToolCalls []ToolCallSpec `yaml:"tool_calls,omitempty" json:"tool_calls,omitempty"`
	Metadata  map[string]any `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

// ToolCallSpec describes a tool call the agent should simulate in a response.
type ToolCallSpec struct {
	Name      string         `yaml:"name" json:"name"`
	Arguments map[string]any `yaml:"arguments,omitempty" json:"arguments,omitempty"`
}

// StreamingConfig controls SSE streaming behavior.
type StreamingConfig struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`
	ChunkSize    int  `yaml:"chunk_size,omitempty" json:"chunk_size,omitempty"`
	ChunkDelayMs int  `yaml:"chunk_delay_ms,omitempty" json:"chunk_delay_ms,omitempty"`
}

// ChaosConfig defines fault injection settings applied to every request
// served by the owning agent. All fields are optional; a nil ChaosConfig
// disables chaos entirely for the agent.
type ChaosConfig struct {
	Enabled   bool                  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Latency   *ChaosLatencyConfig   `yaml:"latency,omitempty" json:"latency,omitempty"`
	Errors    *ChaosErrorConfig     `yaml:"errors,omitempty" json:"errors,omitempty"`
	RateLimit *ChaosRateLimitConfig `yaml:"rate_limit,omitempty" json:"rate_limit,omitempty"`
}

// ChaosLatencyConfig controls how artificial delay is added to responses.
// Supported distributions: "fixed" (uses MinMs), "uniform" (MinMs..MaxMs),
// "normal" (MeanMs ± StddevMs, clipped at zero). An empty distribution
// defaults to "uniform" when Min/Max are set, else "fixed".
type ChaosLatencyConfig struct {
	Distribution string `yaml:"distribution,omitempty" json:"distribution,omitempty"`
	MinMs        int    `yaml:"min_ms,omitempty" json:"min_ms,omitempty"`
	MaxMs        int    `yaml:"max_ms,omitempty" json:"max_ms,omitempty"`
	MeanMs       int    `yaml:"mean_ms,omitempty" json:"mean_ms,omitempty"`
	StddevMs     int    `yaml:"stddev_ms,omitempty" json:"stddev_ms,omitempty"`
}

// ChaosErrorConfig defines random error injection.
// Rate is a probability in [0.0, 1.0]. StatusCode and StatusCodes select
// the error surface: when StatusCodes is set one is picked uniformly per
// injected error, otherwise StatusCode is used (defaulting to 500). Message
// overrides the body text (defaults to the status text). When Timeout is
// true the request goroutine BLOCKS for TimeoutMs — a real sleep, cut short
// only by request-context cancellation — and then returns a synthetic 504.
type ChaosErrorConfig struct {
	Rate        float64 `yaml:"rate,omitempty" json:"rate,omitempty"`
	StatusCode  int     `yaml:"status_code,omitempty" json:"status_code,omitempty"`
	StatusCodes []int   `yaml:"status_codes,omitempty" json:"status_codes,omitempty"`
	Timeout     bool    `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	TimeoutMs   int     `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	Message     string  `yaml:"message,omitempty" json:"message,omitempty"`
}

// ChaosRateLimitConfig caps the number of requests per window. When exceeded,
// the middleware returns a 429 with Retry-After set to the window remainder.
type ChaosRateLimitConfig struct {
	Requests int `yaml:"requests" json:"requests"`
	WindowMs int `yaml:"window_ms" json:"window_ms"`
}

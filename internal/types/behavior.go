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
	// HasImage, when set, matches only when the latest user turn carries at
	// least one image content part (true) or none (false) — A-05 vision matching.
	// The image signal is out-of-band (the flattened user text stays pure, so
	// content_regex/templates are unaffected).
	HasImage *bool `yaml:"has_image,omitempty" json:"has_image,omitempty"`
}

// ScenarioResponse is the response produced when a scenario matches.
type ScenarioResponse struct {
	Content   string         `yaml:"content" json:"content"`
	ToolCalls []ToolCallSpec `yaml:"tool_calls,omitempty" json:"tool_calls,omitempty"`
	Metadata  map[string]any `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	// FinishReason overrides the emitted finish/stop reason (FB-03 semantic
	// errors). Given as an OpenAI-style value ("length", "content_filter",
	// "stop", "tool_calls") and mapped per provider (Anthropic stop_reason,
	// Gemini finishReason). Use "length" to simulate a truncated response.
	FinishReason string `yaml:"finish_reason,omitempty" json:"finish_reason,omitempty"`
	// Refusal, when set, emits an assistant refusal instead of normal content —
	// OpenAI's structured `message.refusal` field (and the refusal text as
	// content on Anthropic/Gemini), to exercise refusal-handling code paths.
	Refusal       string             `yaml:"refusal,omitempty" json:"refusal,omitempty"`
	Hallucination *HallucinationSpec `yaml:"hallucination,omitempty" json:"hallucination,omitempty"`
}

// HallucinationSpec marks a scenario response as a deterministic *hallucination
// fixture* (FB-02): a planted bad output for testing the client's guardrails,
// validators, and fallback logic against failures that are hard to elicit from
// a real model. The mock advertises it via the `X-Mockagents-Hallucination`
// response header so negative tests can assert their guardrail flagged it while
// knowing the ground truth. The Content itself is the (deliberately wrong)
// output; this struct only labels/categorizes it.
type HallucinationSpec struct {
	// Type categorizes the planted fault. One of: fabricated_fact,
	// fabricated_citation, ungrounded, bad_tool_result, other (default: other).
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
	// GroundTruth is the correct answer — documentation for the test author and
	// a reference an assertion can compare against.
	GroundTruth string `yaml:"ground_truth,omitempty" json:"ground_truth,omitempty"`
	// Note explains why the output is wrong.
	Note string `yaml:"note,omitempty" json:"note,omitempty"`
}

// HallucinationTypes are the recognized HallucinationSpec.Type values.
var HallucinationTypes = []string{"fabricated_fact", "fabricated_citation", "ungrounded", "bad_tool_result", "other"}

// ToolCallSpec describes a tool call the agent should simulate in a response.
type ToolCallSpec struct {
	Name      string         `yaml:"name" json:"name"`
	Arguments map[string]any `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	// RawArguments, when set, is emitted VERBATIM as the tool call's argument
	// string instead of marshaling Arguments — so a scenario can plant malformed
	// or schema-violating JSON (e.g. `{"city":`) to exercise a client's tool-call
	// argument parser (FB-03 semantic errors). OpenAI only (the provider whose
	// tool-call arguments are a JSON string).
	RawArguments string `yaml:"raw_arguments,omitempty" json:"raw_arguments,omitempty"`
}

// StreamingConfig controls SSE streaming behavior, including stream-timing
// physics and mid-stream fault injection (RR-07).
type StreamingConfig struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`
	ChunkSize    int  `yaml:"chunk_size,omitempty" json:"chunk_size,omitempty"`
	ChunkDelayMs int  `yaml:"chunk_delay_ms,omitempty" json:"chunk_delay_ms,omitempty"`

	// --- Stream-timing physics ---

	// TTFTMs is the time-to-first-token: a delay before the first content
	// chunk is emitted (the structural opening frame is sent immediately).
	TTFTMs int `yaml:"ttft_ms,omitempty" json:"ttft_ms,omitempty"`
	// TokensPerSec, when > 0, paces content chunks at this rate (the per-chunk
	// delay is derived from the chunk length), overriding ChunkDelayMs.
	TokensPerSec float64 `yaml:"tokens_per_sec,omitempty" json:"tokens_per_sec,omitempty"`
	// JitterMs adds a deterministic +/- jitter (up to this many ms) to each
	// inter-chunk delay, modeling network variance.
	JitterMs int `yaml:"jitter_ms,omitempty" json:"jitter_ms,omitempty"`

	// --- Distribution-based stream timing (FB-05: load-target physics) ---
	//
	// Real LLM latency is long-tailed, so a single fixed value under-models a
	// load test. When the p50 AND p95 of either metric are both > 0, the pacer
	// samples that delay from a lognormal fit to the two percentiles (overriding
	// the fixed TTFTMs / TokensPerSec). Values are milliseconds.

	// TTFTP50Ms / TTFTP95Ms are the median and 95th-percentile time-to-first-token.
	TTFTP50Ms int `yaml:"ttft_p50_ms,omitempty" json:"ttft_p50_ms,omitempty"`
	TTFTP95Ms int `yaml:"ttft_p95_ms,omitempty" json:"ttft_p95_ms,omitempty"`
	// ITLP50Ms / ITLP95Ms are the median and 95th-percentile inter-token latency
	// (per token); the per-chunk delay is the sample times the chunk's token count.
	ITLP50Ms int `yaml:"itl_p50_ms,omitempty" json:"itl_p50_ms,omitempty"`
	ITLP95Ms int `yaml:"itl_p95_ms,omitempty" json:"itl_p95_ms,omitempty"`

	// --- Mid-stream fault injection ---

	// TruncateAfterChunks, when > 0, ends the stream after this many content
	// chunks WITHOUT the terminating finish frame / [DONE] sentinel — a
	// truncated stream, to test client robustness to early disconnects.
	TruncateAfterChunks int `yaml:"truncate_after_chunks,omitempty" json:"truncate_after_chunks,omitempty"`
	// Malformed, when true, emits one deliberately invalid JSON SSE frame at
	// the stop point and then ends the stream (no finish frame / [DONE]) — to
	// test client parser/error handling of malformed chunks and tool calls.
	Malformed bool `yaml:"malformed,omitempty" json:"malformed,omitempty"`
}

// ChaosConfig defines fault injection settings applied to every request
// served by the owning agent. All fields are optional; a nil ChaosConfig
// disables chaos entirely for the agent.
type ChaosConfig struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Preset is a named shorthand (e.g. "server-down", "rate-limited",
	// "access-denied") that expands at load time into the concrete sub-sections
	// below. Explicitly-set sub-sections take precedence over the preset's
	// values. See ChaosPresets for the recognized names.
	Preset    string                `yaml:"preset,omitempty" json:"preset,omitempty"`
	Latency   *ChaosLatencyConfig   `yaml:"latency,omitempty" json:"latency,omitempty"`
	Errors    *ChaosErrorConfig     `yaml:"errors,omitempty" json:"errors,omitempty"`
	RateLimit *ChaosRateLimitConfig `yaml:"rate_limit,omitempty" json:"rate_limit,omitempty"`
}

// ChaosPresets are the recognized ChaosConfig.Preset names. Kept here so the
// validator, the schema, and docs share one source of truth.
var ChaosPresets = []string{"server-down", "rate-limited", "access-denied", "unauthorized", "flaky", "slow"}

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
	// FailFirst, when > 0, deterministically injects the error on the first N
	// requests to the agent and then RECOVERS (every request after the Nth
	// succeeds) — a stateful "flaky then healthy" trigger for exercising client
	// retry/backoff/circuit-breaker logic, which is otherwise impossible to test
	// reproducibly against a live API. It takes precedence over Rate: the first N
	// requests always fail regardless of the probability, then injection stops.
	// The count is per agent and resets when the server restarts.
	FailFirst int `yaml:"fail_first,omitempty" json:"fail_first,omitempty"`
}

// ChaosRateLimitConfig caps the number of requests per window. When exceeded,
// the middleware returns a 429 with Retry-After set to the window remainder.
type ChaosRateLimitConfig struct {
	Requests int `yaml:"requests" json:"requests"`
	WindowMs int `yaml:"window_ms" json:"window_ms"`
}

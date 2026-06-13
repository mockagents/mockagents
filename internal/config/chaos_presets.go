package config

import (
	"sort"

	"github.com/mockagents/mockagents/internal/types"
)

// chaosPresets maps a preset name to a function that fills a ChaosConfig's
// sub-sections. Each only sets a section that the agent left nil, so an author
// can start from a preset and override individual fields (e.g.
// `preset: rate-limited` plus a custom `latency:` block).
//
// The set of names is mirrored by types.ChaosPresets (used by the validator and
// schema) — keep the two in sync.
var chaosPresets = map[string]func(*types.ChaosConfig){
	// server-down: every request returns 503 (overloaded / service unavailable).
	"server-down": func(c *types.ChaosConfig) {
		if c.Errors == nil {
			c.Errors = &types.ChaosErrorConfig{Rate: 1, StatusCode: 503, Message: "the server is temporarily unavailable"}
		}
	},
	// rate-limited: every request returns 429 (with a Retry-After at the wire).
	"rate-limited": func(c *types.ChaosConfig) {
		if c.Errors == nil {
			c.Errors = &types.ChaosErrorConfig{Rate: 1, StatusCode: 429, Message: "rate limit exceeded"}
		}
	},
	// access-denied: every request returns 403 (permission denied).
	"access-denied": func(c *types.ChaosConfig) {
		if c.Errors == nil {
			c.Errors = &types.ChaosErrorConfig{Rate: 1, StatusCode: 403, Message: "you do not have access to this resource"}
		}
	},
	// unauthorized: every request returns 401 (bad/missing credentials).
	"unauthorized": func(c *types.ChaosConfig) {
		if c.Errors == nil {
			c.Errors = &types.ChaosErrorConfig{Rate: 1, StatusCode: 401, Message: "invalid authentication credentials"}
		}
	},
	// flaky: fail the first 2 requests with 503, then recover — the retry fixture.
	"flaky": func(c *types.ChaosConfig) {
		if c.Errors == nil {
			c.Errors = &types.ChaosErrorConfig{FailFirst: 2, StatusCode: 503, Message: "temporarily overloaded, please retry"}
		}
	},
	// slow: add 2–5s of latency to every response.
	"slow": func(c *types.ChaosConfig) {
		if c.Latency == nil {
			c.Latency = &types.ChaosLatencyConfig{Distribution: "uniform", MinMs: 2000, MaxMs: 5000}
		}
	},
	// connection-reset: every request resets the TCP connection (peer-reset).
	"connection-reset": func(c *types.ChaosConfig) {
		if c.Connection == nil {
			c.Connection = &types.ChaosConnectionConfig{Mode: "reset", Rate: 1}
		}
	},
}

// connectionModes is the set of accepted ChaosConnectionConfig.Mode values
// (canonical names plus aliases). Shared by the validator.
var connectionModes = map[string]bool{
	"reset": true, "peer-reset": true,
	"empty":  true,
	"random": true, "random-then-close": true, "garbage": true,
}

// connectionModeNames returns the accepted modes sorted, so a validation hint
// can never drift from connectionModes.
func connectionModeNames() []string {
	names := make([]string, 0, len(connectionModes))
	for m := range connectionModes {
		names = append(names, m)
	}
	sort.Strings(names)
	return names
}

// isChaosPreset reports whether name is a recognized preset.
func isChaosPreset(name string) bool {
	_, ok := chaosPresets[name]
	return ok
}

// expandChaosPreset rewrites a preset name into the concrete chaos sub-sections.
// Unknown presets are left untouched (the validator reports them); explicitly
// set sub-sections always win. Marking the block enabled keeps it active even
// though every section was nil before expansion.
func expandChaosPreset(c *types.ChaosConfig) {
	if c == nil || c.Preset == "" {
		return
	}
	apply, ok := chaosPresets[c.Preset]
	if !ok {
		return
	}
	apply(c)
	c.Enabled = true
}

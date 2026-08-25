// Package chaos provides provider-neutral deterministic fault decisions.
package chaos

import (
	"encoding/binary"
	"hash/fnv"
	"strings"
)

const ForceHeader = "X-Mockagents-Chaos"

// Policy gates a configured fault. A nil Rate preserves legacy always-on
// fixtures; an explicit rate is clamped to [0,1] and sampled deterministically.
type Policy struct {
	Seed int64
	Rate *float64
	// Source labels the configured scope that supplied Rate. Empty preserves
	// the legacy configured/seeded-rate labels.
	Source string
}

// Decision describes why a configured fault should or should not run.
type Decision struct {
	Apply  bool
	Source string
}

// Decide applies precedence: request "off", request action, legacy always-on,
// then fixed-seed probability. A forced action only selects an action that the
// caller has configured; it does not invent a new fault.
func Decide(policy Policy, requestKey, configuredAction, forcedAction string) Decision {
	force := strings.ToLower(strings.TrimSpace(forcedAction))
	action := strings.ToLower(strings.TrimSpace(configuredAction))
	if force == "off" || force == "none" {
		return Decision{Source: "request-off"}
	}
	if force != "" {
		return Decision{Apply: force == action, Source: "request-force"}
	}
	if policy.Rate == nil {
		source := policy.Source
		if source == "" {
			source = "configured"
		}
		return Decision{Apply: true, Source: source}
	}
	source := policy.Source
	if source == "" {
		source = "seeded-rate"
	}
	rate := *policy.Rate
	if rate <= 0 {
		return Decision{Source: source}
	}
	if rate >= 1 {
		return Decision{Apply: true, Source: source}
	}
	h := fnv.New64a()
	var seed [8]byte
	binary.LittleEndian.PutUint64(seed[:], uint64(policy.Seed))
	_, _ = h.Write(seed[:])
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(requestKey))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(action))
	// Use the top 53 bits, matching float64's exact integer precision.
	sample := float64(h.Sum64()>>11) / float64(uint64(1)<<53)
	return Decision{Apply: sample < rate, Source: source}
}

// ForOperation applies an exact operation-rate override ahead of the service
// rate. The operation name also enters the deterministic sampling key.
func ForOperation(base Policy, operation string, rates map[string]float64) (Policy, string) {
	if rate, ok := rates[operation]; ok {
		base.Rate = &rate
		base.Source = "operation-rate"
	}
	return base, operation
}

// ForSequence applies a one-based request-sequence override. Call it after
// broader scopes so sequence policy wins while request force/off remains
// authoritative in Decide.
func ForSequence(base Policy, sequence uint64, rates map[uint64]float64) Policy {
	if rate, ok := rates[sequence]; ok {
		base.Rate = &rate
		base.Source = "sequence-rate"
	}
	return base
}

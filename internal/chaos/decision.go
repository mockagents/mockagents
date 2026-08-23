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
		return Decision{Apply: true, Source: "configured"}
	}
	rate := *policy.Rate
	if rate <= 0 {
		return Decision{Source: "seeded-rate"}
	}
	if rate >= 1 {
		return Decision{Apply: true, Source: "seeded-rate"}
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
	return Decision{Apply: sample < rate, Source: "seeded-rate"}
}

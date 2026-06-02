package engine

import (
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

// ErrNoMatchingScenario is returned when no scenario matches the request.
var ErrNoMatchingScenario = errors.New("no matching scenario found")

// MatchResult holds the matched scenario and any regex capture groups.
type MatchResult struct {
	Scenario *types.Scenario
	Captures map[string]string // Named capture groups from regex match
}

// ScenarioMatcher evaluates scenarios against incoming messages.
type ScenarioMatcher struct {
	regexCache sync.Map // string -> *regexp.Regexp
}

// NewScenarioMatcher creates a new ScenarioMatcher.
func NewScenarioMatcher() *ScenarioMatcher {
	return &ScenarioMatcher{}
}

// Match evaluates scenarios in definition order and returns the first match.
// If no explicit match is found, returns the default scenario (Match == nil).
// Returns ErrNoMatchingScenario if no scenario matches.
func (m *ScenarioMatcher) Match(scenarios []types.Scenario, userMessage string, turnNumber int) (*types.Scenario, error) {
	result := m.MatchWithCaptures(scenarios, userMessage, turnNumber)
	if result != nil {
		return result.Scenario, nil
	}
	return nil, ErrNoMatchingScenario
}

// MatchWithCaptures evaluates scenarios and returns the match result
// including any regex named capture groups.
func (m *ScenarioMatcher) MatchWithCaptures(scenarios []types.Scenario, userMessage string, turnNumber int) *MatchResult {
	var defaultScenario *types.Scenario

	// Lower-case the user message once for the whole scenario sweep. Every
	// content_contains rule does a case-insensitive compare; doing it here
	// avoids re-allocating a lowered copy of the full message per scenario.
	lowerMessage := strings.ToLower(userMessage)

	for i := range scenarios {
		sc := &scenarios[i]
		if sc.Match == nil {
			if defaultScenario == nil {
				defaultScenario = sc
			}
			continue
		}

		captures := m.evaluate(sc.Match, userMessage, lowerMessage, turnNumber)
		if captures != nil {
			return &MatchResult{Scenario: sc, Captures: captures}
		}
	}

	if defaultScenario != nil {
		return &MatchResult{Scenario: defaultScenario, Captures: nil}
	}
	return nil
}

// matchedSentinel is returned by evaluate() when a rule matches but
// carries no capture groups. A single shared non-nil map lets callers
// distinguish "matched with nothing" from "did not match" without
// allocating a fresh empty map on every successful content_contains
// match. The sentinel is deliberately unexported and never written to.
var matchedSentinel = map[string]string{}

// evaluate checks all match conditions (AND logic).
// Returns the capture groups map if matched, nil if not matched.
// A non-nil empty map (matchedSentinel) means "matched with no captures".
//
// The captures map is allocated lazily — only when a content_regex
// rule with named groups actually matches. ContentContains-only
// scenarios (the common case) pay zero map allocations.
// lowerMessage is userMessage pre-lowered by the caller for the
// case-insensitive content_contains compare; userMessage stays in its
// original case for regex matching and capture extraction.
func (m *ScenarioMatcher) evaluate(rule *types.MatchRule, userMessage, lowerMessage string, turnNumber int) map[string]string {
	if rule.ContentContains != "" {
		if !strings.Contains(lowerMessage, strings.ToLower(rule.ContentContains)) {
			return nil
		}
	}

	var captures map[string]string
	if rule.ContentRegex != "" {
		re := m.compileRegex(rule.ContentRegex)
		if re == nil {
			return nil
		}
		match := re.FindStringSubmatch(userMessage)
		if match == nil {
			return nil
		}
		// Extract named capture groups. Allocate only when the regex
		// actually declares at least one name — anonymous groups are
		// common and do not need a map.
		for i, name := range re.SubexpNames() {
			// len(match) == len(SubexpNames()) on a successful match,
			// so indexing match[i] needs no bounds guard.
			if i > 0 && name != "" {
				if captures == nil {
					captures = make(map[string]string, 4)
				}
				captures[name] = match[i]
			}
		}
	}

	if rule.TurnNumber != nil {
		if turnNumber != *rule.TurnNumber {
			return nil
		}
	}

	if captures == nil {
		return matchedSentinel
	}
	return captures
}

// compileRegex compiles a regex pattern with caching.
func (m *ScenarioMatcher) compileRegex(pattern string) *regexp.Regexp {
	if cached, ok := m.regexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	m.regexCache.Store(pattern, re)
	return re
}

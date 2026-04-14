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

	for i := range scenarios {
		sc := &scenarios[i]
		if sc.Match == nil {
			if defaultScenario == nil {
				defaultScenario = sc
			}
			continue
		}

		captures := m.evaluate(sc.Match, userMessage, turnNumber)
		if captures != nil {
			return &MatchResult{Scenario: sc, Captures: captures}
		}
	}

	if defaultScenario != nil {
		return &MatchResult{Scenario: defaultScenario, Captures: nil}
	}
	return nil
}

// evaluate checks all match conditions (AND logic).
// Returns the capture groups map if matched, nil if not matched.
// An empty non-nil map means "matched with no captures".
func (m *ScenarioMatcher) evaluate(rule *types.MatchRule, userMessage string, turnNumber int) map[string]string {
	captures := make(map[string]string)

	if rule.ContentContains != "" {
		if !strings.Contains(strings.ToLower(userMessage), strings.ToLower(rule.ContentContains)) {
			return nil
		}
	}

	if rule.ContentRegex != "" {
		re := m.compileRegex(rule.ContentRegex)
		if re == nil {
			return nil
		}
		match := re.FindStringSubmatch(userMessage)
		if match == nil {
			return nil
		}
		// Extract named capture groups.
		for i, name := range re.SubexpNames() {
			if i > 0 && name != "" && i < len(match) {
				captures[name] = match[i]
			}
		}
	}

	if rule.TurnNumber != nil {
		if turnNumber != *rule.TurnNumber {
			return nil
		}
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

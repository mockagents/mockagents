// Package guard holds deterministic output checks that reduce hallucination
// risk without trusting another model:
//
//   - CheckGrounding: every figure and every [KB-nnn] citation in a summary
//     must appear in the evidence the agents actually retrieved.
//   - ExtractJSON: tolerant structured-output parsing (strips code fences and
//     prose around the JSON object), so a chatty model does not crash a
//     workflow and a truly broken one fails loudly.
//
// The research-brief workflow runs these first, then hands their flags to an
// LLM judge: "a judge augmented by deterministic checks".
package guard

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// GroundingReport is the guard verdict for one text.
type GroundingReport struct {
	Grounded           bool     `json:"grounded"`
	CheckedFigures     []string `json:"checked_figures"`
	UnsupportedFigures []string `json:"unsupported_figures,omitempty"`
	Citations          []string `json:"citations"`
	UnknownCitations   []string `json:"unknown_citations,omitempty"`
	UncitedEvidence    bool     `json:"uncited,omitempty"`
}

// Flags renders the report as the one-line "Guard flags:" value that the
// verifier agent receives.
func (r GroundingReport) Flags() string {
	if r.Grounded {
		return "none"
	}
	var parts []string
	if len(r.UnsupportedFigures) > 0 {
		parts = append(parts, "figures "+strings.Join(r.UnsupportedFigures, ", "))
	}
	if len(r.UnknownCitations) > 0 {
		parts = append(parts, "citations "+strings.Join(r.UnknownCitations, ", "))
	}
	if r.UncitedEvidence {
		parts = append(parts, "no citations")
	}
	return "unsupported " + strings.Join(parts, "; ")
}

var (
	citationRe = regexp.MustCompile(`\bKB-\d+\b`)
	figureRe   = regexp.MustCompile(`\d+(?:\.\d+)?`)
)

// CheckGrounding verifies text against evidence. known is the set of valid
// citation ids. requireCitation makes a summary with no citation at all a
// violation.
func CheckGrounding(text string, evidence []string, known map[string]bool, requireCitation bool) GroundingReport {
	r := GroundingReport{Grounded: true}
	ev := strings.Join(evidence, "\n")
	evFigures := map[string]bool{}
	for _, f := range figureRe.FindAllString(citationRe.ReplaceAllString(ev, " "), -1) {
		evFigures[normalizeFigure(f)] = true
	}
	seenCite := map[string]bool{}
	for _, c := range citationRe.FindAllString(text, -1) {
		if seenCite[c] {
			continue
		}
		seenCite[c] = true
		r.Citations = append(r.Citations, c)
		if !known[c] || !strings.Contains(ev, c) {
			r.UnknownCitations = append(r.UnknownCitations, c)
		}
	}
	// Strip citations before extracting figures so "KB-101" is not read as 101.
	body := citationRe.ReplaceAllString(text, " ")
	seenFig := map[string]bool{}
	for _, f := range figureRe.FindAllString(body, -1) {
		n := normalizeFigure(f)
		if seenFig[n] {
			continue
		}
		seenFig[n] = true
		r.CheckedFigures = append(r.CheckedFigures, f)
		if !evFigures[n] {
			r.UnsupportedFigures = append(r.UnsupportedFigures, f)
		}
	}
	sort.Strings(r.UnknownCitations)
	if requireCitation && len(r.Citations) == 0 {
		r.UncitedEvidence = true
	}
	r.Grounded = len(r.UnsupportedFigures) == 0 && len(r.UnknownCitations) == 0 && !r.UncitedEvidence
	return r
}

// normalizeFigure makes "3200" and "3200.0" compare equal.
func normalizeFigure(f string) string {
	if strings.Contains(f, ".") {
		f = strings.TrimRight(strings.TrimRight(f, "0"), ".")
	}
	if f == "" {
		return "0"
	}
	return f
}

// ErrNoJSON means no JSON object could be found in a model's output.
var ErrNoJSON = errors.New("no JSON object found in model output")

// ExtractJSON decodes the first JSON object in text into v.
func ExtractJSON(text string, v any) error {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ErrNoJSON
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), v); err != nil {
		return fmt.Errorf("%w: %v", ErrNoJSON, err)
	}
	return nil
}

// SanitizeLine makes untrusted user input safe to embed as one line of a
// prompt: no newlines (which could forge "Guard flags:" or similar
// structured lines), no quotes or backslashes (which would break
// JSON-templated fixtures), bounded length.
func SanitizeLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r == '"' || r == '\\' || r == '`':
			return -1
		case r < 0x20:
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len([]rune(s)) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

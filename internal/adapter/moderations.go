package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// ProtocolOpenAIModerations is the wire-protocol label recorded for the OpenAI
// Moderations surface (POST /v1/moderations).
const ProtocolOpenAIModerations = "openai-moderations"

// maxModerationInputs caps the input batch (same rationale as embeddings: bound
// the per-request work so a body of many tiny strings can't amplify CPU).
const maxModerationInputs = 2048

// moderationFlagThreshold is the score above which a category is flagged.
const moderationFlagThreshold = 0.5

const defaultModerationModel = "omni-moderation-latest"

// allModerationCategories is the canonical, ordered list of the 13
// omni-moderation categories (exact key strings, slashes included).
var allModerationCategories = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

// moderationKeywords maps a category to the (tasteful, non-slur) terms that
// flag it. This is a DETERMINISTIC TEST AID, not a real safety classifier:
// guardrail-pipeline tests that want flagged=true should use these exact terms.
// Single-word terms match on word boundaries (so "skill" doesn't trip "kill");
// multi-word phrases match as substrings.
var moderationKeywords = map[string][]string{
	"harassment":             {"harass", "bully", "stalk", "intimidate"},
	"harassment/threatening": {"i'll kill you", "going to attack you", "you're dead", "will hurt you"},
	"hate":                   {"bigot", "they are inferior", "subhuman"},
	"hate/threatening":       {"death to all", "exterminate the", "wipe them out"},
	"illicit":                {"how to steal", "buy drugs", "black market", "how to cheat"},
	"illicit/violent":        {"build a bomb", "make a weapon", "synthesize explosive"},
	"self-harm":              {"cut myself", "harm myself", "hurt myself"},
	"self-harm/intent":       {"kill myself", "end my life", "want to die"},
	"self-harm/instructions": {"how to kill myself", "ways to commit suicide", "steps to self-harm"},
	"sexual":                 {"explicit sexual", "pornographic", "nude content"},
	"sexual/minors":          {"child pornography", "minor explicit", "underage sexual"},
	"violence":               {"kill", "attack", "bomb", "murder", "shoot", "stab", "massacre"},
	"violence/graphic":       {"decapitated", "dismembered", "graphic torture", "bloody massacre"},
}

// --- request / response types ---

// ModerationsRequest is an OpenAI Moderations request. `input` is polymorphic
// (a string, an array of strings, or an array of content parts).
type ModerationsRequest struct {
	Input json.RawMessage `json:"input"`
	Model string          `json:"model,omitempty"`
}

// ModerationsResponse is the OpenAI Moderations response (one result per input).
type ModerationsResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Results []ModerationResult `json:"results"`
}

// ModerationResult is the per-input classification. All three maps carry every
// one of the 13 categories (categories: bool, category_scores: float,
// category_applied_input_types: ["text"] for a text input) — matching the real
// omni-moderation shape, whose SDK model requires every category field.
type ModerationResult struct {
	Flagged                   bool                `json:"flagged"`
	Categories                map[string]bool     `json:"categories"`
	CategoryScores            map[string]float64  `json:"category_scores"`
	CategoryAppliedInputTypes map[string][]string `json:"category_applied_input_types"`
}

// --- handler ---

// ModerationsHandler serves the OpenAI Moderations API (POST /v1/moderations).
// Like EmbeddingsHandler it is engine-free: moderation is a pure deterministic
// function of the input text, so there is no agent/scenario/session to resolve.
type ModerationsHandler struct{ Faults types.SearchFaults }

// Name identifies this adapter in logs and diagnostics.
func (h *ModerationsHandler) Name() string { return "openai-moderations" }

// Routes returns the Moderations route mounted through the adapter Registry.
func (h *ModerationsHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/moderations", Handler: h.HandleModerations},
	}
}

// HandleModerations handles POST /v1/moderations.
func (h *ModerationsHandler) HandleModerations(w http.ResponseWriter, r *http.Request) {
	meta := engine.RequestMetaFromContext(r.Context())
	if meta != nil {
		meta.Protocol = ProtocolOpenAIModerations
	}
	if applyServiceFaults(w, r, h.Faults, func(status int) {
		errType, code := openAIChaosError(status)
		writeErrorCode(w, status, errType, code, "injected moderation fault")
	}) {
		return
	}

	var req ModerationsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()

	inputs, err := parseModerationInputs(req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	model := req.Model
	if model == "" {
		model = defaultModerationModel
	}
	if meta != nil {
		meta.Model = model
	}

	results := make([]ModerationResult, len(inputs))
	for i, text := range inputs {
		results[i] = moderateText(text)
	}
	results = results[:partialResultLimit(h.Faults, len(results))]

	writeJSON(w, http.StatusOK, ModerationsResponse{
		ID:      "modr-" + generateID(),
		Model:   model,
		Results: results,
	})
}

// --- input parsing ---

// moderationInputPart is one element of a content-part input array.
type moderationInputPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// parseModerationInputs decodes the polymorphic `input` into the list of texts
// to classify (one result per element). Accepts a bare string, an array of
// strings, or an array of content parts (text parts contribute their text;
// image_url parts are accepted but treated as benign empty text).
func parseModerationInputs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("input is required")
	}

	// string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil, errors.New("input must not be empty")
		}
		return []string{s}, nil
	}

	// []string
	var ss []string
	if err := json.Unmarshal(raw, &ss); err == nil {
		if len(ss) == 0 {
			return nil, errors.New("input array must not be empty")
		}
		if len(ss) > maxModerationInputs {
			return nil, fmt.Errorf("input array must not exceed %d elements", maxModerationInputs)
		}
		for _, e := range ss {
			if e == "" {
				return nil, errors.New("input must not contain empty strings")
			}
		}
		return ss, nil
	}

	// []content-part
	var parts []moderationInputPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		if len(parts) == 0 {
			return nil, errors.New("input array must not be empty")
		}
		if len(parts) > maxModerationInputs {
			return nil, fmt.Errorf("input array must not exceed %d elements", maxModerationInputs)
		}
		out := make([]string, len(parts))
		for i, p := range parts {
			if p.Type == "text" {
				if p.Text == "" {
					return nil, errors.New("text content part must not be empty")
				}
				out[i] = p.Text
			}
			// image_url (or unknown) parts -> benign empty text.
		}
		return out, nil
	}

	return nil, errors.New("input must be a string, an array of strings, or content parts")
}

// --- scoring ---

// moderateText classifies one input across all categories.
func moderateText(text string) ModerationResult {
	lower := strings.ToLower(text)
	cats := make(map[string]bool, len(allModerationCategories))
	scores := make(map[string]float64, len(allModerationCategories))
	applied := make(map[string][]string)
	flagged := false

	for _, c := range allModerationCategories {
		score := moderationScore(text, lower, c)
		scores[c] = score
		hit := score > moderationFlagThreshold
		cats[c] = hit
		// The real omni-moderation response returns ALL 13 categories in
		// category_applied_input_types (the OpenAI SDK's model declares every
		// field required), so populate each — ["text"] for a text input —
		// regardless of whether it flagged.
		applied[c] = []string{"text"}
		if hit {
			flagged = true
		}
	}
	return ModerationResult{
		Flagged:                   flagged,
		Categories:                cats,
		CategoryScores:            scores,
		CategoryAppliedInputTypes: applied,
	}
}

// moderationScore returns a deterministic score in [0,1) for (text, category):
// a low FNV-seeded baseline (<0.08) for benign text, boosted into [0.55, 0.99)
// when a category keyword matches. Stable across runs/processes. `lower` is the
// pre-lowercased text (passed in to avoid re-lowering per category).
func moderationScore(text, lower, category string) float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(category))
	rng := rand.New(rand.NewPCG(h.Sum64(), 0x9e3779b97f4a7c15))

	base := rng.Float64() * 0.08
	if matchesCategory(lower, category) {
		s := 0.55 + base*5.0
		if s > 0.99 {
			s = 0.99
		}
		return s
	}
	return base
}

// matchesCategory reports whether any keyword for the category appears in the
// (already lowercased) text.
func matchesCategory(lower, category string) bool {
	for _, kw := range moderationKeywords[category] {
		if containsTerm(lower, kw) {
			return true
		}
	}
	return false
}

// containsTerm reports whether term occurs in text anchored at WORD BOUNDARIES,
// so "skill" does not match "kill" and "black marketing" does not match the
// phrase "black market". Internal spaces in a phrase are fine — only the outer
// edges of the match must be at a boundary. text is assumed already lowercased.
// Boundary detection is ASCII-only (isWordByte); a term glued to a non-ASCII
// letter may still match — acceptable for this English-keyword test aid.
func containsTerm(text, term string) bool {
	if term == "" {
		return false
	}
	from := 0
	for {
		i := strings.Index(text[from:], term)
		if i < 0 {
			return false
		}
		i += from
		beforeOK := i == 0 || !isWordByte(text[i-1])
		afterOK := i+len(term) >= len(text) || !isWordByte(text[i+len(term)])
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

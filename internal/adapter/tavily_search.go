package adapter

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"regexp"
	"strings"

	"github.com/mockagents/mockagents/internal/types"
)

const ProtocolTavily = "tavily"

type compiledSearchScenario struct {
	scenario types.SearchScenario
	regex    *regexp.Regexp
}
type TavilySearchHandler struct {
	scenarios []compiledSearchScenario
	faults    types.SearchFaults
}

func NewTavilySearchHandler(scenarios []types.SearchScenario) (*TavilySearchHandler, error) {
	h := &TavilySearchHandler{}
	for _, s := range scenarios {
		c := compiledSearchScenario{scenario: s}
		if s.Match.QueryRegex != "" {
			r, e := regexp.Compile(s.Match.QueryRegex)
			if e != nil {
				return nil, fmt.Errorf("scenario %q: invalid query_regex: %w", s.Name, e)
			}
			c.regex = r
		}
		h.scenarios = append(h.scenarios, c)
	}
	return h, nil
}

func NewTavilySearchService(def *types.SearchServiceDefinition) (*TavilySearchHandler, error) {
	if def == nil {
		return NewTavilySearchHandler(nil)
	}
	h, err := NewTavilySearchHandler(def.Spec.Scenarios)
	if err != nil {
		return nil, err
	}
	h.faults = def.Spec.Faults
	return h, nil
}
func (h *TavilySearchHandler) Name() string { return ProtocolTavily }
func (h *TavilySearchHandler) Routes() []Route {
	return []Route{{Pattern: "POST /search", Handler: h.Search}}
}

type tavilySearchRequest struct {
	Query             string `json:"query"`
	SearchDepth       string `json:"search_depth,omitempty"`
	Topic             string `json:"topic,omitempty"`
	MaxResults        int    `json:"max_results,omitempty"`
	IncludeAnswer     any    `json:"include_answer,omitempty"`
	IncludeRawContent any    `json:"include_raw_content,omitempty"`
}

func (h *TavilySearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	if applyServiceFaults(w, r, h.faults, func(status int) {
		writeJSON(w, status, map[string]any{"detail": map[string]any{"error": "injected search fault"}})
	}) {
		return
	}
	var q tavilySearchRequest
	if !decodeQdrant(w, r, &q) {
		return
	}
	if strings.TrimSpace(q.Query) == "" {
		writeJSON(w, 400, map[string]any{"detail": map[string]any{"error": "query is required"}})
		return
	}
	if q.MaxResults == 0 {
		q.MaxResults = 5
	}
	if q.MaxResults < 1 || q.MaxResults > 20 {
		writeJSON(w, 400, map[string]any{"detail": map[string]any{"error": "max_results must be between 1 and 20"}})
		return
	}
	depth := q.SearchDepth
	if depth == "" {
		depth = "basic"
	}
	if depth != "basic" && depth != "advanced" && depth != "fast" && depth != "ultra-fast" {
		writeJSON(w, 400, map[string]any{"detail": map[string]any{"error": "invalid search_depth"}})
		return
	}
	scenario, ok := h.match(q.Query)
	response := types.SearchResponse{}
	if ok {
		response = scenario.Response
	}
	results := response.Results
	if results == nil {
		results = []types.SearchResult{}
	}
	if len(results) > q.MaxResults {
		results = results[:q.MaxResults]
	}
	results = results[:partialResultLimit(h.faults, len(results))]
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(q.Query))
	writeJSON(w, 200, map[string]any{"query": q.Query, "answer": response.Answer, "images": []any{}, "results": results, "response_time": 0, "usage": map[string]any{"credits": map[bool]int{true: 2, false: 1}[depth == "advanced"]}, "request_id": fmt.Sprintf("search-%016x", hash.Sum64())})
}
func (h *TavilySearchHandler) match(query string) (types.SearchScenario, bool) {
	var fallback *types.SearchScenario
	for i := range h.scenarios {
		s := &h.scenarios[i]
		if s.scenario.Match.Default {
			copy := s.scenario
			fallback = &copy
			continue
		}
		if s.scenario.Match.QueryContains != "" && strings.Contains(strings.ToLower(query), strings.ToLower(s.scenario.Match.QueryContains)) {
			return s.scenario, true
		}
		if s.regex != nil && s.regex.MatchString(query) {
			return s.scenario, true
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return types.SearchScenario{}, false
}

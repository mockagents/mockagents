package adapter

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

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
	now       func() time.Time
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
	Query             string   `json:"query"`
	SearchDepth       string   `json:"search_depth,omitempty"`
	Topic             string   `json:"topic,omitempty"`
	MaxResults        int      `json:"max_results,omitempty"`
	IncludeAnswer     any      `json:"include_answer,omitempty"`
	IncludeRawContent any      `json:"include_raw_content,omitempty"`
	IncludeDomains    []string `json:"include_domains,omitempty"`
	ExcludeDomains    []string `json:"exclude_domains,omitempty"`
	TimeRange         string   `json:"time_range,omitempty"`
	StartDate         string   `json:"start_date,omitempty"`
	EndDate           string   `json:"end_date,omitempty"`
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
	if len(q.IncludeDomains) > 300 || len(q.ExcludeDomains) > 150 {
		writeJSON(w, 400, map[string]any{"detail": map[string]any{"error": "include_domains supports 300 entries and exclude_domains supports 150"}})
		return
	}
	start, end, err := h.dateBounds(q.TimeRange, q.StartDate, q.EndDate)
	if err != nil {
		writeJSON(w, 400, map[string]any{"detail": map[string]any{"error": err.Error()}})
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
	results = filterSearchResults(results, q.IncludeDomains, q.ExcludeDomains, start, end)
	if len(results) > q.MaxResults {
		results = results[:q.MaxResults]
	}
	results = results[:partialResultLimit(h.faults, len(results))]
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(q.Query))
	writeJSON(w, 200, map[string]any{"query": q.Query, "answer": response.Answer, "images": []any{}, "results": results, "response_time": 0, "usage": map[string]any{"credits": map[bool]int{true: 2, false: 1}[depth == "advanced"]}, "request_id": fmt.Sprintf("search-%016x", hash.Sum64())})
}

func (h *TavilySearchHandler) dateBounds(timeRange, startDate, endDate string) (*time.Time, *time.Time, error) {
	parse := func(label, value string) (*time.Time, error) {
		if value == "" {
			return nil, nil
		}
		d, err := time.Parse("2006-01-02", value)
		if err != nil {
			return nil, fmt.Errorf("%s must use YYYY-MM-DD", label)
		}
		return &d, nil
	}
	start, err := parse("start_date", startDate)
	if err != nil {
		return nil, nil, err
	}
	end, err := parse("end_date", endDate)
	if err != nil {
		return nil, nil, err
	}
	if start != nil && end != nil && start.After(*end) {
		return nil, nil, fmt.Errorf("start_date must not be after end_date")
	}
	if timeRange != "" {
		if start != nil || end != nil {
			return nil, nil, fmt.Errorf("time_range cannot be combined with start_date or end_date")
		}
		days := map[string]int{"day": 1, "d": 1, "week": 7, "w": 7, "month": 30, "m": 30, "year": 365, "y": 365}[timeRange]
		if days == 0 {
			return nil, nil, fmt.Errorf("invalid time_range")
		}
		now := time.Now().UTC()
		if h.now != nil {
			now = h.now().UTC()
		}
		boundary := now.AddDate(0, 0, -days)
		start = &boundary
	}
	return start, end, nil
}

func filterSearchResults(results []types.SearchResult, includes, excludes []string, start, end *time.Time) []types.SearchResult {
	out := make([]types.SearchResult, 0, len(results))
	for _, result := range results {
		u, err := url.Parse(result.URL)
		if err != nil || u.Hostname() == "" {
			continue
		}
		if len(includes) > 0 && !matchesAnyDomain(u, includes) {
			continue
		}
		if matchesAnyDomain(u, excludes) {
			continue
		}
		if start != nil || end != nil {
			published, err := time.Parse("2006-01-02", result.PublishedDate)
			if err != nil {
				continue
			}
			if start != nil && !published.After(*start) {
				continue
			}
			if end != nil && !published.Before(*end) {
				continue
			}
		}
		out = append(out, result)
	}
	return out
}

func matchesAnyDomain(u *url.URL, patterns []string) bool {
	host := strings.ToLower(u.Hostname())
	hostPath := host + strings.TrimSuffix(u.EscapedPath(), "/")
	for _, raw := range patterns {
		pattern := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "https://")))
		pattern = strings.TrimPrefix(pattern, "http://")
		pattern = strings.TrimSuffix(pattern, "/")
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(host, suffix) {
				return true
			}
			continue
		}
		if hostPath == pattern || strings.HasPrefix(hostPath, pattern+"/") || host == pattern || strings.HasSuffix(host, "."+pattern) {
			return true
		}
	}
	return false
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

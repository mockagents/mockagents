package adapter

import (
	"bytes"
	"encoding/json"
	"github.com/mockagents/mockagents/internal/types"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTavilyDomainDateFilteringBeforeResultWindow(t *testing.T) {
	results := []types.SearchResult{
		{Title: "old", URL: "https://docs.example.com/old", Score: .9, PublishedDate: "2025-01-01"},
		{Title: "wanted", URL: "https://docs.example.com/new", Score: .8, PublishedDate: "2025-06-15"},
		{Title: "excluded", URL: "https://blog.example.com/new", Score: .7, PublishedDate: "2025-06-16"},
		{Title: "other", URL: "https://other.test/new", Score: .6, PublishedDate: "2025-06-17"},
	}
	h, err := NewTavilySearchHandler([]types.SearchScenario{{Name: "all", Match: types.SearchMatch{Default: true}, Response: types.SearchResponse{Results: results}}})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"query":"news","include_domains":["*.example.com"],"exclude_domains":["blog.example.com"],"start_date":"2025-06-01","end_date":"2025-07-01","max_results":1}`
	w := httptest.NewRecorder()
	h.Search(w, httptest.NewRequest(http.MethodPost, "/search", bytes.NewBufferString(body)))
	var got struct {
		Results []types.SearchResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 || got.Results[0].Title != "wanted" || got.Results[0].PublishedDate != "2025-06-15" {
		t.Fatalf("results=%+v body=%s", got.Results, w.Body.String())
	}
}

func TestTavilyRelativeDateRangeAndValidation(t *testing.T) {
	h, err := NewTavilySearchHandler([]types.SearchScenario{{Name: "all", Match: types.SearchMatch{Default: true}, Response: types.SearchResponse{Results: []types.SearchResult{
		{Title: "recent", URL: "https://example.com/recent", PublishedDate: "2025-07-09"},
		{Title: "old", URL: "https://example.com/old", PublishedDate: "2025-06-01"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	h.now = func() time.Time { return time.Date(2025, 7, 10, 12, 0, 0, 0, time.UTC) }
	w := httptest.NewRecorder()
	h.Search(w, httptest.NewRequest(http.MethodPost, "/search", bytes.NewBufferString(`{"query":"q","time_range":"week"}`)))
	if !bytes.Contains(w.Body.Bytes(), []byte(`"title":"recent"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"title":"old"`)) {
		t.Fatalf("body=%s", w.Body.String())
	}
	for _, body := range []string{`{"query":"q","time_range":"quarter"}`, `{"query":"q","start_date":"07/01/2025"}`, `{"query":"q","start_date":"2025-08-01","end_date":"2025-07-01"}`, `{"query":"q","time_range":"week","start_date":"2025-07-01"}`} {
		w = httptest.NewRecorder()
		h.Search(w, httptest.NewRequest(http.MethodPost, "/search", bytes.NewBufferString(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

func TestTavilyDeclarativeServiceFaults(t *testing.T) {
	def := &types.SearchServiceDefinition{Spec: types.SearchServiceSpec{
		Scenarios: []types.SearchScenario{{Name: "default", Match: types.SearchMatch{Default: true}, Response: types.SearchResponse{Results: []types.SearchResult{{Title: "one", URL: "https://example/1", Score: 1}, {Title: "two", URL: "https://example/2", Score: .5}}}}},
		Faults:    types.SearchFaults{PartialResults: &types.SearchPartialResultsFault{MaxResults: 1}},
	}}
	h, err := NewTavilySearchService(def)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.Search(w, httptest.NewRequest(http.MethodPost, "/search", bytes.NewBufferString(`{"query":"anything"}`)))
	var body struct {
		Results []types.SearchResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Results) != 1 || body.Results[0].Title != "one" {
		t.Fatalf("unexpected partial results: %+v", body.Results)
	}

	def.Spec.Faults = types.SearchFaults{StatusCode: 429}
	h, _ = NewTavilySearchService(def)
	w = httptest.NewRecorder()
	h.Search(w, httptest.NewRequest(http.MethodPost, "/search", bytes.NewBufferString(`{"query":"anything"}`)))
	if w.Code != 429 {
		t.Fatalf("status = %d, want 429", w.Code)
	}

	def.Spec.Faults = types.SearchFaults{MalformedJSON: true}
	h, _ = NewTavilySearchService(def)
	w = httptest.NewRecorder()
	h.Search(w, httptest.NewRequest(http.MethodPost, "/search", bytes.NewBufferString(`{"query":"anything"}`)))
	if json.Valid(w.Body.Bytes()) {
		t.Fatalf("expected malformed JSON, got %q", w.Body.String())
	}
}

func TestTavilySearchMatchesAndTruncates(t *testing.T) {
	h, e := NewTavilySearchHandler([]types.SearchScenario{{Name: "docs", Match: types.SearchMatch{QueryContains: "mockagents"}, Response: types.SearchResponse{Answer: "A mock server", Results: []types.SearchResult{{Title: "one", URL: "https://example/1", Content: "one", Score: .9}, {Title: "two", URL: "https://example/2", Content: "two", Score: .8}}}}, {Name: "fallback", Match: types.SearchMatch{Default: true}}})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest(http.MethodPost, "/search", bytes.NewBufferString(`{"query":"What is MockAgents?","max_results":1}`))
	w := httptest.NewRecorder()
	h.Search(w, r)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"answer":"A mock server"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"title":"two"`)) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
func TestTavilySearchRegexAndSafeCatchAll(t *testing.T) {
	h, e := NewTavilySearchHandler([]types.SearchScenario{{Name: "version", Match: types.SearchMatch{QueryRegex: `(?i)v[0-9]+`}, Response: types.SearchResponse{Results: []types.SearchResult{{Title: "version", URL: "https://example", Content: "v", Score: 1}}}}})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ q, needle string }{{"find v4", `"title":"version"`}, {"unknown", `"results":[]`}} {
		r := httptest.NewRequest("POST", "/search", bytes.NewBufferString(`{"query":"`+tc.q+`"}`))
		w := httptest.NewRecorder()
		h.Search(w, r)
		if !bytes.Contains(w.Body.Bytes(), []byte(tc.needle)) {
			t.Fatalf("%s: %s", tc.q, w.Body.String())
		}
	}
}
func TestTavilyRejectsInvalidRegex(t *testing.T) {
	if _, e := NewTavilySearchHandler([]types.SearchScenario{{Name: "bad", Match: types.SearchMatch{QueryRegex: "["}}}); e == nil {
		t.Fatal("expected regex error")
	}
}

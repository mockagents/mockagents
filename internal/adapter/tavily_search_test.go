package adapter

import (
	"bytes"
	"encoding/json"
	"github.com/mockagents/mockagents/internal/types"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

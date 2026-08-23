package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func TestCohereRerankCommonServiceFaults(t *testing.T) {
	body := `{"model":"m","query":"q","documents":["q one","q two"]}`
	h := &CohereRerankHandler{Faults: types.SearchFaults{PartialResults: &types.SearchPartialResultsFault{MaxResults: 1}}}
	w := httptest.NewRecorder()
	h.Rerank(w, httptest.NewRequest(http.MethodPost, "/v2/rerank", bytes.NewBufferString(body)))
	var got struct {
		Results []cohereRerankResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Results) != 1 {
		t.Fatalf("partial response: %s (%v)", w.Body.String(), err)
	}
	h.Faults = types.SearchFaults{StatusCode: 503}
	w = httptest.NewRecorder()
	h.Rerank(w, httptest.NewRequest(http.MethodPost, "/v2/rerank", bytes.NewBufferString(body)))
	if w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
	h.Faults = types.SearchFaults{MalformedJSON: true}
	w = httptest.NewRecorder()
	h.Rerank(w, httptest.NewRequest(http.MethodPost, "/v2/rerank", bytes.NewBufferString(body)))
	if json.Valid(w.Body.Bytes()) {
		t.Fatalf("expected malformed JSON: %q", w.Body.String())
	}
}

func TestCohereRerankOrdersAndLimitsDeterministically(t *testing.T) {
	h := &CohereRerankHandler{}
	body := `{"model":"rerank-v4.0-fast","query":"capital united states","documents":["Carson City is a capital","Washington is the capital of the United States","unrelated","another unrelated"],"top_n":3}`
	run := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v2/rerank", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.Rerank(w, r)
		return w
	}
	a, b := run(), run()
	if a.Code != 200 || a.Body.String() != b.Body.String() {
		t.Fatalf("responses differ: %d %s / %s", a.Code, a.Body.String(), b.Body.String())
	}
	var got struct {
		Results []cohereRerankResult `json:"results"`
	}
	if err := json.Unmarshal(a.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 3 || got.Results[0].Index != 1 || got.Results[1].Index != 0 || got.Results[2].Index != 2 {
		t.Fatalf("results=%+v", got.Results)
	}
}
func TestCohereRerankValidatesRequest(t *testing.T) {
	for _, body := range []string{`{"query":"q","documents":["d"]}`, `{"model":"m","query":"","documents":["d"]}`, `{"model":"m","query":"q","documents":[],"top_n":0}`} {
		r := httptest.NewRequest(http.MethodPost, "/v2/rerank", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		(&CohereRerankHandler{}).Rerank(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, w.Code)
		}
	}
}

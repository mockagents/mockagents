package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/vector"
)

func pineconeMux(store *vector.Store) *http.ServeMux {
	h := NewPineconeHandler(store)
	mux := http.NewServeMux()
	for _, route := range h.Routes() {
		mux.HandleFunc(route.Pattern, route.Handler)
	}
	return mux
}

func pineconeRequest(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestPineconeDataPlaneFlowAndNamespaces(t *testing.T) {
	store := &vector.Store{}
	if err := store.CreateCollection("docs", 2, vector.Cosine); err != nil {
		t.Fatal(err)
	}
	mux := pineconeMux(store)
	rec := pineconeRequest(t, mux, http.MethodPost, "/pinecone/docs/vectors/upsert", `{"namespace":"support","vectors":[{"id":"b","values":[1,0],"metadata":{"team":"blue"}},{"id":"a","values":[1,0],"metadata":{"team":"blue"}}]}`)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"upsertedCount":2`)) {
		t.Fatalf("upsert: %d %s", rec.Code, rec.Body.String())
	}
	rec = pineconeRequest(t, mux, http.MethodPost, "/pinecone/docs/query", `{"namespace":"support","vector":[1,0],"topK":2,"includeValues":true,"includeMetadata":true,"filter":{"team":{"$eq":"blue"}}}`)
	var query struct {
		Matches []struct {
			ID     string    `json:"id"`
			Values []float64 `json:"values"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &query); err != nil || rec.Code != http.StatusOK || len(query.Matches) != 2 || query.Matches[0].ID != "a" || len(query.Matches[0].Values) != 2 {
		t.Fatalf("query=%+v status=%d err=%v body=%s", query, rec.Code, err, rec.Body.String())
	}
	rec = pineconeRequest(t, mux, http.MethodGet, "/pinecone/docs/vectors/fetch?namespace=support&ids=a", "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"a"`)) {
		t.Fatalf("fetch: %d %s", rec.Code, rec.Body.String())
	}
	rec = pineconeRequest(t, mux, http.MethodPost, "/pinecone/docs/describe_index_stats", `{}`)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"totalVectorCount":2`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"support"`)) {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body.String())
	}
	rec = pineconeRequest(t, mux, http.MethodPost, "/pinecone/docs/vectors/delete", `{"namespace":"support","deleteAll":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPineconeSharesFixtureAndPartialFaultWithQdrant(t *testing.T) {
	store := seededQdrantStore(t)
	limit := 1
	if err := store.SetPartialResultLimit("docs", &limit); err != nil {
		t.Fatal(err)
	}
	rec := pineconeRequest(t, pineconeMux(store), http.MethodPost, "/pinecone/docs/query", `{"vector":[1,0],"topK":2}`)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Mockagents-Vector-Partial") != "true" {
		t.Fatalf("partial: %d %q %s", rec.Code, rec.Header().Get("X-Mockagents-Vector-Partial"), rec.Body.String())
	}
}

func TestPineconeRejectsUnsupportedFilter(t *testing.T) {
	store := &vector.Store{}
	_ = store.CreateCollection("docs", 1, vector.Dot)
	rec := pineconeRequest(t, pineconeMux(store), http.MethodPost, "/pinecone/docs/query", `{"vector":[1],"topK":1,"filter":{"year":{"$gt":2020}}}`)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("only $eq")) {
		t.Fatalf("filter: %d %s", rec.Code, rec.Body.String())
	}
}

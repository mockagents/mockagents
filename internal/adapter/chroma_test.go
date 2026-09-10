package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/vector"
)

func TestChromaQueryProjectsDocumentsURIsAndRequestedFields(t *testing.T) {
	m := chromaMux(&vector.Store{})
	root := "/api/v2/tenants/default_tenant/databases/default_database"
	if r := chromaRequest(t, m, http.MethodPost, root+"/collections", `{"name":"docs","configuration":{"hnsw":{"space":"cosine"}}}`); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	base := root + "/collections/docs"
	if r := chromaRequest(t, m, http.MethodPost, base+"/upsert", `{"ids":["a"],"embeddings":[[1,0]],"documents":["release evidence"],"uris":["file:///evidence"]}`); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	r := chromaRequest(t, m, http.MethodPost, base+"/query", `{"query_embeddings":[[1,0]],"n_results":1,"include":["documents","uris"]}`)
	var body map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := body["documents"].([]any)[0].([]any)[0]; got != "release evidence" {
		t.Fatalf("document=%v", got)
	}
	if got := body["uris"].([]any)[0].([]any)[0]; got != "file:///evidence" {
		t.Fatalf("uri=%v", got)
	}
	if body["embeddings"] != nil || body["metadatas"] != nil || body["distances"] != nil {
		t.Fatalf("unrequested fields leaked: %s", r.Body.String())
	}
}

func TestChromaMetricDefaultsAndKnownL2Distance(t *testing.T) {
	m := chromaMux(&vector.Store{})
	root := "/api/v2/tenants/default_tenant/databases/default_database"
	r := chromaRequest(t, m, http.MethodPost, root+"/collections", `{"name":"l2-default"}`)
	if !bytes.Contains(r.Body.Bytes(), []byte(`"space":"l2"`)) {
		t.Fatalf("default metric: %s", r.Body.String())
	}
	base := root + "/collections/l2-default"
	chromaRequest(t, m, http.MethodPost, base+"/upsert", `{"ids":["a"],"embeddings":[[3,4]]}`)
	r = chromaRequest(t, m, http.MethodPost, base+"/query", `{"query_embeddings":[[0,0]],"n_results":1,"include":["distances"]}`)
	if !bytes.Contains(r.Body.Bytes(), []byte(`"distances":[[25]]`)) {
		t.Fatalf("l2 distance: %s", r.Body.String())
	}
	if bad := chromaRequest(t, m, http.MethodPost, root+"/collections", `{"name":"bad","configuration":{"hnsw":{"space":"manhattan"}}}`); bad.Code != 400 {
		t.Fatalf("invalid metric status=%d", bad.Code)
	}
}

func chromaMux(store *vector.Store) *http.ServeMux {
	h := NewChromaHandler(store)
	mux := http.NewServeMux()
	for _, route := range h.Routes() {
		mux.HandleFunc(route.Pattern, route.Handler)
	}
	return mux
}
func chromaRequest(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestChromaSharedCollectionCRUDAndQuery(t *testing.T) {
	s := &vector.Store{}
	if err := s.CreateCollection("docs", 2, vector.Cosine); err != nil {
		t.Fatal(err)
	}
	m := chromaMux(s)
	base := "/api/v2/tenants/default_tenant/databases/default_database/collections/docs"
	r := chromaRequest(t, m, http.MethodPost, base+"/add", `{"ids":["b","a"],"embeddings":[[1,0],[1,0]],"metadatas":[{"team":"blue"},{"team":"blue"}],"documents":["B","A"]}`)
	if r.Code != http.StatusCreated {
		t.Fatalf("add %d %s", r.Code, r.Body.String())
	}
	r = chromaRequest(t, m, http.MethodPost, base+"/query", `{"query_embeddings":[[1,0]],"n_results":2,"where":{"team":{"$eq":"blue"}},"include":["distances","metadatas"]}`)
	if r.Code != http.StatusOK || !bytes.Contains(r.Body.Bytes(), []byte(`"ids":[["a","b"]]`)) {
		t.Fatalf("query %d %s", r.Code, r.Body.String())
	}
	r = chromaRequest(t, m, http.MethodPost, base+"/get", `{"ids":["a"],"include":["documents","embeddings"]}`)
	if r.Code != http.StatusOK || !bytes.Contains(r.Body.Bytes(), []byte(`"A"`)) {
		t.Fatalf("get %d %s", r.Code, r.Body.String())
	}
	r = chromaRequest(t, m, http.MethodGet, base+"/count", "")
	if r.Code != http.StatusOK || r.Body.String() != "2\n" {
		t.Fatalf("count %d %s", r.Code, r.Body.String())
	}
	r = chromaRequest(t, m, http.MethodPost, base+"/delete", `{"ids":["a"]}`)
	if r.Code != http.StatusOK || !bytes.Contains(r.Body.Bytes(), []byte(`"deleted":1`)) {
		t.Fatalf("delete %d %s", r.Code, r.Body.String())
	}
}

func TestChromaPartialFaultSignal(t *testing.T) {
	s := seededQdrantStore(t)
	n := 1
	_ = s.SetPartialResultLimit("docs", &n)
	base := "/api/v2/tenants/default_tenant/databases/default_database/collections/docs/query"
	r := chromaRequest(t, chromaMux(s), http.MethodPost, base, `{"query_embeddings":[[1,0]],"n_results":2}`)
	if r.Header().Get("X-Mockagents-Vector-Partial") != "true" {
		t.Fatalf("partial header=%q", r.Header().Get("X-Mockagents-Vector-Partial"))
	}
}

func TestChromaCollectionLifecycle(t *testing.T) {
	m := chromaMux(&vector.Store{})
	root := "/api/v2/tenants/default_tenant/databases/default_database"
	r := chromaRequest(t, m, http.MethodPost, root+"/collections", `{"name":"new-docs"}`)
	if r.Code != http.StatusOK || !bytes.Contains(r.Body.Bytes(), []byte(`"dimension":0`)) {
		t.Fatalf("create %d %s", r.Code, r.Body.String())
	}
	r = chromaRequest(t, m, http.MethodPost, root+"/collections/new-docs/upsert", `{"ids":["one"],"embeddings":[[1,2,3]]}`)
	if r.Code != http.StatusOK {
		t.Fatalf("upsert %d %s", r.Code, r.Body.String())
	}
	r = chromaRequest(t, m, http.MethodGet, root+"/collections", "")
	if r.Code != http.StatusOK || !bytes.Contains(r.Body.Bytes(), []byte(`"new-docs"`)) {
		t.Fatalf("list %d %s", r.Code, r.Body.String())
	}
	r = chromaRequest(t, m, http.MethodGet, root+"/collections_count", "")
	if r.Body.String() != "1\n" {
		t.Fatalf("count collections %s", r.Body.String())
	}
	r = chromaRequest(t, m, http.MethodDelete, root+"/collections/new-docs", "")
	if r.Code != http.StatusOK {
		t.Fatalf("delete collection %d %s", r.Code, r.Body.String())
	}
}

package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/vector"
)

func qdrantMux() *http.ServeMux {
	h := NewQdrantHandler(&vector.Store{})
	mux := http.NewServeMux()
	for _, route := range h.Routes() {
		mux.HandleFunc(route.Pattern, route.Handler)
	}
	return mux
}

func qdrantRequest(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestQdrantCollectionPointAndSearchFlow(t *testing.T) {
	mux := qdrantMux()
	rec := qdrantRequest(t, mux, http.MethodPut, "/collections/docs", `{
		"vectors":{"size":2,"distance":"Cosine"}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = qdrantRequest(t, mux, http.MethodPut, "/collections/docs/points?wait=true", `{
		"points":[
			{"id":2,"vector":[1,0],"payload":{"team":"blue"}},
			{"id":1,"vector":[1,0],"payload":{"team":"blue"}},
			{"id":"red","vector":[0,1],"payload":{"team":"red"}}
		]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = qdrantRequest(t, mux, http.MethodPost, "/collections/docs/points/search", `{
		"vector":[1,0],"limit":3,
		"filter":{"must":[{"key":"team","match":{"value":"blue"}}]}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Result []struct {
			ID    any     `json:"id"`
			Score float64 `json:"score"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Result) != 2 || response.Result[0].ID != float64(1) || response.Result[1].ID != float64(2) {
		t.Fatalf("stable result = %+v", response.Result)
	}

	rec = qdrantRequest(t, mux, http.MethodGet, "/collections/docs", "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"points_count":3`)) {
		t.Fatalf("collection: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQdrantFailureModes(t *testing.T) {
	mux := qdrantMux()
	rec := qdrantRequest(t, mux, http.MethodPost, "/collections/missing/points/search", `{"vector":[1],"limit":1}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing collection status=%d body=%s", rec.Code, rec.Body.String())
	}

	qdrantRequest(t, mux, http.MethodPut, "/collections/docs", `{"vectors":{"size":2,"distance":"Dot"}}`)
	rec = qdrantRequest(t, mux, http.MethodPut, "/collections/docs/points", `{"points":[{"id":1,"vector":[1]}]}`)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("dimension mismatch")) {
		t.Fatalf("dimension status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = qdrantRequest(t, mux, http.MethodPost, "/collections/docs/points/search", `{"vector":[1,0],"limit":1001}`)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("top-k")) {
		t.Fatalf("top-k status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQdrantSearchSignalsConfiguredPartialResults(t *testing.T) {
	store := seededQdrantStore(t)
	limit := 1
	if err := store.SetPartialResultLimit("docs", &limit); err != nil {
		t.Fatal(err)
	}
	h := NewQdrantHandler(store)
	mux := http.NewServeMux()
	for _, route := range h.Routes() {
		mux.HandleFunc(route.Pattern, route.Handler)
	}
	rec := qdrantRequest(t, mux, http.MethodPost, "/collections/docs/points/search", `{"vector":[1,0],"limit":3}`)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Mockagents-Vector-Partial") != "true" {
		t.Fatalf("status=%d partial=%q body=%s", rec.Code, rec.Header().Get("X-Mockagents-Vector-Partial"), rec.Body.String())
	}
	if rec.Header().Get("X-Mockagents-Chaos-Action") != "partial" || rec.Header().Get("X-Mockagents-Chaos-Source") != "configured" {
		t.Fatalf("chaos action=%q source=%q", rec.Header().Get("X-Mockagents-Chaos-Action"), rec.Header().Get("X-Mockagents-Chaos-Source"))
	}
	var body struct {
		Result []any `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Result) != 1 {
		t.Fatalf("result=%+v err=%v", body.Result, err)
	}
}

func seededQdrantStore(t *testing.T) *vector.Store {
	t.Helper()
	store := &vector.Store{}
	if err := store.CreateCollection("docs", 2, vector.Cosine); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert("docs", []vector.Point{{ID: "a", Vector: []float64{1, 0}}, {ID: "b", Vector: []float64{.9, .1}}}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestQdrantFetchAndDeletePreserveIDType(t *testing.T) {
	mux := qdrantMux()
	qdrantRequest(t, mux, http.MethodPut, "/collections/docs", `{"vectors":{"size":1,"distance":"Euclid"}}`)
	qdrantRequest(t, mux, http.MethodPut, "/collections/docs/points", `{"points":[{"id":"doc-1","vector":[1]}]}`)
	rec := qdrantRequest(t, mux, http.MethodPost, "/collections/docs/points", `{"points":["doc-1"]}`)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"id":"doc-1"`)) {
		t.Fatalf("fetch status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = qdrantRequest(t, mux, http.MethodPost, "/collections/docs/points/delete", `{"points":["doc-1"]}`)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"affected":1`)) {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQdrantCollectionsAreTenantIsolated(t *testing.T) {
	h := NewQdrantHandler(&vector.Store{})
	create := func(tenant string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/collections/docs",
			bytes.NewBufferString(`{"vectors":{"size":1,"distance":"Cosine"}}`))
		req.SetPathValue("collection", "docs")
		req = req.WithContext(engine.WithTenantID(context.Background(), tenant))
		rec := httptest.NewRecorder()
		h.CreateCollection(rec, req)
		return rec
	}
	if rec := create("tenant-a"); rec.Code != http.StatusOK {
		t.Fatalf("tenant-a create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := create("tenant-b"); rec.Code != http.StatusOK {
		t.Fatalf("tenant-b create collided/leaked: %d %s", rec.Code, rec.Body.String())
	}
}

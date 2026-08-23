package adapter

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/vector"
)

const ProtocolChroma = "chroma"

type ChromaHandler struct{ Store *vector.Store }

func NewChromaHandler(s *vector.Store) *ChromaHandler {
	if s == nil {
		s = &vector.Store{}
	}
	return &ChromaHandler{Store: s}
}
func (h *ChromaHandler) Name() string { return ProtocolChroma }
func (h *ChromaHandler) Routes() []Route {
	const p = "/api/v2/tenants/{tenant}/databases/{database}/collections/{collection}"
	return []Route{
		{Pattern: "GET /api/v2/heartbeat", Handler: h.Heartbeat}, {Pattern: "GET " + p, Handler: h.GetCollection},
		{Pattern: "GET /api/v2/tenants/{tenant}/databases/{database}/collections", Handler: h.ListCollections},
		{Pattern: "POST /api/v2/tenants/{tenant}/databases/{database}/collections", Handler: h.CreateCollection},
		{Pattern: "GET /api/v2/tenants/{tenant}/databases/{database}/collections_count", Handler: h.CountCollections},
		{Pattern: "DELETE " + p, Handler: h.DeleteCollection},
		{Pattern: "POST " + p + "/add", Handler: h.Add}, {Pattern: "POST " + p + "/upsert", Handler: h.Upsert},
		{Pattern: "POST " + p + "/get", Handler: h.Get}, {Pattern: "POST " + p + "/query", Handler: h.Query},
		{Pattern: "POST " + p + "/delete", Handler: h.Delete}, {Pattern: "GET " + p + "/count", Handler: h.Count},
	}
}

type chromaWrite struct {
	IDs        []string         `json:"ids"`
	Embeddings [][]float64      `json:"embeddings"`
	Metadatas  []map[string]any `json:"metadatas"`
	Documents  []*string        `json:"documents"`
	URIs       []*string        `json:"uris"`
}
type chromaGet struct {
	IDs     []string       `json:"ids"`
	Where   map[string]any `json:"where"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
	Include []string       `json:"include"`
}
type chromaQuery struct {
	QueryEmbeddings [][]float64    `json:"query_embeddings"`
	NResults        int            `json:"n_results"`
	Where           map[string]any `json:"where"`
	Include         []string       `json:"include"`
}
type chromaCreate struct {
	Name          string         `json:"name"`
	GetOrCreate   bool           `json:"get_or_create"`
	Metadata      map[string]any `json:"metadata"`
	Configuration map[string]any `json:"configuration"`
}

func (h *ChromaHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"nanosecond heartbeat": 0})
}
func (h *ChromaHandler) GetCollection(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	c, e := h.Store.Collection(h.key(r))
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, h.collectionJSON(r, c))
}
func (h *ChromaHandler) CreateCollection(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var q chromaCreate
	if !decodeQdrant(w, r, &q) {
		return
	}
	if strings.TrimSpace(q.Name) == "" {
		h.err(w, errors.New("name is required"))
		return
	}
	key := h.keyFor(r, q.Name)
	e := h.Store.CreatePendingCollection(key, vector.Cosine)
	if e != nil && q.GetOrCreate && errors.Is(e, vector.ErrCollectionExists) {
		e = nil
	}
	if e != nil {
		h.err(w, e)
		return
	}
	r.SetPathValue("collection", q.Name)
	h.GetCollection(w, r)
}
func (h *ChromaHandler) DeleteCollection(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	if e := h.Store.DeleteCollection(h.key(r)); e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{})
}
func (h *ChromaHandler) ListCollections(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	out := []any{}
	for _, name := range h.collectionNames(r) {
		rr := r.Clone(r.Context())
		rr.SetPathValue("collection", name)
		c, e := h.Store.Collection(h.key(rr))
		if e == nil {
			out = append(out, h.collectionJSON(rr, c))
		}
	}
	writeJSON(w, 200, out)
}
func (h *ChromaHandler) CountCollections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, len(h.collectionNames(r)))
}
func (h *ChromaHandler) Add(w http.ResponseWriter, r *http.Request)    { h.write(w, r, false) }
func (h *ChromaHandler) Upsert(w http.ResponseWriter, r *http.Request) { h.write(w, r, true) }
func (h *ChromaHandler) write(w http.ResponseWriter, r *http.Request, upsert bool) {
	h.stamp(r)
	var q chromaWrite
	if !decodeQdrant(w, r, &q) {
		return
	}
	if len(q.IDs) == 0 || len(q.Embeddings) != len(q.IDs) {
		h.err(w, errors.New("ids and embeddings must have equal non-zero lengths"))
		return
	}
	ids := make([]string, len(q.IDs))
	for i, id := range q.IDs {
		ids[i] = "s:" + id
	}
	if !upsert {
		old, e := h.Store.Fetch(h.key(r), ids)
		if e != nil {
			h.err(w, e)
			return
		}
		if len(old) > 0 {
			h.err(w, errors.New("record id already exists"))
			return
		}
	}
	pts := make([]vector.Point, len(ids))
	for i := range ids {
		m := map[string]any{}
		if i < len(q.Metadatas) && q.Metadatas[i] != nil {
			for k, v := range q.Metadatas[i] {
				m[k] = v
			}
		}
		if i < len(q.Documents) && q.Documents[i] != nil {
			m["__chroma_document"] = *q.Documents[i]
		}
		if i < len(q.URIs) && q.URIs[i] != nil {
			m["__chroma_uri"] = *q.URIs[i]
		}
		pts[i] = vector.Point{ID: ids[i], ExternalID: q.IDs[i], Vector: q.Embeddings[i], Metadata: m}
	}
	if e := h.Store.Upsert(h.key(r), pts); e != nil {
		h.err(w, e)
		return
	}
	status := 200
	if !upsert {
		status = 201
	}
	writeJSON(w, status, map[string]any{})
}
func (h *ChromaHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var q chromaGet
	if !decodeQdrant(w, r, &q) {
		return
	}
	f, e := pineconeFilter(q.Where)
	if e != nil {
		h.err(w, e)
		return
	}
	var pts []vector.Point
	if len(q.IDs) > 0 {
		ids := make([]string, len(q.IDs))
		for i, id := range q.IDs {
			ids[i] = "s:" + id
		}
		pts, e = h.Store.Fetch(h.key(r), ids)
	} else {
		pts, e = h.Store.List(h.key(r), f, q.Limit, q.Offset)
	}
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, chromaRows(pts, q.Include))
}
func (h *ChromaHandler) Query(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var q chromaQuery
	if !decodeQdrant(w, r, &q) {
		return
	}
	if q.NResults == 0 {
		q.NResults = 10
	}
	f, e := pineconeFilter(q.Where)
	if e != nil {
		h.err(w, e)
		return
	}
	ids := [][]string{}
	dists := [][]float64{}
	metas := [][]map[string]any{}
	embeds := [][][]float64{}
	for _, v := range q.QueryEmbeddings {
		res, x := h.Store.QueryWithInfo(h.key(r), vectorChaosQuery(r, vector.Query{Vector: v, TopK: q.NResults, Filter: f}))
		if x != nil {
			h.err(w, x)
			return
		}
		stampVectorChaos(w, res)
		ii := []string{}
		dd := []float64{}
		mm := []map[string]any{}
		ee := [][]float64{}
		for _, m := range res.Matches {
			ii = append(ii, fmt.Sprint(externalID(m.ID, m.ExternalID)))
			dd = append(dd, 1-m.Score)
			mm = append(mm, chromaMetadata(m.Metadata))
			p, _ := h.Store.Fetch(h.key(r), []string{m.ID})
			if len(p) > 0 {
				ee = append(ee, p[0].Vector)
			}
		}
		ids = append(ids, ii)
		dists = append(dists, dd)
		metas = append(metas, mm)
		embeds = append(embeds, ee)
	}
	writeJSON(w, 200, map[string]any{"ids": ids, "distances": dists, "metadatas": metas, "embeddings": embeds, "documents": nil, "uris": nil, "include": q.Include})
}
func (h *ChromaHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var q chromaGet
	if !decodeQdrant(w, r, &q) {
		return
	}
	if len(q.IDs) == 0 {
		h.err(w, errors.New("ids are required"))
		return
	}
	ids := make([]string, len(q.IDs))
	for i, id := range q.IDs {
		ids[i] = "s:" + id
	}
	n, e := h.Store.Delete(h.key(r), ids)
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}
func (h *ChromaHandler) Count(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	c, e := h.Store.Collection(h.key(r))
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, c.PointCount)
}
func (h *ChromaHandler) key(r *http.Request) string {
	return h.keyFor(r, r.PathValue("collection"))
}
func (h *ChromaHandler) keyFor(r *http.Request, name string) string {
	base := vector.ScopedCollectionName(engine.TenantIDFromContext(r.Context()), name)
	if r.PathValue("tenant") == "default_tenant" && r.PathValue("database") == "default_database" {
		return base
	}
	return base + "\x02" + r.PathValue("tenant") + "\x02" + r.PathValue("database")
}
func (h *ChromaHandler) collectionJSON(r *http.Request, c vector.CollectionConfig) map[string]any {
	return map[string]any{"id": r.PathValue("collection"), "name": r.PathValue("collection"), "tenant": r.PathValue("tenant"), "database": r.PathValue("database"), "dimension": c.Dimension, "metadata": map[string]any{}, "version": 0, "log_position": 0, "configuration_json": map[string]any{"hnsw": map[string]any{"space": c.Metric}}}
}
func (h *ChromaHandler) collectionNames(r *http.Request) []string {
	auth := engine.TenantIDFromContext(r.Context())
	prefix := ""
	if auth != "" {
		prefix = auth + "\x00"
	}
	suffix := ""
	if r.PathValue("tenant") != "default_tenant" || r.PathValue("database") != "default_database" {
		suffix = "\x02" + r.PathValue("tenant") + "\x02" + r.PathValue("database")
	}
	names := []string{}
	for _, c := range h.Store.Collections() {
		n := c.Name
		if !strings.HasPrefix(n, prefix) {
			continue
		}
		n = strings.TrimPrefix(n, prefix)
		if suffix == "" {
			if strings.ContainsAny(n, "\x00\x01\x02") {
				continue
			}
			names = append(names, n)
		} else if strings.HasSuffix(n, suffix) {
			names = append(names, strings.TrimSuffix(n, suffix))
		}
	}
	return names
}
func (h *ChromaHandler) stamp(r *http.Request) {
	if m := engine.RequestMetaFromContext(r.Context()); m != nil {
		m.Protocol = ProtocolChroma
	}
}
func (h *ChromaHandler) err(w http.ResponseWriter, e error) {
	s := 400
	if errors.Is(e, vector.ErrCollectionNotFound) {
		s = 404
	}
	writeJSON(w, s, map[string]any{"error": e.Error(), "message": e.Error()})
}
func chromaMetadata(m map[string]any) map[string]any {
	o := map[string]any{}
	for k, v := range m {
		if !strings.HasPrefix(k, "__chroma_") {
			o[k] = v
		}
	}
	return o
}
func chromaRows(p []vector.Point, include []string) map[string]any {
	ids := []string{}
	meta := []map[string]any{}
	emb := [][]float64{}
	docs := []any{}
	uris := []any{}
	for _, x := range p {
		ids = append(ids, fmt.Sprint(externalID(x.ID, x.ExternalID)))
		meta = append(meta, chromaMetadata(x.Metadata))
		emb = append(emb, x.Vector)
		docs = append(docs, x.Metadata["__chroma_document"])
		uris = append(uris, x.Metadata["__chroma_uri"])
	}
	return map[string]any{"ids": ids, "metadatas": meta, "embeddings": emb, "documents": docs, "uris": uris, "include": include}
}

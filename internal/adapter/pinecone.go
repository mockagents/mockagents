package adapter

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/vector"
)

const ProtocolPinecone = "pinecone"

// PineconeHandler exposes Pinecone's host-targeted data-plane API below a
// per-index path prefix so multiple deterministic indexes fit on one server.
type PineconeHandler struct{ Store *vector.Store }

func NewPineconeHandler(store *vector.Store) *PineconeHandler {
	if store == nil {
		store = &vector.Store{}
	}
	return &PineconeHandler{Store: store}
}
func (h *PineconeHandler) Name() string { return "pinecone" }
func (h *PineconeHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /pinecone/{index}/vectors/upsert", Handler: h.Upsert},
		{Pattern: "POST /pinecone/{index}/query", Handler: h.Query},
		{Pattern: "GET /pinecone/{index}/vectors/fetch", Handler: h.Fetch},
		{Pattern: "POST /pinecone/{index}/vectors/delete", Handler: h.Delete},
		{Pattern: "POST /pinecone/{index}/describe_index_stats", Handler: h.DescribeIndexStats},
	}
}

type pineconeVector struct {
	ID       string         `json:"id"`
	Values   []float64      `json:"values"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
type pineconeUpsertRequest struct {
	Vectors   []pineconeVector `json:"vectors"`
	Namespace string           `json:"namespace,omitempty"`
}
type pineconeQueryRequest struct {
	Vector          []float64      `json:"vector"`
	TopK            int            `json:"topK"`
	Namespace       string         `json:"namespace,omitempty"`
	Filter          map[string]any `json:"filter,omitempty"`
	IncludeValues   bool           `json:"includeValues,omitempty"`
	IncludeMetadata bool           `json:"includeMetadata,omitempty"`
}
type pineconeDeleteRequest struct {
	IDs       []string `json:"ids,omitempty"`
	DeleteAll bool     `json:"deleteAll,omitempty"`
	Namespace string   `json:"namespace,omitempty"`
}

func (h *PineconeHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var req pineconeUpsertRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	if len(req.Vectors) == 0 {
		h.writeError(w, errors.New("vectors must contain at least one item"))
		return
	}
	key, err := h.namespaceKey(r, req.Namespace, true)
	if err != nil {
		h.writeError(w, err)
		return
	}
	points := make([]vector.Point, len(req.Vectors))
	for i, v := range req.Vectors {
		if strings.TrimSpace(v.ID) == "" {
			h.writeError(w, fmt.Errorf("vector %d: id is required", i))
			return
		}
		points[i] = vector.Point{ID: "s:" + v.ID, ExternalID: v.ID, Vector: v.Values, Metadata: v.Metadata}
	}
	if err := h.Store.Upsert(key, points); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"upsertedCount": len(points)})
}

func (h *PineconeHandler) Query(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var req pineconeQueryRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	filter, err := pineconeFilter(req.Filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	key, err := h.namespaceKey(r, req.Namespace, false)
	if err != nil {
		h.writeError(w, err)
		return
	}
	result, err := h.Store.QueryWithInfo(key, vectorChaosQuery(r, vector.Query{Vector: req.Vector, TopK: req.TopK, Filter: filter}))
	if err != nil {
		h.writeError(w, err)
		return
	}
	stampVectorChaos(w, result)
	matches := make([]map[string]any, len(result.Matches))
	for i, match := range result.Matches {
		item := map[string]any{"id": externalID(match.ID, match.ExternalID), "score": match.Score}
		if req.IncludeMetadata {
			item["metadata"] = match.Metadata
		}
		if req.IncludeValues {
			points, _ := h.Store.Fetch(key, []string{match.ID})
			if len(points) == 1 {
				item["values"] = points[0].Vector
			}
		}
		matches[i] = item
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches, "namespace": req.Namespace, "usage": map[string]any{"readUnits": 1}})
}

func (h *PineconeHandler) Fetch(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	namespace := r.URL.Query().Get("namespace")
	key, err := h.namespaceKey(r, namespace, false)
	if err != nil {
		h.writeError(w, err)
		return
	}
	ids := r.URL.Query()["ids"]
	internal := make([]string, len(ids))
	for i, id := range ids {
		internal[i] = "s:" + id
	}
	points, err := h.Store.Fetch(key, internal)
	if err != nil {
		h.writeError(w, err)
		return
	}
	vectors := make(map[string]any, len(points))
	for _, p := range points {
		id := fmt.Sprint(externalID(p.ID, p.ExternalID))
		vectors[id] = map[string]any{"id": id, "values": p.Vector, "metadata": p.Metadata}
	}
	writeJSON(w, http.StatusOK, map[string]any{"vectors": vectors, "namespace": namespace, "usage": map[string]any{"readUnits": 1}})
}

func (h *PineconeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var req pineconeDeleteRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	key, err := h.namespaceKey(r, req.Namespace, false)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if req.DeleteAll {
		_, err = h.Store.DeleteAll(key)
	} else {
		ids := make([]string, len(req.IDs))
		for i, id := range req.IDs {
			ids[i] = "s:" + id
		}
		_, err = h.Store.Delete(key, ids)
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *PineconeHandler) DescribeIndexStats(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	base := vector.ScopedCollectionName(engine.TenantIDFromContext(r.Context()), r.PathValue("index"))
	cfg, err := h.Store.Collection(base)
	if err != nil {
		h.writeError(w, err)
		return
	}
	total := 0
	namespaces := make(map[string]any)
	for _, collection := range h.Store.CollectionsWithPrefix(base) {
		namespace := strings.TrimPrefix(strings.TrimPrefix(collection.Name, base), "\x01")
		namespaces[namespace] = map[string]any{"vectorCount": collection.PointCount}
		total += collection.PointCount
	}
	writeJSON(w, http.StatusOK, map[string]any{"dimension": cfg.Dimension, "indexFullness": 0, "totalVectorCount": total, "metric": cfg.Metric, "vectorType": "dense", "namespaces": namespaces})
}

func (h *PineconeHandler) namespaceKey(r *http.Request, namespace string, create bool) (string, error) {
	base := vector.ScopedCollectionName(engine.TenantIDFromContext(r.Context()), r.PathValue("index"))
	if namespace == "" {
		return base, nil
	}
	key := base + "\x01" + namespace
	if _, err := h.Store.Collection(key); err == nil {
		return key, nil
	} else if !create || !errors.Is(err, vector.ErrCollectionNotFound) {
		return "", err
	}
	cfg, err := h.Store.Collection(base)
	if err != nil {
		return "", err
	}
	if err := h.Store.CreateCollection(key, cfg.Dimension, cfg.Metric); err != nil && !errors.Is(err, vector.ErrCollectionExists) {
		return "", err
	}
	return key, nil
}
func (h *PineconeHandler) stamp(r *http.Request) {
	if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
		meta.Protocol = ProtocolPinecone
	}
}
func (h *PineconeHandler) writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, vector.ErrCollectionNotFound) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]any{"code": "INVALID_ARGUMENT", "message": err.Error()})
}

func pineconeFilter(in map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(in))
	for key, value := range in {
		if strings.HasPrefix(key, "$") {
			return nil, fmt.Errorf("logical filter operator %q is not supported", key)
		}
		if op, ok := value.(map[string]any); ok {
			eq, exists := op["$eq"]
			if !exists || len(op) != 1 {
				return nil, fmt.Errorf("filter %q supports only $eq", key)
			}
			value = eq
		}
		out[key] = value
	}
	return out, nil
}

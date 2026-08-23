package adapter

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/vector"
)

const ProtocolQdrant = "qdrant"

// QdrantHandler exposes a deterministic, in-memory subset of the Qdrant HTTP
// API. It never calls an upstream and is intentionally process-local.
type QdrantHandler struct{ Store *vector.Store }

func NewQdrantHandler(store *vector.Store) *QdrantHandler {
	if store == nil {
		store = &vector.Store{}
	}
	return &QdrantHandler{Store: store}
}

func (h *QdrantHandler) Name() string { return "qdrant" }

func (h *QdrantHandler) Routes() []Route {
	return []Route{
		{Pattern: "PUT /collections/{collection}", Handler: h.CreateCollection},
		{Pattern: "GET /collections/{collection}", Handler: h.GetCollection},
		{Pattern: "DELETE /collections/{collection}", Handler: h.DeleteCollection},
		{Pattern: "PUT /collections/{collection}/points", Handler: h.UpsertPoints},
		{Pattern: "POST /collections/{collection}/points", Handler: h.FetchPoints},
		{Pattern: "POST /collections/{collection}/points/delete", Handler: h.DeletePoints},
		{Pattern: "POST /collections/{collection}/points/search", Handler: h.SearchPoints},
	}
}

type qdrantVectorsConfig struct {
	Size     int    `json:"size"`
	Distance string `json:"distance"`
}

type qdrantCreateRequest struct {
	Vectors qdrantVectorsConfig `json:"vectors"`
}

type qdrantPoint struct {
	ID      any            `json:"id"`
	Vector  []float64      `json:"vector"`
	Payload map[string]any `json:"payload,omitempty"`
}

type qdrantPointsRequest struct {
	Points []qdrantPoint `json:"points"`
}

type qdrantIDsRequest struct {
	Points []any `json:"points"`
}

type qdrantSearchRequest struct {
	Vector         []float64      `json:"vector"`
	Limit          int            `json:"limit"`
	ScoreThreshold *float64       `json:"score_threshold,omitempty"`
	Filter         map[string]any `json:"filter,omitempty"`
	WithPayload    *bool          `json:"with_payload,omitempty"`
}

func (h *QdrantHandler) CreateCollection(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var req qdrantCreateRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	metric, err := qdrantMetric(req.Vectors.Distance)
	if err == nil {
		err = h.Store.CreateCollection(qdrantCollectionKey(r), req.Vectors.Size, metric)
	}
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": true, "status": "ok", "time": 0})
}

func (h *QdrantHandler) GetCollection(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	config, err := h.Store.Collection(qdrantCollectionKey(r))
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"result": map[string]any{
			"status": "green", "points_count": config.PointCount, "vectors_count": config.PointCount,
			"config": map[string]any{"params": map[string]any{"vectors": map[string]any{
				"size": config.Dimension, "distance": qdrantDistance(config.Metric),
			}}},
		},
		"status": "ok", "time": 0,
	})
}

func (h *QdrantHandler) DeleteCollection(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	if err := h.Store.DeleteCollection(qdrantCollectionKey(r)); err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": true, "status": "ok", "time": 0})
}

func (h *QdrantHandler) UpsertPoints(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var req qdrantPointsRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	points := make([]vector.Point, len(req.Points))
	for i, point := range req.Points {
		key, err := qdrantPointKey(point.ID)
		if err != nil {
			writeQdrantError(w, http.StatusBadRequest, err.Error())
			return
		}
		points[i] = vector.Point{ID: key, ExternalID: point.ID, Vector: point.Vector, Metadata: point.Payload}
	}
	if err := h.Store.Upsert(qdrantCollectionKey(r), points); err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, qdrantOperationResult(len(points)))
}

func (h *QdrantHandler) FetchPoints(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var req qdrantIDsRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	ids, ok := qdrantKeys(w, req.Points)
	if !ok {
		return
	}
	points, err := h.Store.Fetch(qdrantCollectionKey(r), ids)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	result := make([]qdrantPoint, len(points))
	for i, point := range points {
		result[i] = qdrantPoint{ID: externalID(point.ID, point.ExternalID), Vector: point.Vector, Payload: point.Metadata}
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result, "status": "ok", "time": 0})
}

func (h *QdrantHandler) DeletePoints(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var req qdrantIDsRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	ids, ok := qdrantKeys(w, req.Points)
	if !ok {
		return
	}
	deleted, err := h.Store.Delete(qdrantCollectionKey(r), ids)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, qdrantOperationResult(deleted))
}

func (h *QdrantHandler) SearchPoints(w http.ResponseWriter, r *http.Request) {
	h.stamp(r)
	var req qdrantSearchRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	filter, err := flattenQdrantFilter(req.Filter)
	if err != nil {
		writeQdrantError(w, http.StatusBadRequest, err.Error())
		return
	}
	queryResult, err := h.Store.QueryWithInfo(qdrantCollectionKey(r), vectorChaosQuery(r, vector.Query{
		Vector: req.Vector, TopK: req.Limit, Filter: filter, MinScore: req.ScoreThreshold,
	}))
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	stampVectorChaos(w, queryResult)
	matches := queryResult.Matches
	withPayload := req.WithPayload == nil || *req.WithPayload
	result := make([]map[string]any, len(matches))
	for i, match := range matches {
		result[i] = map[string]any{"id": externalID(match.ID, match.ExternalID), "score": match.Score, "version": 0}
		if withPayload {
			result[i]["payload"] = match.Metadata
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result, "status": "ok", "time": 0})
}

func (h *QdrantHandler) stamp(r *http.Request) {
	if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
		meta.Protocol = ProtocolQdrant
	}
}

// qdrantCollectionKey isolates mutable vector state by authenticated tenant.
// NUL cannot occur in a server-generated tenant id, so tenants cannot forge a
// key that lands in another namespace. Anonymous single-tenant mode keeps the
// human-readable collection name unchanged.
func qdrantCollectionKey(r *http.Request) string {
	return vector.ScopedCollectionName(engine.TenantIDFromContext(r.Context()), r.PathValue("collection"))
}

func (h *QdrantHandler) writeStoreError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, vector.ErrCollectionNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, vector.ErrCollectionExists) {
		status = http.StatusConflict
	}
	writeQdrantError(w, status, err.Error())
}

func decodeQdrant(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := decodeJSONBody(r, dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeQdrantError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeQdrantError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		}
		return false
	}
	return true
}

func writeQdrantError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"status": map[string]any{"error": message}, "result": nil, "time": 0})
}

func qdrantMetric(distance string) (vector.Metric, error) {
	switch strings.ToLower(distance) {
	case "cosine":
		return vector.Cosine, nil
	case "dot":
		return vector.Dot, nil
	case "euclid", "euclidean":
		return vector.Euclidean, nil
	default:
		return "", fmt.Errorf("unsupported distance %q", distance)
	}
}

func qdrantDistance(metric vector.Metric) string {
	switch metric {
	case vector.Dot:
		return "Dot"
	case vector.Euclidean:
		return "Euclid"
	default:
		return "Cosine"
	}
}

func qdrantPointKey(id any) (string, error) {
	switch value := id.(type) {
	case string:
		if value == "" {
			return "", errors.New("point id must not be empty")
		}
		return "s:" + value, nil
	case float64:
		if value < 0 || value != math.Trunc(value) || value > math.MaxInt64 {
			return "", errors.New("numeric point id must be a non-negative integer")
		}
		return "n:" + strconv.FormatUint(uint64(value), 10), nil
	default:
		return "", errors.New("point id must be a string or non-negative integer")
	}
}

func qdrantKeys(w http.ResponseWriter, raw []any) ([]string, bool) {
	ids := make([]string, len(raw))
	for i, id := range raw {
		key, err := qdrantPointKey(id)
		if err != nil {
			writeQdrantError(w, http.StatusBadRequest, err.Error())
			return nil, false
		}
		ids[i] = key
	}
	return ids, true
}

func externalID(key string, value any) any {
	if value != nil {
		return value
	}
	return strings.TrimPrefix(strings.TrimPrefix(key, "s:"), "n:")
}

func qdrantOperationResult(affected int) map[string]any {
	return map[string]any{"result": map[string]any{"operation_id": 0, "status": "completed", "affected": affected}, "status": "ok", "time": 0}
}

// flattenQdrantFilter supports Qdrant's common equality form:
// {"must":[{"key":"team","match":{"value":"blue"}}]}.
func flattenQdrantFilter(raw map[string]any) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	must, ok := raw["must"].([]any)
	if !ok {
		return nil, errors.New("filter must contain a must array")
	}
	out := make(map[string]any, len(must))
	for _, item := range must {
		condition, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("filter condition must be an object")
		}
		key, _ := condition["key"].(string)
		match, _ := condition["match"].(map[string]any)
		value, exists := match["value"]
		if key == "" || !exists {
			return nil, errors.New("filter condition requires key and match.value")
		}
		if previous, duplicate := out[key]; duplicate && !reflect.DeepEqual(previous, value) {
			// Contradictory metadata requirements deterministically match nothing.
			out["\x00contradiction"] = true
		}
		out[key] = value
	}
	return out, nil
}

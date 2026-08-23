package adapter

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/mockagents/mockagents/internal/types"
)

const ProtocolCohereRerank = "cohere-rerank"
const maxRerankDocuments = 1000

type CohereRerankHandler struct{ Faults types.SearchFaults }

func (h *CohereRerankHandler) Name() string { return ProtocolCohereRerank }
func (h *CohereRerankHandler) Routes() []Route {
	return []Route{{Pattern: "POST /v2/rerank", Handler: h.Rerank}}
}

type cohereRerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      *int     `json:"top_n,omitempty"`
}
type cohereRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

func (h *CohereRerankHandler) Rerank(w http.ResponseWriter, r *http.Request) {
	if applyServiceFaults(w, r, h.Faults, func(status int) { writeJSON(w, status, map[string]any{"message": "injected rerank fault"}) }) {
		return
	}
	var req cohereRerankRequest
	if !decodeQdrant(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeCohereError(w, "model is required")
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeCohereError(w, "query is required")
		return
	}
	if len(req.Documents) == 0 || len(req.Documents) > maxRerankDocuments {
		writeCohereError(w, fmt.Sprintf("documents must contain between 1 and %d items", maxRerankDocuments))
		return
	}
	top := len(req.Documents)
	if req.TopN != nil {
		if *req.TopN < 1 {
			writeCohereError(w, "top_n must be at least 1")
			return
		}
		if *req.TopN < top {
			top = *req.TopN
		}
	}
	results := make([]cohereRerankResult, len(req.Documents))
	for i, doc := range req.Documents {
		results[i] = cohereRerankResult{Index: i, RelevanceScore: rerankScore(req.Query, doc)}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].RelevanceScore > results[j].RelevanceScore })
	results = results[:top]
	results = results[:partialResultLimit(h.Faults, len(results))]
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(req.Model))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(req.Query))
	for _, d := range req.Documents {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": fmt.Sprintf("rerank-%016x", hash.Sum64()), "results": results, "meta": map[string]any{"api_version": map[string]any{"version": "2", "is_experimental": false}, "billed_units": map[string]any{"search_units": 1}}})
}
func writeCohereError(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"message": message})
}
func rerankScore(query, document string) float64 {
	q := tokenSet(query)
	d := tokenSet(document)
	if len(q) == 0 || len(d) == 0 {
		return 0
	}
	hits := 0
	for token := range q {
		if _, ok := d[token]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(q))
}
func tokenSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, token := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if token != "" {
			out[token] = struct{}{}
		}
	}
	return out
}

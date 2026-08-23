package adapter

import (
	"net/http"

	commonchaos "github.com/mockagents/mockagents/internal/chaos"
	"github.com/mockagents/mockagents/internal/vector"
)

func vectorChaosQuery(r *http.Request, query vector.Query) vector.Query {
	query.RequestKey = r.Header.Get("X-Request-Id")
	if query.RequestKey == "" {
		query.RequestKey = r.Method + " " + r.URL.Path
	}
	query.ForcedChaos = r.Header.Get(commonchaos.ForceHeader)
	return query
}

func stampVectorChaos(w http.ResponseWriter, result vector.QueryResult) {
	if !result.Partial {
		return
	}
	w.Header().Set("X-Mockagents-Vector-Partial", "true")
	w.Header().Set("X-Mockagents-Chaos-Action", result.ChaosAction)
	w.Header().Set("X-Mockagents-Chaos-Source", result.ChaosSource)
}

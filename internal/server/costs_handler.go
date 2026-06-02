package server

import (
	"net/http"
	"sort"

	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/storage"
)

// CostsHandlers serves the cost aggregate endpoint. It shares the
// interaction-log store with LogHandlers but exposes a different
// materialization: grouped totals rather than per-row.
type CostsHandlers struct {
	Store  *storage.SQLiteStore
	Prices *pricing.Table
}

// CostGroup is one row in the aggregate response, keyed by either a
// model name or an agent name (never both — each response lists two
// parallel arrays so callers don't have to pivot).
type CostGroup struct {
	Key              string  `json:"key"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// CostsResponse is the full wire shape for GET /api/v1/costs.
type CostsResponse struct {
	Window struct {
		Since string `json:"since,omitempty"`
		Until string `json:"until,omitempty"`
	} `json:"window"`
	Requests         int         `json:"total_requests"`
	PromptTokens     int         `json:"total_prompt_tokens"`
	CompletionTokens int         `json:"total_completion_tokens"`
	CostUSD          float64     `json:"total_cost_usd"`
	ByModel          []CostGroup `json:"by_model"`
	ByAgent          []CostGroup `json:"by_agent"`
}

// ListCosts handles GET /api/v1/costs.
//
// Query parameters:
//
//	since  RFC3339 lower bound on log timestamp
//	until  RFC3339 upper bound
//	agent  restrict aggregation to a single agent
//	limit  max rows to scan (default 1000, max 10000)
func (h *CostsHandlers) ListCosts(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "logging is not enabled",
		})
		return
	}

	filter := storage.InteractionFilter{
		AgentName: r.URL.Query().Get("agent"),
		Since:     r.URL.Query().Get("since"),
		Until:     r.URL.Query().Get("until"),
		Limit:     1000,
	}
	if tenantID := callerTenantID(r); tenantID != "" {
		filter.TenantID = tenantID
		filter.FilterTenantID = true
	}
	if ls := r.URL.Query().Get("limit"); ls != "" {
		n, ok := parseBoundedInt(w, ls, "limit", 1, maxListLimit)
		if !ok {
			return
		}
		filter.Limit = n
	}

	rows, err := h.Store.Query(r.Context(), filter)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Aggregate in memory. This is O(N) over the filtered log set
	// which is already bounded by the limit clamp above.
	byModel := make(map[string]*CostGroup)
	byAgent := make(map[string]*CostGroup)
	var resp CostsResponse
	resp.Window.Since = filter.Since
	resp.Window.Until = filter.Until

	for _, row := range rows {
		usage := pricing.ExtractUsage([]byte(row.ResponseBody))
		cost := 0.0
		if h.Prices != nil {
			cost = h.Prices.Estimate(usage.Model, usage.PromptTokens, usage.CompletionTokens)
		}

		resp.Requests++
		resp.PromptTokens += usage.PromptTokens
		resp.CompletionTokens += usage.CompletionTokens
		resp.CostUSD += cost

		mk := usage.Model
		if mk == "" {
			mk = "(unknown)"
		}
		if g, ok := byModel[mk]; ok {
			g.Requests++
			g.PromptTokens += usage.PromptTokens
			g.CompletionTokens += usage.CompletionTokens
			g.CostUSD += cost
		} else {
			byModel[mk] = &CostGroup{
				Key:              mk,
				Requests:         1,
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				CostUSD:          cost,
			}
		}

		ak := row.AgentName
		if ak == "" {
			ak = "(unknown)"
		}
		if g, ok := byAgent[ak]; ok {
			g.Requests++
			g.PromptTokens += usage.PromptTokens
			g.CompletionTokens += usage.CompletionTokens
			g.CostUSD += cost
		} else {
			byAgent[ak] = &CostGroup{
				Key:              ak,
				Requests:         1,
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				CostUSD:          cost,
			}
		}
	}

	resp.ByModel = flattenGroups(byModel)
	resp.ByAgent = flattenGroups(byAgent)
	writeJSON(w, http.StatusOK, resp)
}

// flattenGroups converts a map into a descending-cost-ordered slice
// so dashboards and CLIs don't have to re-sort.
func flattenGroups(m map[string]*CostGroup) []CostGroup {
	out := make([]CostGroup, 0, len(m))
	for _, g := range m {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		return out[i].Key < out[j].Key
	})
	return out
}

package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	commonchaos "github.com/mockagents/mockagents/internal/chaos"
	"github.com/mockagents/mockagents/internal/types"
)

// ChaosHTTPHandler applies MCP-scoped deterministic faults to client-facing
// HTTP transports. Admin notification routes and health checks are not wrapped.
type ChaosHTTPHandler struct {
	Next   http.Handler
	Faults types.MCPFaults
}

func NewChaosHTTPHandler(next http.Handler, faults types.MCPFaults) *ChaosHTTPHandler {
	return &ChaosHTTPHandler{Next: next, Faults: faults}
}

func (h *ChaosHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Request-Id")
	if key == "" {
		key = r.Method + " " + r.URL.Path
	}
	force := r.Header.Get(commonchaos.ForceHeader)
	policy := commonchaos.Policy{Seed: h.Faults.Seed, Rate: h.Faults.Rate}
	applies := func(action string) (bool, string) {
		decision := commonchaos.Decide(policy, key, action, force)
		return decision.Apply, decision.Source
	}
	if h.Faults.LatencyMs > 0 {
		if apply, source := applies("latency"); apply {
			stampMCPChaos(w, "latency", source)
			timer := time.NewTimer(time.Duration(h.Faults.LatencyMs) * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
	}
	if r.Method == http.MethodPost && h.Faults.Error {
		if apply, source := applies("error"); apply {
			stampMCPChaos(w, "error", source)
			writeMCPChaosError(w, r)
			return
		}
	}
	h.Next.ServeHTTP(w, r)
}

func stampMCPChaos(w http.ResponseWriter, action, source string) {
	w.Header().Set("X-Mockagents-Chaos-Action", action)
	w.Header().Set("X-Mockagents-Chaos-Source", source)
}

func writeMCPChaosError(w http.ResponseWriter, r *http.Request) {
	var id json.RawMessage
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxMCPBodyBytes+1))
		if err == nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			if len(body) <= maxMCPBodyBytes {
				var request struct {
					ID json.RawMessage `json:"id"`
				}
				if json.Unmarshal(body, &request) == nil {
					id = request.ID
				}
			}
		}
	}
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32000,
			"message": "mock MCP chaos fault",
		},
	})
}

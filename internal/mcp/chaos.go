package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	commonchaos "github.com/mockagents/mockagents/internal/chaos"
	"github.com/mockagents/mockagents/internal/types"
)

// ChaosHTTPHandler applies MCP-scoped deterministic faults to client-facing
// HTTP transports. Admin notification routes and health checks are not wrapped.
type ChaosHTTPHandler struct {
	Next     http.Handler
	Faults   types.MCPFaults
	sequence atomic.Uint64
}

func NewChaosHTTPHandler(next http.Handler, faults types.MCPFaults) *ChaosHTTPHandler {
	return &ChaosHTTPHandler{Next: next, Faults: faults}
}

func (h *ChaosHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	operation := r.Method + " " + r.URL.Path
	fixture := ""
	if r.Method == http.MethodPost && r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxMCPBodyBytes+1))
		if err == nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			var request struct {
				Method string `json:"method"`
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			if len(body) <= maxMCPBodyBytes && json.Unmarshal(body, &request) == nil && request.Method != "" {
				operation = request.Method
				if request.Method == "tools/call" {
					fixture = request.Params.Name
				}
			}
		}
	}
	key := r.Header.Get("X-Request-Id")
	if key == "" {
		key = r.Method + " " + r.URL.Path
	}
	force := r.Header.Get(commonchaos.ForceHeader)
	policy, operation := commonchaos.ForOperation(commonchaos.Policy{Seed: h.Faults.Seed, Rate: h.Faults.Rate}, operation, h.Faults.OperationRates)
	policy = commonchaos.ForFixture(policy, fixture, h.Faults.FixtureRates)
	sequence := h.sequence.Add(1)
	policy = commonchaos.ForSequence(policy, sequence, h.Faults.SequenceRates)
	key += "\x00" + operation
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
	if r.Method == http.MethodPost && h.Faults.TimeoutMs > 0 {
		if apply, source := applies("timeout"); apply {
			stampMCPChaos(w, "timeout", source)
			timer := time.NewTimer(time.Duration(h.Faults.TimeoutMs) * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				writeMCPChaosError(w, r, "timeout", source, "mock MCP timeout")
			case <-r.Context().Done():
			}
			return
		}
	}
	if h.Faults.Disconnect {
		if apply, source := applies("disconnect"); apply {
			stampMCPChaos(w, "disconnect", source)
			if !commonchaos.DisconnectHTTP(w) {
				http.Error(w, "mock MCP disconnect", http.StatusBadGateway)
			}
			return
		}
	}
	if r.Method == http.MethodPost && h.Faults.StatusCode != 0 {
		if apply, source := applies("status"); apply {
			stampMCPChaos(w, "status", source)
			writeMCPChaosErrorStatus(w, r, h.Faults.StatusCode, "status", source, "mock MCP HTTP status fault")
			return
		}
	}
	if r.Method == http.MethodPost && h.Faults.MalformedSchema {
		if apply, source := applies("malformed-schema"); apply {
			stampMCPChaos(w, "malformed-schema", source)
			writeMCPMalformedSchema(w, r)
			return
		}
	}
	if r.Method == http.MethodPost && h.Faults.TruncateAfterBytes > 0 {
		if apply, source := applies("truncate"); apply {
			stampMCPChaos(w, "truncate", source)
			writeMCPTruncated(w, r, h.Faults.TruncateAfterBytes)
			return
		}
	}
	if r.Method == http.MethodPost && h.Faults.Malformed {
		if apply, source := applies("malformed"); apply {
			stampMCPChaos(w, "malformed", source)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":`))
			return
		}
	}
	if r.Method == http.MethodPost && h.Faults.Error {
		if apply, source := applies("error"); apply {
			stampMCPChaos(w, "error", source)
			writeMCPChaosError(w, r, "error", source, "mock MCP chaos fault")
			return
		}
	}
	h.Next.ServeHTTP(w, r)
}

func stampMCPChaos(w http.ResponseWriter, action, source string) {
	w.Header().Set("X-Mockagents-Chaos-Action", action)
	w.Header().Set("X-Mockagents-Chaos-Source", source)
}

func writeMCPChaosError(w http.ResponseWriter, r *http.Request, action, source, message string) {
	writeMCPChaosErrorStatus(w, r, http.StatusOK, action, source, message)
}

func writeMCPChaosErrorStatus(w http.ResponseWriter, r *http.Request, status int, action, source, message string) {
	id := readMCPRequestID(r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32000,
			"message": message,
			"data": map[string]any{
				"chaos": map[string]string{"action": action, "source": source},
			},
		},
	})
}

func writeMCPMalformedSchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      readMCPRequestID(r),
		"result":  map[string]any{"unexpected": true},
	})
}

func writeMCPTruncated(w http.ResponseWriter, r *http.Request, after int) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      readMCPRequestID(r),
		"result": map[string]any{
			"content": []map[string]string{{"type": "text", "text": strings.Repeat("x", after+1)}},
		},
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload[:after])
}

func readMCPRequestID(r *http.Request) json.RawMessage {
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
	return id
}

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func TestChaosHTTPHandlerForceErrorAndOffPrecedence(t *testing.T) {
	zero := 0.0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := NewChaosHTTPHandler(next, types.MCPFaults{Rate: &zero, Error: true})

	req := httptest.NewRequest(http.MethodPost, "/mcp/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"ping"}`))
	req.Header.Set("X-Mockagents-Chaos", "error")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Mockagents-Chaos-Source") != "request-force" {
		t.Fatalf("forced status=%d source=%q body=%s", rec.Code, rec.Header().Get("X-Mockagents-Chaos-Source"), rec.Body.String())
	}
	var body struct {
		ID    int `json:"id"`
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.ID != 7 || body.Error.Code != -32000 {
		t.Fatalf("response=%+v err=%v", body, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"ping"}`))
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("off status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChaosHTTPHandlerSeededDecisionUsesRequestID(t *testing.T) {
	rate := 0.5
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), types.MCPFaults{Seed: 99, Rate: &rate, Error: true})
	result := func() int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("X-Request-Id", "stable-request")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	want := result()
	for i := 0; i < 10; i++ {
		if got := result(); got != want {
			t.Fatalf("same request id changed status from %d to %d", want, got)
		}
	}
}

func TestChaosHTTPHandlerOperationRateOverridesServiceRate(t *testing.T) {
	one, zero := 1.0, 0.0
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), types.MCPFaults{
		Rate: &one, Error: true, OperationRates: map[string]float64{"tools/list": zero},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("tools/list status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call"}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Source") != "seeded-rate" {
		t.Fatalf("tools/call source=%q", rec.Header().Get("X-Mockagents-Chaos-Source"))
	}
}

func TestChaosHTTPHandlerSequenceRatePrecedence(t *testing.T) {
	one, zero := 1.0, 0.0
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), types.MCPFaults{Rate: &zero, Error: true, SequenceRates: map[uint64]float64{2: one}})
	for sequence := 1; sequence <= 2; sequence++ {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if sequence == 1 && rec.Code != http.StatusNoContent {
			t.Fatalf("first status=%d", rec.Code)
		}
		if sequence == 2 && rec.Header().Get("X-Mockagents-Chaos-Source") != "sequence-rate" {
			t.Fatalf("second source=%q", rec.Header().Get("X-Mockagents-Chaos-Source"))
		}
	}
}

func TestChaosHTTPHandlerFixtureRatePrecedence(t *testing.T) {
	one, zero := 1.0, 0.0
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), types.MCPFaults{Rate: &one, Error: true, FixtureRates: map[string]float64{"weather": zero}})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"weather"}}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("weather status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"other"}}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Source") != "seeded-rate" {
		t.Fatalf("other source=%q", rec.Header().Get("X-Mockagents-Chaos-Source"))
	}
}

func TestChaosHTTPHandlerForceMalformedAndActionPrecedence(t *testing.T) {
	zero := 0.0
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), types.MCPFaults{Rate: &zero, Malformed: true, Error: true})

	req := httptest.NewRequest(http.MethodPost, "/mcp/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"ping"}`))
	req.Header.Set("X-Mockagents-Chaos", "malformed")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Mockagents-Chaos-Action") != "malformed" || json.Valid(rec.Body.Bytes()) {
		t.Fatalf("malformed status=%d action=%q body=%q", rec.Code, rec.Header().Get("X-Mockagents-Chaos-Action"), rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":10,"method":"ping"}`))
	req.Header.Set("X-Mockagents-Chaos", "error")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Action") != "error" || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("error action=%q body=%q", rec.Header().Get("X-Mockagents-Chaos-Action"), rec.Body.String())
	}
}

func TestChaosHTTPHandlerForceTimeoutAndOff(t *testing.T) {
	zero := 0.0
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), types.MCPFaults{Rate: &zero, TimeoutMs: 1})

	req := httptest.NewRequest(http.MethodPost, "/mcp/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":13,"method":"ping"}`))
	req.Header.Set("X-Mockagents-Chaos", "timeout")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body struct {
		ID    int `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Chaos struct {
					Action string `json:"action"`
					Source string `json:"source"`
				} `json:"chaos"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.ID != 13 || body.Error.Code != -32000 || body.Error.Message != "mock MCP timeout" || body.Error.Data.Chaos.Action != "timeout" || body.Error.Data.Chaos.Source != "request-force" {
		t.Fatalf("timeout response=%+v err=%v body=%s", body, err, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":14,"method":"ping"}`))
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("off status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChaosHTTPHandlerDisconnectFallbackAndOff(t *testing.T) {
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), types.MCPFaults{Disconnect: true})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || rec.Header().Get("X-Mockagents-Chaos-Action") != "disconnect" {
		t.Fatalf("disconnect status=%d action=%q", rec.Code, rec.Header().Get("X-Mockagents-Chaos-Action"))
	}
	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("off status=%d", rec.Code)
	}
}

func TestChaosHTTPHandlerStatusFaultAndOff(t *testing.T) {
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), types.MCPFaults{StatusCode: http.StatusTooManyRequests})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":21,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusTooManyRequests || body.ID != 21 || rec.Header().Get("X-Mockagents-Chaos-Action") != "status" || rec.Header().Get("X-Mockagents-Chaos-Source") != "configured" {
		t.Fatalf("status=%d headers=%v body=%s err=%v", rec.Code, rec.Header(), rec.Body.String(), err)
	}
	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("off status=%d", rec.Code)
	}
}

func TestChaosHTTPHandlerMalformedSchemaFault(t *testing.T) {
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), types.MCPFaults{MalformedSchema: true})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":31,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["id"] != float64(31) || body["error"] != nil || rec.Header().Get("X-Mockagents-Chaos-Action") != "malformed-schema" || rec.Header().Get("X-Mockagents-Chaos-Source") != "configured" {
		t.Fatalf("headers=%v body=%s err=%v", rec.Header(), rec.Body.String(), err)
	}
	result, ok := body["result"].(map[string]any)
	if !ok || result["unexpected"] != true {
		t.Fatalf("result=%v", body["result"])
	}
}

func TestChaosHTTPHandlerTruncationFaultAndOff(t *testing.T) {
	h := NewChaosHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), types.MCPFaults{TruncateAfterBytes: 24})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":41,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Body.Len() != 24 || json.Valid(rec.Body.Bytes()) || rec.Header().Get("X-Mockagents-Chaos-Action") != "truncate" || rec.Header().Get("X-Mockagents-Chaos-Source") != "configured" {
		t.Fatalf("headers=%v body=%q", rec.Header(), rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("off status=%d", rec.Code)
	}
}

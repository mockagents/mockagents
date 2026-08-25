package a2a

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func TestA2AChaosForceErrorAndOffPrecedence(t *testing.T) {
	zero := 0.0
	def := testDef()
	def.Spec.Faults = types.A2AFaults{Rate: &zero, Error: true}
	h := NewServer(def).RPCHandler()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"message/send","params":{}}`))
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
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.ID != 7 || body.Error.Code != errInternal {
		t.Fatalf("response=%+v err=%v", body, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"message/send","params":{}}`))
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Action") != "" || rec.Code != http.StatusOK {
		t.Fatalf("off status=%d action=%q body=%s", rec.Code, rec.Header().Get("X-Mockagents-Chaos-Action"), rec.Body.String())
	}
}

func TestA2AChaosSeededDecisionUsesRequestID(t *testing.T) {
	rate := 0.5
	def := testDef()
	def.Spec.Faults = types.A2AFaults{Seed: 99, Rate: &rate, Error: true}
	h := NewServer(def).RPCHandler()
	result := func() string {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"message/send","params":{}}`))
		req.Header.Set("X-Request-Id", "stable-request")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Header().Get("X-Mockagents-Chaos-Action")
	}
	want := result()
	for i := 0; i < 10; i++ {
		if got := result(); got != want {
			t.Fatalf("same request id changed action from %q to %q", want, got)
		}
	}
}

func TestA2AChaosOperationRateOverridesServiceRate(t *testing.T) {
	one, zero := 1.0, 0.0
	def := testDef()
	def.Spec.Faults = types.A2AFaults{Rate: &one, Error: true, OperationRates: map[string]float64{"message/send": zero}}
	s := NewServer(def)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"parts":[{"kind":"text","text":"hello"}]}}}`))
	rec := httptest.NewRecorder()
	s.RPCHandler().ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Action") != "" {
		t.Fatalf("message/send unexpectedly faulted: %s", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tasks/get"}`))
	rec = httptest.NewRecorder()
	s.RPCHandler().ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Source") != "seeded-rate" {
		t.Fatalf("tasks/get source=%q", rec.Header().Get("X-Mockagents-Chaos-Source"))
	}
}

func TestA2AChaosSequenceRatePrecedence(t *testing.T) {
	one, zero := 1.0, 0.0
	def := testDef()
	def.Spec.Faults = types.A2AFaults{Rate: &zero, Error: true, SequenceRates: map[uint64]float64{2: one}}
	s := NewServer(def)
	for sequence := 1; sequence <= 2; sequence++ {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tasks/get"}`))
		rec := httptest.NewRecorder()
		s.RPCHandler().ServeHTTP(rec, req)
		if sequence == 1 && rec.Header().Get("X-Mockagents-Chaos-Action") != "" {
			t.Fatalf("first faulted: %s", rec.Body.String())
		}
		if sequence == 2 && rec.Header().Get("X-Mockagents-Chaos-Source") != "sequence-rate" {
			t.Fatalf("second source=%q", rec.Header().Get("X-Mockagents-Chaos-Source"))
		}
	}
}

func TestA2AChaosFixtureRatePrecedence(t *testing.T) {
	one, zero := 1.0, 0.0
	def := testDef()
	def.Spec.Faults = types.A2AFaults{Rate: &one, Error: true, FixtureRates: map[string]float64{"weather": zero}}
	s := NewServer(def)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"parts":[{"kind":"text","text":"weather please"}]}}}`))
	rec := httptest.NewRecorder()
	s.RPCHandler().ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Action") != "" {
		t.Fatalf("weather faulted: %s", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"message/send","params":{"message":{"parts":[{"kind":"text","text":"unknown"}]}}}`))
	rec = httptest.NewRecorder()
	s.RPCHandler().ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Source") != "seeded-rate" {
		t.Fatalf("unknown source=%q", rec.Header().Get("X-Mockagents-Chaos-Source"))
	}
}

func TestA2AChaosForceMalformedAndActionPrecedence(t *testing.T) {
	zero := 0.0
	def := testDef()
	def.Spec.Faults = types.A2AFaults{Rate: &zero, Malformed: true, Error: true}
	h := NewServer(def).RPCHandler()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"message/send","params":{}}`))
	req.Header.Set("X-Mockagents-Chaos", "malformed")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Mockagents-Chaos-Action") != "malformed" || rec.Header().Get("X-Mockagents-Chaos-Source") != "request-force" || json.Valid(rec.Body.Bytes()) {
		t.Fatalf("malformed status=%d action=%q source=%q body=%q", rec.Code, rec.Header().Get("X-Mockagents-Chaos-Action"), rec.Header().Get("X-Mockagents-Chaos-Source"), rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":10,"method":"message/send","params":{}}`))
	req.Header.Set("X-Mockagents-Chaos", "error")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Action") != "error" || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("error action=%q body=%q", rec.Header().Get("X-Mockagents-Chaos-Action"), rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":11,"method":"message/send","params":{}}`))
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Action") != "" || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("off action=%q body=%q", rec.Header().Get("X-Mockagents-Chaos-Action"), rec.Body.String())
	}
}

func TestA2AChaosForceTimeoutAndOff(t *testing.T) {
	zero := 0.0
	def := testDef()
	def.Spec.Faults = types.A2AFaults{Rate: &zero, TimeoutMs: 1}
	h := NewServer(def).RPCHandler()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":12,"method":"message/send","params":{}}`))
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
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.ID != 12 || body.Error.Code != errInternal || body.Error.Message != "mock A2A timeout" || body.Error.Data.Chaos.Action != "timeout" || body.Error.Data.Chaos.Source != "request-force" {
		t.Fatalf("timeout response=%+v err=%v body=%s", body, err, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":13,"method":"message/send","params":{}}`))
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Mockagents-Chaos-Action") != "" || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("off action=%q body=%q", rec.Header().Get("X-Mockagents-Chaos-Action"), rec.Body.String())
	}
}

func TestA2AChaosDisconnectFallbackAndOff(t *testing.T) {
	def := testDef()
	def.Spec.Faults = types.A2AFaults{Disconnect: true}
	s := NewServer(def)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tasks/get"}`))
	rec := httptest.NewRecorder()
	s.RPCHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || rec.Header().Get("X-Mockagents-Chaos-Action") != "disconnect" {
		t.Fatalf("disconnect status=%d action=%q", rec.Code, rec.Header().Get("X-Mockagents-Chaos-Action"))
	}
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tasks/get"}`))
	req.Header.Set("X-Mockagents-Chaos", "off")
	rec = httptest.NewRecorder()
	s.RPCHandler().ServeHTTP(rec, req)
	if rec.Code == http.StatusBadGateway {
		t.Fatal("off request disconnected")
	}
}

package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

func TestCommonServiceFaultLatencyAndDisconnectFallback(t *testing.T) {
	w := httptest.NewRecorder()
	start := time.Now()
	handled := applyServiceFaults(w, httptest.NewRequest(http.MethodPost, "/", nil), types.SearchFaults{LatencyMs: 2}, func(status int) { w.WriteHeader(status) })
	if handled || time.Since(start) < 2*time.Millisecond {
		t.Fatalf("latency fault handled=%v elapsed=%v", handled, time.Since(start))
	}

	w = httptest.NewRecorder() // ResponseRecorder cannot hijack, so fallback is deterministic.
	handled = applyServiceFaults(w, httptest.NewRequest(http.MethodPost, "/", nil), types.SearchFaults{Disconnect: true}, func(status int) { w.WriteHeader(status) })
	if !handled || w.Code != http.StatusBadGateway {
		t.Fatalf("disconnect handled=%v status=%d", handled, w.Code)
	}
}

func TestCommonServiceFaultRequestForcePrecedence(t *testing.T) {
	rate := 0.0
	faults := types.SearchFaults{Rate: &rate, StatusCode: http.StatusServiceUnavailable, MalformedJSON: true}
	req := httptest.NewRequest(http.MethodPost, "/v2/rerank", nil)
	req.Header.Set("X-Mockagents-Chaos", "status")
	w := httptest.NewRecorder()
	handled := applyServiceFaults(w, req, faults, func(status int) { w.WriteHeader(status) })
	if !handled || w.Code != http.StatusServiceUnavailable {
		t.Fatalf("forced status handled=%v status=%d", handled, w.Code)
	}
	if got := w.Header().Get("X-Mockagents-Chaos-Action"); got != "status" {
		t.Fatalf("action header=%q", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/v2/rerank", nil)
	req.Header.Set("X-Mockagents-Chaos", "off")
	w = httptest.NewRecorder()
	if applyServiceFaults(w, req, faults, func(status int) { w.WriteHeader(status) }) {
		t.Fatal("off request unexpectedly applied chaos")
	}
}

func TestCommonServiceFaultSeededDecisionUsesRequestID(t *testing.T) {
	rate := 0.5
	faults := types.SearchFaults{Seed: 1234, Rate: &rate, StatusCode: http.StatusServiceUnavailable}
	result := func(id string) bool {
		req := httptest.NewRequest(http.MethodPost, "/search", nil)
		req.Header.Set("X-Request-Id", id)
		return applyServiceFaults(httptest.NewRecorder(), req, faults, func(int) {})
	}
	first := result("stable-request")
	for i := 0; i < 10; i++ {
		if got := result("stable-request"); got != first {
			t.Fatalf("same request id changed from %v to %v", first, got)
		}
	}
}

func TestCommonServiceFaultOperationRatePrecedence(t *testing.T) {
	one, zero := 1.0, 0.0
	faults := types.SearchFaults{Rate: &one, StatusCode: http.StatusServiceUnavailable, OperationRates: map[string]float64{"/v2/rerank": zero}}
	req := httptest.NewRequest(http.MethodPost, "/v2/rerank", nil)
	w := httptest.NewRecorder()
	if applyServiceFaults(w, req, faults, func(status int) { w.WriteHeader(status) }) {
		t.Fatal("rerank operation unexpectedly faulted")
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/moderations", nil)
	w = httptest.NewRecorder()
	if !applyServiceFaults(w, req, faults, func(status int) { w.WriteHeader(status) }) {
		t.Fatal("moderation did not inherit service rate")
	}
	if got := w.Header().Get("X-Mockagents-Chaos-Source"); got != "seeded-rate" {
		t.Fatalf("moderation source=%q", got)
	}
}

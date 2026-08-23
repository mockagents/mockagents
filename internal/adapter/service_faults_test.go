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

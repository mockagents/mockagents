package adapter

import (
	"net/http"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// applyServiceFaults runs the common deterministic pre-response fault order
// used by search, rerank, and moderation. It returns true when the request is
// fully handled by a fault.
func applyServiceFaults(w http.ResponseWriter, r *http.Request, faults types.SearchFaults, errorBody func(int)) bool {
	if faults.LatencyMs > 0 {
		timer := time.NewTimer(time.Duration(faults.LatencyMs) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return true
		}
	}
	if faults.Disconnect {
		if !connectionFault(w, "empty") {
			errorBody(http.StatusBadGateway)
		}
		return true
	}
	if faults.StatusCode != 0 {
		errorBody(faults.StatusCode)
		return true
	}
	if faults.MalformedJSON {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":`))
		return true
	}
	return false
}

func partialResultLimit(faults types.SearchFaults, length int) int {
	if p := faults.PartialResults; p != nil && p.MaxResults < length {
		return p.MaxResults
	}
	return length
}

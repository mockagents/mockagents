package adapter

import (
	"net/http"
	"strconv"
	"time"

	commonchaos "github.com/mockagents/mockagents/internal/chaos"
	"github.com/mockagents/mockagents/internal/types"
)

// applyServiceFaults runs the common deterministic pre-response fault order
// used by search, rerank, and moderation. It returns true when the request is
// fully handled by a fault.
func applyServiceFaults(w http.ResponseWriter, r *http.Request, faults types.SearchFaults, errorBody func(int)) bool {
	requestKey := r.Header.Get("X-Request-Id")
	if requestKey == "" {
		requestKey = r.Method + " " + r.URL.Path
	}
	forced := r.Header.Get(commonchaos.ForceHeader)
	policy, operation := commonchaos.ForOperation(commonchaos.Policy{Seed: faults.Seed, Rate: faults.Rate}, r.URL.Path, faults.OperationRates)
	requestKey += "\x00" + operation
	applies := func(action string) bool {
		decision := commonchaos.Decide(policy, requestKey, action, forced)
		if decision.Apply {
			w.Header().Set("X-Mockagents-Chaos-Action", action)
			w.Header().Set("X-Mockagents-Chaos-Source", decision.Source)
			w.Header().Set("X-Mockagents-Chaos-Seed", strconv.FormatInt(policy.Seed, 10))
			if policy.Rate != nil {
				w.Header().Set("X-Mockagents-Chaos-Rate", strconv.FormatFloat(*policy.Rate, 'g', -1, 64))
			}
		}
		return decision.Apply
	}
	if faults.LatencyMs > 0 && applies("latency") {
		timer := time.NewTimer(time.Duration(faults.LatencyMs) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return true
		}
	}
	if faults.Disconnect && applies("disconnect") {
		if !connectionFault(w, "empty") {
			errorBody(http.StatusBadGateway)
		}
		return true
	}
	if faults.StatusCode != 0 && applies("status") {
		errorBody(faults.StatusCode)
		return true
	}
	if faults.MalformedJSON && applies("malformed") {
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

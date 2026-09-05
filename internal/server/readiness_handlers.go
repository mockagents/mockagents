package server

import (
	"context"
	"net/http"
	"time"
)

// readinessTimeout bounds the whole readiness sweep. The chart's readiness
// probe defaults to timeoutSeconds: 1, so a check that hangs must fail fast
// enough that kubelet sees a 503 rather than a timeout — the two look the same
// to Kubernetes but only one of them tells the operator which dependency broke.
const readinessTimeout = 750 * time.Millisecond

// ReadinessCheck is one named dependency probed by GET /api/v1/ready.
// A nil error means ready.
type ReadinessCheck struct {
	Name  string
	Check func(context.Context) error
}

// ReadinessHandlers answers GET /api/v1/ready.
//
// This is the endpoint that makes readiness a DIFFERENT question from
// liveness, which PRD §11 requires and which the deployment did not have:
// /api/v1/health returns 200 the moment the process is up, so pointing both
// Helm probes at it made them the same check. A pod whose interaction log had
// become unreachable, or whose last agent had been removed through the write
// API, stayed in rotation answering 404s.
type ReadinessHandlers struct {
	Checks []ReadinessCheck
}

// ReadinessCheckResult is one check as GET /api/v1/ready reports it.
//
// Deliberately distinct from ReadinessCheck, which is the probe CONFIG: one
// carries a function, the other is what goes on the wire. Conflating them is
// an easy mistake — the drift checker made it first.
type ReadinessCheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type ReadinessResponse struct {
	Status string                 `json:"status"`
	Checks []ReadinessCheckResult `json:"checks"`
}

// Ready runs every check and returns 200 only when all pass; otherwise 503
// with the failing check named in the body, so `kubectl describe pod` and a
// plain curl both say WHY the pod is not serving.
func (h *ReadinessHandlers) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	resp := ReadinessResponse{
		Status: "ready",
		Checks: make([]ReadinessCheckResult, 0, len(h.Checks)),
	}
	status := http.StatusOK
	for _, c := range h.Checks {
		res := ReadinessCheckResult{Name: c.Name, Status: "ok"}
		if err := c.Check(ctx); err != nil {
			res.Status = "failed"
			res.Error = err.Error()
			resp.Status = "not_ready"
			status = http.StatusServiceUnavailable
		}
		resp.Checks = append(resp.Checks, res)
	}
	// Probes poll; a cached 200 would keep a broken pod in rotation.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, resp)
}

// IsReady reports whether every check passes. Used by the mockagents_ready
// gauge so a scrape and a probe cannot disagree about the same process.
func (h *ReadinessHandlers) IsReady(ctx context.Context) bool {
	for _, c := range h.Checks {
		if c.Check(ctx) != nil {
			return false
		}
	}
	return true
}

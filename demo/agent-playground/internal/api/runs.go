package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
)

func (s *Server) listWorkflows(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"workflows": s.Engine.Definitions(false)})
}

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	info, ok := s.Engine.Definition(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("workflow %q not found", r.PathValue("name")), nil)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// StartRunRequest is the body of POST /api/workflows/{name}/runs.
type StartRunRequest struct {
	Input   map[string]any      `json:"input"`
	Options workflow.RunOptions `json:"options"`
}

// waitParam parses ?wait=<seconds> (0..MaxWait).
func (s *Server) waitParam(r *http.Request) (time.Duration, error) {
	v := r.URL.Query().Get("wait")
	if v == "" {
		return 0, nil
	}
	secs, err := strconv.ParseFloat(v, 64)
	if err != nil || secs < 0 {
		return 0, fmt.Errorf("wait must be a non-negative number of seconds")
	}
	d := time.Duration(secs * float64(time.Second))
	if d > s.MaxWait {
		d = s.MaxWait
	}
	return d, nil
}

// settled: terminal, or blocked on a human, so waiting longer is pointless.
func settled(r *workflow.Run) bool { return r.Terminal() || r.Waiting() }

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	var req StartRunRequest
	if err := decode(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	wait, err := s.waitParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	run, err := s.Engine.Start(r.PathValue("name"), req.Input, req.Options, workflow.StartOptions{})
	if err != nil {
		s.startError(w, err)
		return
	}
	if wait > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), wait)
		defer cancel()
		if got, err := s.Engine.Wait(ctx, run.ID, settled); err == nil {
			run = got
		}
	}
	w.Header().Set("Location", "/api/runs/"+run.ID)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) startError(w http.ResponseWriter, err error) {
	var ie *workflow.InputError
	switch {
	case errors.Is(err, workflow.ErrUnknownWorkflow):
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.As(err, &ie):
		writeError(w, http.StatusBadRequest, "invalid_input", "the request was rejected before a run was created", strings.Split(ie.Error(), "\n"))
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error(), nil)
	}
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := workflow.RunFilter{Workflow: q.Get("workflow"), Status: workflow.Status(q.Get("status"))}
	if f.Status != "" && f.Status != workflow.StatusInProgress && f.Status != workflow.StatusCompleted && f.Status != workflow.StatusFailed {
		writeError(w, http.StatusBadRequest, "invalid_request", "status must be in_progress, completed or failed", nil)
		return
	}
	f.Limit = 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 500", nil)
			return
		}
		f.Limit = n
	}
	runs := s.Engine.Store.List(f)
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "counts": s.Engine.Store.Counts()})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.Engine.Store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) getTrace(w http.ResponseWriter, r *http.Request) {
	spans, err := s.Engine.Store.Trace(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run_id": r.PathValue("id"), "spans": spans})
}

// CancelRequest is the body of POST /api/runs/{id}/cancel.
type CancelRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	var req CancelRequest
	if err := decode(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	ok, err := s.Engine.Cancel(r.PathValue("id"), req.Reason)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	run, _ := s.Engine.Store.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusConflict, "already_finished", fmt.Sprintf("run is already %s", run.Status), nil)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// runEvents streams run snapshots as SSE ("run" events) until the run
// finishes, then sends "done" and closes.
func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch, unsub, err := s.Engine.Store.Subscribe(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	defer unsub()
	sse, ok := newSSE(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported", nil)
		return
	}
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	throttle := time.NewTicker(150 * time.Millisecond)
	defer throttle.Stop()
	dirty := true
	for {
		if dirty {
			run, err := s.Engine.Store.Get(id)
			if err != nil {
				return
			}
			sse.send("run", run)
			dirty = false
			if run.Terminal() {
				sse.send("done", map[string]any{"run_id": id, "status": run.Status})
				return
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			// Coalesce bursts: wait for the next throttle tick.
			select {
			case <-throttle.C:
			case <-r.Context().Done():
				return
			}
			dirty = true
		case <-keepAlive.C:
			sse.comment("keep-alive")
		}
	}
}

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := workflow.Filter{Status: q.Get("status"), RunID: q.Get("run_id"), Kind: q.Get("kind")}
	if v := q.Get("blocking"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "blocking must be true or false", nil)
			return
		}
		f.Blocking = &b
	}
	items := s.Engine.Reviews.List(f)
	writeJSON(w, http.StatusOK, map[string]any{"reviews": items, "count": len(items)})
}

func (s *Server) getReview(w http.ResponseWriter, r *http.Request) {
	it, err := s.Engine.Reviews.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// DecisionRequest is the body of POST /api/reviews/{id}/decision.
type DecisionRequest struct {
	Action   string `json:"action"`
	Comment  string `json:"comment"`
	Reviewer string `json:"reviewer"`
}

func (s *Server) decideReview(w http.ResponseWriter, r *http.Request) {
	var req DecisionRequest
	if err := decode(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Action == workflow.ActionRevise && strings.TrimSpace(req.Comment) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "a revise decision needs a comment telling the editor what to change", nil)
		return
	}
	if len(req.Comment) > 2000 || len(req.Reviewer) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_request", "comment (2000) or reviewer (80) too long", nil)
		return
	}
	it, err := s.Engine.Reviews.Decide(r.PathValue("id"), workflow.Decision{Action: req.Action, Comment: req.Comment, Reviewer: req.Reviewer})
	switch {
	case errors.Is(err, workflow.ErrReviewNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, workflow.ErrReviewNotPending):
		writeError(w, http.StatusConflict, "not_pending", fmt.Sprintf("review is already %s", it.Status), it)
	case errors.Is(err, workflow.ErrActionNotAllowed):
		writeError(w, http.StatusBadRequest, "action_not_allowed", err.Error(), map[string]any{"allowed_actions": it.AllowedActions})
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", err.Error(), nil)
	default:
		writeJSON(w, http.StatusOK, it)
	}
}

// ---------------------------------------------------------------------------
// SSE writer
// ---------------------------------------------------------------------------

type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSE(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &sseWriter{w: w, f: f}, true
}

func (s *sseWriter) send(event string, v any) {
	raw, _ := json.Marshal(v)
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, raw)
	s.f.Flush()
}

func (s *sseWriter) comment(text string) {
	fmt.Fprintf(s.w, ": %s\n\n", text)
	s.f.Flush()
}

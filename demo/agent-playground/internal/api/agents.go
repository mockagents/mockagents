package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/router"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/tools"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
)

func (s *Server) view(cfg *config.Config, a config.Agent) agentView {
	v := agentView{Agent: a, EffectiveRetry: cfg.RetryFor(a)}
	if d, err := router.Route(cfg, a, ""); err == nil {
		v.EffectiveRoute = d
	} else {
		v.EffectiveRoute = map[string]any{"error": err.Error()}
	}
	if v.Tools == nil {
		v.Tools = []string{}
	}
	return v
}

func (s *Server) listAgents(w http.ResponseWriter, _ *http.Request) {
	cfg := s.Config.Current()
	out := make([]agentView, 0, len(cfg.Agents))
	for _, a := range cfg.Agents {
		out = append(out, s.view(cfg, a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config.Current()
	a, ok := cfg.Agent(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("agent %q not found", r.PathValue("name")), nil)
		return
	}
	v := s.view(cfg, a)
	var details []tools.Info
	for _, t := range s.Tools.List() {
		for _, n := range a.Tools {
			if n == t.Name {
				details = append(details, t)
			}
		}
	}
	v.ToolDetails = details
	writeJSON(w, http.StatusOK, v)
}

// patchAgent applies an RFC 7386 JSON merge patch to one agent's config.
func (s *Server) patchAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var patch map[string]any
	if err := decodeLoose(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if n, ok := patch["name"]; ok && n != name {
		writeError(w, http.StatusBadRequest, "invalid_request", "an agent's name cannot be changed", nil)
		return
	}
	var updated config.Agent
	cfg, version, err := s.Config.Update(func(c *config.Config) error {
		for i, a := range c.Agents {
			if a.Name != name {
				continue
			}
			next, err := mergeInto(a, patch)
			if err != nil {
				return err
			}
			next.Name = name
			c.Agents[i] = next
			updated = next
			return nil
		}
		return fmt.Errorf("%w: %q", config.ErrUnknownAgent, name)
	})
	if err != nil {
		s.configError(w, err)
		return
	}
	s.Logger.Info("agent reconfigured", "agent", name, "config_version", version)
	w.Header().Set("X-Config-Version", fmt.Sprint(version))
	writeJSON(w, http.StatusOK, s.view(cfg, updated))
}

func (s *Server) configError(w http.ResponseWriter, err error) {
	var ve *config.ValidationError
	switch {
	case errors.Is(err, config.ErrUnknownAgent):
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, "invalid_config", "the change was rejected; the previous configuration is still active", ve.Fields)
	default:
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error(), nil)
	}
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, version := s.Config.Get()
	w.Header().Set("X-Config-Version", fmt.Sprint(version))
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "config": cfg})
}

// patchConfig merge-patches defaults/router/workflows/pricing.
func (s *Server) patchConfig(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := decodeLoose(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if _, ok := patch["agents"]; ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "use PATCH /api/agents/{name} to change agents", nil)
		return
	}
	cfg, version, err := s.Config.Update(func(c *config.Config) error {
		next, err := mergeInto(*c, patch)
		if err != nil {
			return err
		}
		*c = next
		return nil
	})
	if err != nil {
		s.configError(w, err)
		return
	}
	s.Logger.Info("configuration updated", "config_version", version)
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "config": cfg})
}

func (s *Server) resetConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, version := s.Config.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "config": cfg})
}

func (s *Server) listTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tools": s.Tools.List()})
}

// ExecuteToolRequest is the body of POST /api/tools/{name}/execute.
type ExecuteToolRequest struct {
	Arguments map[string]any `json:"arguments"`
}

func (s *Server) executeTool(w http.ResponseWriter, r *http.Request) {
	t, ok := s.Tools.Get(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("tool %q not found", r.PathValue("name")), nil)
		return
	}
	if t.RequiresApproval() {
		writeError(w, http.StatusForbidden, "approval_required",
			"side-effecting tools only run inside an agent loop, behind a human approval gate", nil)
		return
	}
	var req ExecuteToolRequest
	if err := decode(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	raw, _ := json.Marshal(req.Arguments)
	args, err := tools.ParseArgs(t, string(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_arguments", err.Error(), nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	start := time.Now()
	res, err := t.Execute(ctx, args)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "tool_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tool": t.Spec().Name, "result": res, "duration_ms": time.Since(start).Milliseconds()})
}

// InvokeRequest is the body of POST /api/agents/{name}/invoke.
type InvokeRequest struct {
	Input   string              `json:"input"`
	Tier    string              `json:"tier,omitempty"`
	Stream  bool                `json:"stream,omitempty"`
	Options workflow.RunOptions `json:"options"`
}

// InvokeResponse is the synchronous invoke result.
type InvokeResponse struct {
	RunID          string                `json:"run_id"`
	Status         workflow.Status       `json:"status"`
	Phase          string                `json:"phase"`
	Result         any                   `json:"result,omitempty"`
	Error          *workflow.RunError    `json:"error,omitempty"`
	PendingReviews []workflow.ReviewItem `json:"pending_reviews,omitempty"`
	Links          map[string]string     `json:"links"`
}

func (s *Server) invokeAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := s.Config.Current().Agent(name); !ok {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("agent %q not found", name), nil)
		return
	}
	var req InvokeRequest
	if err := decode(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	stream := req.Stream || r.URL.Query().Get("stream") == "true" || strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	opts := req.Options
	opts.Stream = opts.Stream || stream
	input := map[string]any{"agent": name, "input": req.Input}
	if req.Tier != "" {
		input["tier"] = req.Tier
	}
	if !stream {
		run, err := s.Engine.Start("agent", input, opts, workflow.StartOptions{})
		if err != nil {
			s.startError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.MaxWait)
		defer cancel()
		if got, err := s.Engine.Wait(ctx, run.ID, settled); err == nil {
			run = got
		}
		status := http.StatusOK
		if !run.Terminal() {
			status = http.StatusAccepted
		}
		writeJSON(w, status, s.invokeResponse(run))
		return
	}

	// Streaming: forward agent events as SSE. The channel send never blocks;
	// a slow or vanished client cannot stall the agent (events are dropped).
	events := make(chan agents.Event, 1024)
	run, err := s.Engine.Start("agent", input, opts, workflow.StartOptions{OnEvent: func(ev agents.Event) {
		select {
		case events <- ev:
		default:
		}
	}})
	if err != nil {
		s.startError(w, err)
		return
	}
	ch, unsub, err := s.Engine.Store.Subscribe(run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error(), nil)
		return
	}
	defer unsub()
	sse, ok := newSSE(w)
	if !ok {
		return
	}
	sse.send("run", map[string]any{"run_id": run.ID, "status": run.Status})
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	drain := func() {
		for {
			select {
			case ev := <-events:
				sse.send(ev.Type, ev.Data)
			default:
				return
			}
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return // the run continues server-side and stays queryable
		case ev := <-events:
			sse.send(ev.Type, ev.Data)
		case <-keepAlive.C:
			sse.comment("keep-alive")
		case <-ch:
			cur, err := s.Engine.Store.Get(run.ID)
			if err != nil {
				return
			}
			if settled(cur) {
				drain()
				sse.send("done", s.invokeResponse(cur))
				return
			}
		}
	}
}

func (s *Server) invokeResponse(run *workflow.Run) InvokeResponse {
	resp := InvokeResponse{RunID: run.ID, Status: run.Status, Phase: run.Phase, Error: run.Error, Result: run.Output,
		Links: map[string]string{"run": "/api/runs/" + run.ID, "trace": "/api/runs/" + run.ID + "/trace"}}
	if run.Waiting() {
		resp.PendingReviews = s.Engine.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewPending, Blocking: ptr(true)})
	}
	return resp
}

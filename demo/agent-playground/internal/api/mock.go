package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The /api/mock/* endpoints proxy the mockagents management API, so the UI
// (same origin, no CORS) and API users can see the mock's side of every
// call: its agent catalog, raw interaction logs with cost, the cost rollup,
// native pipelines, and runtime fixture authoring.

func (s *Server) proxy(w http.ResponseWriter, r *http.Request, method, path string, query url.Values, contentType string) {
	s.proxyAccept(w, r, method, path, query, contentType, "")
}

func (s *Server) proxyAccept(w http.ResponseWriter, r *http.Request, method, path string, query url.Values, contentType, accept string) {
	var body []byte
	if r.Body != nil && (method == http.MethodPost || method == http.MethodPut) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		body = b
		if contentType == "" {
			contentType = r.Header.Get("Content-Type")
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	resp, err := s.Mock.Do(ctx, method, path, query, body, contentType, accept)
	if err != nil {
		writeError(w, http.StatusBadGateway, "mock_unreachable", err.Error(), map[string]any{"mock_url": s.Mock.BaseURL()})
		return
	}
	ct := resp.ContentType
	if ct == "" {
		ct = "application/json; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Proxied-From", "mockagents")
	w.WriteHeader(resp.Status)
	_, _ = w.Write(resp.Body)
}

func (s *Server) mockStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	health, err := s.Mock.Health(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "mock_unreachable", err.Error(), map[string]any{"mock_url": s.Mock.BaseURL()})
		return
	}
	list, err := s.Mock.Agents(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "mock_unreachable", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": s.Mock.BaseURL(), "mode": s.MockMode, "health": health, "agents": list})
}

func (s *Server) mockLogs(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for _, k := range []string{"limit", "offset", "agent", "since", "until", "session_id", "session_prefix", "fields"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	if q.Get("limit") == "" {
		q.Set("limit", "50")
	}
	s.proxy(w, r, http.MethodGet, "/api/v1/logs", q, "")
}

func (s *Server) mockCosts(w http.ResponseWriter, r *http.Request) {
	s.proxy(w, r, http.MethodGet, "/api/v1/costs", r.URL.Query(), "")
}

func (s *Server) mockPipelines(w http.ResponseWriter, r *http.Request) {
	s.proxy(w, r, http.MethodGet, "/api/v1/pipelines", nil, "")
}

func (s *Server) mockRunPipeline(w http.ResponseWriter, r *http.Request) {
	s.proxy(w, r, http.MethodPost, "/api/v1/pipelines/"+url.PathEscape(r.PathValue("name"))+"/run", nil, "application/json")
}

// mockGetAgent returns the full fixture definition: JSON by default,
// canonical YAML with ?format=yaml or Accept: application/yaml.
func (s *Server) mockGetAgent(w http.ResponseWriter, r *http.Request) {
	accept := ""
	if r.URL.Query().Get("format") == "yaml" || strings.Contains(r.Header.Get("Accept"), "yaml") {
		accept = "application/yaml"
	}
	s.proxyAccept(w, r, http.MethodGet, "/api/v1/agents/"+url.PathEscape(r.PathValue("name")), nil, "", accept)
}

// mockPutAgent forwards a YAML (or JSON) agent definition to the mockagents
// write API, creating or replacing the fixture at runtime.
func (s *Server) mockPutAgent(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct == "" || strings.HasPrefix(ct, "text/plain") {
		ct = "application/yaml"
	}
	s.proxy(w, r, http.MethodPut, "/api/v1/agents/"+url.PathEscape(r.PathValue("name")), nil, ct)
}

func (s *Server) mockReloadAgent(w http.ResponseWriter, r *http.Request) {
	s.proxy(w, r, http.MethodPost, "/api/v1/agents/"+url.PathEscape(r.PathValue("name"))+"/reload", nil, "application/json")
}

func (s *Server) mockValidate(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct == "" || strings.HasPrefix(ct, "text/plain") {
		ct = "application/yaml"
	}
	s.proxy(w, r, http.MethodPost, "/api/v1/config/validate", nil, ct)
}

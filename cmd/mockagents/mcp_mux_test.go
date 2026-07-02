package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/mcp"
	"github.com/mockagents/mockagents/internal/types"
)

// Round-10 R10-1: every documented MCP endpoint must actually be mounted by
// the CLI's HTTP server — the bidirectional surface (events/response/sample/
// roots) shipped for two releases as dead code that the README, api-spec, and
// all three SDKs targeted while every request 404'd.
func TestMCPMuxMountsDocumentedEndpoints(t *testing.T) {
	server := mcp.NewServer(&types.MCPServerDefinition{
		Kind:     types.MCPServerKind,
		Metadata: types.Metadata{Name: "mux-test"},
		Spec:     types.MCPServerSpec{Capabilities: types.MCPCapabilities{Tools: true}},
	})
	srv := httptest.NewServer(newMCPMux(server))
	defer srv.Close()

	// Anything but 404/405 counts as mounted (handlers validate their own
	// methods/headers — this test only guards the routing table).
	// Probe every route with a method its handler rejects: a mounted handler
	// answers 405 (or its own validation error), an unmounted path answers
	// the mux's 404. This avoids conflating a handler's own 404s (e.g.
	// /mcp/response with no pending request) — and avoids blocking on the
	// SSE stream.
	for _, path := range []string{
		"/mcp", "/mcp/rpc", "/mcp/notify",
		"/mcp/events", "/mcp/response", "/mcp/sample", "/mcp/roots",
		"/healthz",
	} {
		req, _ := http.NewRequest("PATCH", srv.URL+path, strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PATCH %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("%s not mounted (mux 404)", path)
		}
	}
}

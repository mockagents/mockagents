package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/mcp"
	"github.com/mockagents/mockagents/internal/mcpadmin"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run a mock Model Context Protocol (MCP) server",
	Long: `Load one or more kind: MCPServer definitions from --agents-dir and
serve them over either HTTP (Streamable HTTP at /mcp) or stdio (line-delimited
JSON). The HTTP transport implements the current MCP Streamable HTTP revision:
a single /mcp endpoint answering POST (JSON or SSE), GET (resumable server→client
SSE stream), and DELETE (session termination), with Mcp-Session-Id session
tracking and Origin / MCP-Protocol-Version validation.

Examples:
  mockagents mcp --transport http --port 8081 --agents-dir ./agents
  mockagents mcp --transport stdio --server weather-mcp --agents-dir ./agents

When multiple MCPServer definitions are present, --server selects which
one to expose; for HTTP transport a single server can also be served
unambiguously by name when only one is loaded.

With --manage, the server also exposes built-in agent-management tools
(list_agents, get_agent, validate_agent, create_agent, put_agent,
delete_agent) backed by the MockAgents write API, so an MCP client can
manage the agents loaded from --agents-dir over MCP. --manage works even
when no kind:MCPServer document exists (it serves a synthetic admin server).

Examples:
  mockagents mcp --manage --agents-dir ./agents               # admin tools only
  mockagents mcp --manage --server weather-mcp --agents-dir ./agents`,
	RunE: runMCP,
}

var (
	mcpTransport  string
	mcpPort       int
	mcpServerName string
	mcpManage     bool
)

func init() {
	mcpCmd.Flags().StringVar(&mcpTransport, "transport", "http", "Transport: http or stdio")
	mcpCmd.Flags().IntVarP(&mcpPort, "port", "p", 8081, "HTTP port when --transport=http")
	mcpCmd.Flags().StringVar(&mcpServerName, "server", "", "Name of the MCPServer to serve (required when multiple are loaded)")
	mcpCmd.Flags().BoolVar(&mcpManage, "manage", false, "Also expose built-in agent-management tools backed by the write API")
	rootCmd.AddCommand(mcpCmd)
}

// adminServerName is the synthetic MCPServer name used when --manage runs
// without any kind:MCPServer document of its own.
const adminServerName = "mockagents-admin"

func runMCP(cmd *cobra.Command, args []string) error {
	agentsDir, _ := cmd.Flags().GetString("agents-dir")
	docs, loadErrs := config.LoadAllDocuments(agentsDir)
	for _, e := range loadErrs {
		fmt.Fprintln(os.Stderr, "load error:", e)
	}

	def, err := selectMCPServer(docs, agentsDir)
	if err != nil {
		return err
	}

	server := mcp.NewServer(def)

	// --manage attaches the built-in agent-management tools, backed by a registry
	// loaded from --agents-dir. It composes with a declarative server (the admin
	// tools are added alongside the def's own tools).
	if mcpManage {
		registry := buildManageRegistry(docs)
		mcpadmin.NewManager(registry, agentsDir, "").Register(server)
		// Stderr, NEVER stdout: on --transport stdio, stdout IS the JSON-RPC
		// stream and the spec forbids non-MCP bytes on it — this banner on
		// stdout corrupted the official SDK's session (round-10 R10-4).
		fmt.Fprintf(os.Stderr, "mockagents mcp: agent-management tools enabled (%d agents loaded from %q)\n",
			len(registry.ListForTenant("")), agentsDir)
	}

	switch mcpTransport {
	case "http":
		return serveMCPHTTP(server, mcpPort)
	case "stdio":
		return mcp.ServeStdio(server, os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("unknown transport %q (valid: http, stdio)", mcpTransport)
	}
}

// selectMCPServer picks the MCPServer definition to expose. With --server it
// selects by name; with exactly one loaded it uses that; with several it errors
// asking for --server. When none are loaded it errors UNLESS --manage is set, in
// which case it returns a synthetic admin server so the management tools can be
// served without any kind:MCPServer document.
func selectMCPServer(docs *config.Documents, agentsDir string) (*types.MCPServerDefinition, error) {
	if len(docs.MCPServers) == 0 {
		if mcpManage {
			return &types.MCPServerDefinition{
				Kind:     types.MCPServerKind,
				Metadata: types.Metadata{Name: adminServerName},
				Spec:     types.MCPServerSpec{Capabilities: types.MCPCapabilities{Tools: true}},
			}, nil
		}
		return nil, fmt.Errorf("no kind:%s definitions found in %q (pass --manage to serve the built-in agent-management tools without one)", types.MCPServerKind, agentsDir)
	}

	switch {
	case mcpServerName != "":
		for _, r := range docs.MCPServers {
			if r.Definition.Metadata.Name == mcpServerName {
				return r.Definition, nil
			}
		}
		return nil, fmt.Errorf("mcp server %q not found in %q", mcpServerName, agentsDir)
	case len(docs.MCPServers) == 1:
		return docs.MCPServers[0].Definition, nil
	default:
		names := make([]string, 0, len(docs.MCPServers))
		for _, r := range docs.MCPServers {
			names = append(names, r.Definition.Metadata.Name)
		}
		return nil, fmt.Errorf("multiple MCPServer definitions loaded; pick one with --server (%v)", names)
	}
}

// buildManageRegistry builds an agent registry from the loaded Agent documents,
// mirroring start.go: apply defaults, validate, and register the valid ones with
// their source path so the management tools update/delete the exact backing file.
func buildManageRegistry(docs *config.Documents) *engine.AgentRegistry {
	registry := engine.NewAgentRegistry()
	validator := &config.Validator{}
	for _, result := range docs.Agents {
		config.ApplyDefaults(result.Definition)
		if errList := validator.Validate(result.Definition, result.FilePath, result.Node); errList != nil {
			fmt.Fprintf(os.Stderr, "skipping invalid agent %q: %v\n", result.FilePath, errList.Error())
			continue
		}
		registry.RegisterWithSource(result.Definition, result.FilePath)
	}
	return registry
}

func serveMCPHTTP(server *mcp.Server, port int) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           newMCPMux(server),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Printf("mockagents mcp listening on :%d (server=%s)\n", port, server.Definition().Metadata.Name)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// newMCPMux builds the full HTTP route set for `mockagents mcp` — extracted
// so tests can assert every documented endpoint is actually mounted (R10-1:
// the bidirectional surface existed for two releases without ever being
// reachable from a runnable server).
func newMCPMux(server *mcp.Server) *http.ServeMux {
	mux := http.NewServeMux()
	// `/mcp` speaks the current Streamable HTTP transport (single endpoint:
	// POST/GET/DELETE, Mcp-Session-Id lifecycle, SSE-on-POST, GET resumability).
	// The legacy POST-only JSON transport is still available at `/mcp/rpc` for
	// older clients, and `/mcp/notify` pushes a server notification onto every
	// live streamable session's GET stream.
	streamable := mcp.NewStreamableHTTPHandler(server)
	mux.Handle("/mcp", streamable)
	mux.Handle("/mcp/rpc", mcp.NewHTTPHandler(server))
	mux.Handle("/mcp/notify", mcp.NewStreamableNotifyHandler(streamable))
	// The bidirectional/admin surface (v0.3): a server-initiated SSE stream +
	// the response route back, plus the sampling/roots admin triggers. These
	// were documented (README, api-spec, all three SDKs' McpClient) but never
	// mounted, so every sampling/roots flow 404'd against a runnable server
	// (round-10 R10-1). Routing sampling over the Streamable HTTP GET stream
	// is the recorded follow-on; this makes the documented surface real.
	mux.Handle("/mcp/events", mcp.NewEventStreamHandler(server))
	mux.Handle("/mcp/response", mcp.NewResponseHandler(server))
	mux.Handle("/mcp/sample", mcp.NewSendRequestHandler(server, "sampling/createMessage"))
	mux.Handle("/mcp/roots", mcp.NewSendRequestHandler(server, "roots/list"))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	return mux
}

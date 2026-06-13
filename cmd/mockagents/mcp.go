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
	"github.com/mockagents/mockagents/internal/mcp"
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
unambiguously by name when only one is loaded.`,
	RunE: runMCP,
}

var (
	mcpTransport  string
	mcpPort       int
	mcpServerName string
)

func init() {
	mcpCmd.Flags().StringVar(&mcpTransport, "transport", "http", "Transport: http or stdio")
	mcpCmd.Flags().IntVarP(&mcpPort, "port", "p", 8081, "HTTP port when --transport=http")
	mcpCmd.Flags().StringVar(&mcpServerName, "server", "", "Name of the MCPServer to serve (required when multiple are loaded)")
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	agentsDir, _ := cmd.Flags().GetString("agents-dir")
	docs, loadErrs := config.LoadAllDocuments(agentsDir)
	for _, e := range loadErrs {
		fmt.Fprintln(os.Stderr, "load error:", e)
	}
	if len(docs.MCPServers) == 0 {
		return fmt.Errorf("no kind:%s definitions found in %q", types.MCPServerKind, agentsDir)
	}

	var def *types.MCPServerDefinition
	switch {
	case mcpServerName != "":
		for _, r := range docs.MCPServers {
			if r.Definition.Metadata.Name == mcpServerName {
				def = r.Definition
				break
			}
		}
		if def == nil {
			return fmt.Errorf("mcp server %q not found in %q", mcpServerName, agentsDir)
		}
	case len(docs.MCPServers) == 1:
		def = docs.MCPServers[0].Definition
	default:
		names := make([]string, 0, len(docs.MCPServers))
		for _, r := range docs.MCPServers {
			names = append(names, r.Definition.Metadata.Name)
		}
		return fmt.Errorf("multiple MCPServer definitions loaded; pick one with --server (%v)", names)
	}

	server := mcp.NewServer(def)

	switch mcpTransport {
	case "http":
		return serveMCPHTTP(server, mcpPort)
	case "stdio":
		return mcp.ServeStdio(server, os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("unknown transport %q (valid: http, stdio)", mcpTransport)
	}
}

func serveMCPHTTP(server *mcp.Server, port int) error {
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
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
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

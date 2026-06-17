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

	"github.com/mockagents/mockagents/internal/a2a"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/spf13/cobra"
)

var a2aCmd = &cobra.Command{
	Use:   "a2a",
	Short: "Run a mock A2A (Agent2Agent) server",
	Long: `Load a kind: A2AServer definition from --agents-dir and serve it over the A2A
protocol: the public Agent Card at GET /.well-known/agent-card.json plus the
JSON-RPC 2.0 endpoint at POST / (message/send, tasks/get, tasks/cancel). The
server answers message/send with the document's canned, match-based responses
and models the task lifecycle.

When multiple A2AServer definitions are present, --server selects which one to
expose.

Examples:
  mockagents a2a --port 8083 --agents-dir ./agents
  mockagents a2a --server weather-a2a --agents-dir ./agents`,
	RunE: runA2A,
}

var (
	a2aPort       int
	a2aServerName string
)

func init() {
	a2aCmd.Flags().IntVarP(&a2aPort, "port", "p", 8083, "HTTP port")
	a2aCmd.Flags().StringVar(&a2aServerName, "server", "", "Name of the A2AServer to serve (required when multiple are loaded)")
	rootCmd.AddCommand(a2aCmd)
}

func runA2A(cmd *cobra.Command, args []string) error {
	agentsDir, _ := cmd.Flags().GetString("agents-dir")
	docs, loadErrs := config.LoadAllDocuments(agentsDir)
	for _, e := range loadErrs {
		fmt.Fprintln(os.Stderr, "load error:", e)
	}

	def, err := selectA2AServer(docs, agentsDir)
	if err != nil {
		return err
	}
	return serveA2AHTTP(a2a.NewServer(def), def.Spec.Card.Name, a2aPort)
}

func selectA2AServer(docs *config.Documents, agentsDir string) (*types.A2AServerDefinition, error) {
	if len(docs.A2AServers) == 0 {
		return nil, fmt.Errorf("no kind:%s definitions found in %q", types.A2AServerKind, agentsDir)
	}
	switch {
	case a2aServerName != "":
		for _, r := range docs.A2AServers {
			if r.Definition.Metadata.Name == a2aServerName {
				return r.Definition, nil
			}
		}
		return nil, fmt.Errorf("a2a server %q not found in %q", a2aServerName, agentsDir)
	case len(docs.A2AServers) == 1:
		return docs.A2AServers[0].Definition, nil
	default:
		names := make([]string, 0, len(docs.A2AServers))
		for _, r := range docs.A2AServers {
			names = append(names, r.Definition.Metadata.Name)
		}
		return nil, fmt.Errorf("multiple A2AServer definitions loaded; pick one with --server (%v)", names)
	}
}

func serveA2AHTTP(server *a2a.Server, cardName string, port int) error {
	mux := http.NewServeMux()
	// Agent Card discovery: the current well-known path plus the older alias.
	mux.HandleFunc("GET /.well-known/agent-card.json", server.CardHandler())
	mux.HandleFunc("GET /.well-known/agent.json", server.CardHandler())
	// JSON-RPC endpoint (the card advertises this origin's root as its url).
	mux.HandleFunc("POST /", server.RPCHandler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Printf("mockagents a2a listening on :%d (agent=%s)\n", port, cardName)

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

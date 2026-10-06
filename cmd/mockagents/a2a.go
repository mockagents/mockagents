package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
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

The server binds 127.0.0.1 by default; pass --bind 0.0.0.0 to expose it.

Examples:
  mockagents a2a --port 8083 --agents-dir ./agents
  mockagents a2a --server weather-a2a --agents-dir ./agents`,
	RunE: runA2A,
}

var (
	a2aPort         int
	a2aBind         string
	a2aServerName   string
	a2aMaxTasks     int
	a2aMaxTaskBytes int
	a2aMaxHistory   int
	a2aTaskTTL      time.Duration
)

func init() {
	a2aCmd.Flags().IntVarP(&a2aPort, "port", "p", 8083, "HTTP port")
	a2aCmd.Flags().StringVar(&a2aBind, "bind", "127.0.0.1", "Interface to bind (0.0.0.0 to expose)")
	a2aCmd.Flags().StringVar(&a2aServerName, "server", "", "Name of the A2AServer to serve (required when multiple are loaded)")
	a2aCmd.Flags().IntVar(&a2aMaxTasks, "max-tasks", a2a.DefaultMaxTasks, "Maximum retained A2A tasks")
	a2aCmd.Flags().IntVar(&a2aMaxTaskBytes, "max-task-bytes", a2a.DefaultMaxTaskBytes, "Maximum bytes retained by A2A tasks")
	a2aCmd.Flags().IntVar(&a2aMaxHistory, "max-task-history", a2a.DefaultMaxTaskHistory, "Maximum retained messages per A2A task")
	a2aCmd.Flags().DurationVar(&a2aTaskTTL, "task-ttl", a2a.DefaultTaskTTL, "Retention time for A2A tasks after their last update (terminal or idle)")
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
	server := a2a.NewServerWithOptions(def, a2a.ServerOptions{MaxTasks: a2aMaxTasks, MaxTaskBytes: a2aMaxTaskBytes, MaxHistory: a2aMaxHistory, TaskTTL: a2aTaskTTL})
	return serveA2AHTTP(server, def.Spec.Card.Name, a2aBind, a2aPort)
}

func selectA2AServer(docs *config.Documents, agentsDir string) (*types.A2AServerDefinition, error) {
	if len(docs.A2AServers) == 0 {
		return nil, fmt.Errorf("no kind:%s definitions found in %q", types.A2AServerKind, agentsDir)
	}
	switch {
	case a2aServerName != "":
		for _, r := range docs.A2AServers {
			if r.Definition.Metadata.Name == a2aServerName {
				return validA2AServer(r)
			}
		}
		return nil, fmt.Errorf("a2a server %q not found in %q", a2aServerName, agentsDir)
	case len(docs.A2AServers) == 1:
		return validA2AServer(docs.A2AServers[0])
	default:
		names := make([]string, 0, len(docs.A2AServers))
		for _, r := range docs.A2AServers {
			names = append(names, r.Definition.Metadata.Name)
		}
		return nil, fmt.Errorf("multiple A2AServer definitions loaded; pick one with --server (%v)", names)
	}
}

// validA2AServer refuses to serve a definition its validator rejects. Serving
// one used to be possible because only `validate` ran the validator: a fault
// status_code of 42 then panicked net/http on every request.
func validA2AServer(r *config.A2AServerLoadResult) (*types.A2AServerDefinition, error) {
	if errs := config.ValidateA2AServer(r.Definition, r.FilePath, r.Node); errs != nil {
		return nil, fmt.Errorf("a2a server %q is invalid:\n%s", r.Definition.Metadata.Name, errs.Error())
	}
	return r.Definition, nil
}

// newA2AMux builds the HTTP route set for `mockagents a2a`, extracted so
// tests can exercise every route without binding a port.
func newA2AMux(server *a2a.Server) *http.ServeMux {
	mux := http.NewServeMux()
	// Agent Card discovery: the current well-known path plus the older alias.
	mux.HandleFunc("GET /.well-known/agent-card.json", server.CardHandler())
	mux.HandleFunc("GET /.well-known/agent.json", server.CardHandler())
	// JSON-RPC endpoint (the card advertises this origin's root as its url).
	mux.HandleFunc("POST /", server.RPCHandler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	return mux
}

// serveA2AHTTP serves the A2A routes on bind:port (loopback by default, like
// mcp, record and replay) until SIGINT/SIGTERM.
func serveA2AHTTP(server *a2a.Server, cardName, bind string, port int) error {
	srv := &http.Server{
		Handler:           newA2AMux(server),
		ReadHeaderTimeout: 10 * time.Second,
		// Bound the request read and idle keep-alives like record/replay
		// (audit L-43). No WriteTimeout: the SSE streams are long-lived.
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
	if err != nil {
		return err
	}

	fmt.Printf("mockagents a2a listening on %s (agent=%s)\n", ln.Addr(), cardName)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	sigCh := make(chan os.Signal, 1)
	notifySignals(sigCh, syscall.SIGINT, syscall.SIGTERM)

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

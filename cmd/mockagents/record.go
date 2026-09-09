package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mockagents/mockagents/internal/recording"
	"github.com/spf13/cobra"
)

var recordCmd = &cobra.Command{
	Use:   "record",
	Short: "Proxy requests to a real upstream LLM API and record them to a cassette",
	Long: `Start an HTTP server that forwards /v1/chat/completions and /v1/messages
requests to a real upstream (OpenAI, Anthropic, or compatible), captures every
exchange to a JSON-lines cassette file, and returns the upstream response to
the client. Point your SDK at this server to record live interactions that
can later be replayed offline with "mockagents replay".

Streaming (SSE) responses are tee'd: each chunk is relayed to the client and
captured to the cassette, then replayed in order by "mockagents replay".`,
	RunE: runRecord,
}

var (
	recordUpstream       string
	recordCassette       string
	recordPort           int
	recordBind           string
	recordAPIKey         string
	recordRedact         bool
	recordRedactPatterns []string
)

func init() {
	recordCmd.Flags().StringVar(&recordUpstream, "upstream", "", "Upstream base URL (e.g. https://api.openai.com) [required]")
	recordCmd.Flags().StringVar(&recordCassette, "cassette", "cassette.jsonl", "Path to the cassette file to write")
	recordCmd.Flags().IntVarP(&recordPort, "port", "p", 8080, "Port to listen on")
	recordCmd.Flags().StringVar(&recordBind, "bind", "127.0.0.1", "Interface to bind (0.0.0.0 to expose; the proxy forwards --api-key to the upstream for anyone who can reach it)")
	recordCmd.Flags().StringVar(&recordAPIKey, "api-key", "", "API key to forward to upstream (overrides client Authorization header)")
	recordCmd.Flags().BoolVar(&recordRedact, "redact", false, "Mask common secret formats (sk-*, key-*, Bearer, AWS/GitHub/Slack/Google keys, JWTs) in recorded cassette bodies before they are written. Best-effort: review the cassette before committing")
	recordCmd.Flags().StringArrayVar(&recordRedactPatterns, "redact-pattern", nil, "Additional regexp to mask in recorded bodies (repeatable; implies --redact). Applied to JSON string values only, so a pattern can never break the cassette's structure")
	rootCmd.AddCommand(recordCmd)
}

func runRecord(cmd *cobra.Command, args []string) error {
	if recordUpstream == "" {
		return errors.New("--upstream is required")
	}

	cass, err := recording.Load(recordCassette)
	if err != nil {
		return fmt.Errorf("loading cassette: %w", err)
	}
	proxy, err := recording.NewProxy(recordUpstream, cass)
	if err != nil {
		return fmt.Errorf("building proxy: %w", err)
	}
	proxy.UpstreamAPIKey = recordAPIKey

	if recordRedact || len(recordRedactPatterns) > 0 {
		rd, err := recording.NewRedactor(recordRedactPatterns)
		if err != nil {
			return fmt.Errorf("compiling redact patterns: %w", err)
		}
		proxy.Redactor = rd
		fmt.Println("redaction enabled: API keys + custom patterns will be masked in the cassette")
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", proxy)
	mux.Handle("/v1/messages", proxy)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok: %d interactions recorded\n", cass.Len())
	})

	// Loopback by default (audit M-22): the proxy relays the operator's
	// provider key upstream for every request it receives.
	addr := net.JoinHostPort(recordBind, strconv.Itoa(recordPort))
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	fmt.Printf("mockagents record listening on %s -> %s (cassette=%s)\n",
		addr, recordUpstream, recordCassette)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		fmt.Printf("\nrecorded %d interactions to %s\n", cass.Len(), recordCassette)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

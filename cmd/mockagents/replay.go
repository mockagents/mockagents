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

	"github.com/mockagents/mockagents/internal/recording"
	"github.com/spf13/cobra"
)

var replayCmd = &cobra.Command{
	Use:   "replay",
	Short: "Serve a cassette file over the OpenAI/Anthropic API endpoints",
	Long: `Start an HTTP server that replies to /v1/chat/completions and /v1/messages
requests using interactions previously captured with "mockagents record".
Requests that don't match any recorded interaction return 404 by default.`,
	RunE: runReplay,
}

var (
	replayCassette       string
	replayPort           int
	replayMatchIgnore    []string
	replayRecordMode     string
	replayUpstream       string
	replayAPIKey         string
	replayRedact         bool
	replayRedactPatterns []string
)

func init() {
	replayCmd.Flags().StringVar(&replayCassette, "cassette", "cassette.jsonl", "Path to the cassette file to load")
	replayCmd.Flags().IntVarP(&replayPort, "port", "p", 8080, "Port to listen on")
	replayCmd.Flags().StringArrayVar(&replayMatchIgnore, "match-ignore", nil, "Top-level request body field to ignore when matching (repeatable, e.g. --match-ignore temperature --match-ignore seed). Replay-time only; the cassette is unchanged")
	replayCmd.Flags().StringVar(&replayRecordMode, "record-mode", "none", "Record mode: none (replay only) | new_episodes (record on miss) | once (record only if the cassette is new) | all (always forward+record)")
	replayCmd.Flags().StringVar(&replayUpstream, "upstream", "", "Upstream base URL for record-on-miss (required for new_episodes/all and for once on a new cassette)")
	replayCmd.Flags().StringVar(&replayAPIKey, "api-key", "", "API key to forward to the upstream when recording on miss")
	replayCmd.Flags().BoolVar(&replayRedact, "redact", false, "Mask secrets in newly-recorded cassette bodies (see `mockagents record --redact`)")
	replayCmd.Flags().StringArrayVar(&replayRedactPatterns, "redact-pattern", nil, "Additional regexp to mask in newly-recorded bodies (repeatable; implies --redact)")
	rootCmd.AddCommand(replayCmd)
}

// buildRecordProxy constructs the recording proxy used by the recording modes,
// mirroring the `record` command's api-key/redact wiring. skipOnError suppresses
// caching of upstream 4xx/5xx — true for the record-on-miss fallback (don't
// cache a transient error as the canonical replay), false for `all` (a faithful
// re-record session captures errors too, like `mockagents record`).
func buildRecordProxy(cass *recording.Cassette, skipOnError bool) (*recording.Proxy, error) {
	proxy, err := recording.NewProxy(replayUpstream, cass)
	if err != nil {
		return nil, fmt.Errorf("building record proxy: %w", err)
	}
	proxy.UpstreamAPIKey = replayAPIKey
	proxy.SkipRecordOnError = skipOnError
	if replayRedact || len(replayRedactPatterns) > 0 {
		rd, err := recording.NewRedactor(replayRedactPatterns)
		if err != nil {
			return nil, fmt.Errorf("compiling redact patterns: %w", err)
		}
		proxy.Redactor = rd
	}
	return proxy, nil
}

func runReplay(cmd *cobra.Command, args []string) error {
	mode, err := recording.ParseRecordMode(replayRecordMode)
	if err != nil {
		return err
	}

	cass, err := recording.Load(replayCassette)
	if err != nil {
		return fmt.Errorf("loading cassette: %w", err)
	}

	// `once` is resolved against whether anything is RECORDED, not file
	// presence: an empty (or absent) cassette means "nothing recorded yet" →
	// record like new_episodes; a populated cassette → replay only. (Using
	// os.Stat would make a 0-byte file from a prior empty run fail to record.)
	effective := mode.Resolve(cass.Len())
	// Validate the upstream requirement against the EFFECTIVE mode, so `once` on
	// a populated cassette doesn't demand an upstream it won't use.
	if effective.RequiresUpstream() && replayUpstream == "" {
		return fmt.Errorf("--upstream is required for --record-mode=%s", mode)
	}
	// Replay-only over an empty cassette is a no-op; keep the original guard.
	// Recording modes may legitimately start from an empty/new cassette.
	if effective == recording.RecordModeNone && cass.Len() == 0 {
		return fmt.Errorf("cassette %q is empty or does not exist", replayCassette)
	}

	var handler http.Handler
	switch effective {
	case recording.RecordModeAll:
		// all = faithful re-record/passthrough: capture errors too (skipOnError
		// false), matching `mockagents record`.
		proxy, err := buildRecordProxy(cass, false)
		if err != nil {
			return err
		}
		handler = proxy
		if len(replayMatchIgnore) > 0 {
			fmt.Println("mockagents replay: --match-ignore is ignored in --record-mode=all (passthrough)")
		}
		fmt.Printf("mockagents replay: record-mode=all → forwarding + recording every request to %s\n", replayUpstream)
	default: // none or new_episodes
		rp := recording.NewReplay(cass)
		if len(replayMatchIgnore) > 0 {
			rp.Matcher = recording.NewMatcher(replayMatchIgnore)
			fmt.Printf("mockagents replay: match-ignore=%v (%d field(s))\n", replayMatchIgnore, len(replayMatchIgnore))
		}
		if effective.Records() {
			// record-on-miss: don't cache a transient upstream error.
			proxy, err := buildRecordProxy(cass, true)
			if err != nil {
				return err
			}
			rp.Fallback = proxy
			fmt.Printf("mockagents replay: record-mode=%s → record-on-miss from %s\n", mode, replayUpstream)
		}
		handler = rp
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", handler)
	mux.Handle("/v1/messages", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok: %d interactions loaded\n", cass.Len())
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", replayPort),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Printf("mockagents replay listening on :%d (cassette=%s, %d interactions)\n",
		replayPort, replayCassette, cass.Len())

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

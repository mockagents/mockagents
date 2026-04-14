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
	replayCassette string
	replayPort     int
)

func init() {
	replayCmd.Flags().StringVar(&replayCassette, "cassette", "cassette.jsonl", "Path to the cassette file to load")
	replayCmd.Flags().IntVarP(&replayPort, "port", "p", 8080, "Port to listen on")
	rootCmd.AddCommand(replayCmd)
}

func runReplay(cmd *cobra.Command, args []string) error {
	cass, err := recording.Load(replayCassette)
	if err != nil {
		return fmt.Errorf("loading cassette: %w", err)
	}
	if cass.Len() == 0 {
		return fmt.Errorf("cassette %q is empty or does not exist", replayCassette)
	}

	rp := recording.NewReplay(cass)

	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", rp)
	mux.Handle("/v1/messages", rp)
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

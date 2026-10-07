// Package mockhost runs a complete mockagents server inside the playground
// process (the "embedded mock"), so `playground serve` is a single command
// with nothing else to install.
//
// It wires the same pieces as `mockagents start`: the agent registry
// (validated), pipelines, interaction logging, the price table, and the
// full HTTP surface (provider endpoints plus the management API). The
// playground still talks to it over HTTP, exactly as it would to an external
// mockagents server or a real provider. Only the process boundary is gone.
package mockhost

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/server"
	"github.com/mockagents/mockagents/internal/storage"
)

// Options configure the embedded server.
type Options struct {
	// AgentsDir holds the mockagents YAML documents (agents + pipelines).
	AgentsDir string
	// PricingFile is an optional MOCKAGENTS_PRICING-format YAML.
	PricingFile string
	// Addr is host:port; "127.0.0.1:0" picks a free port.
	Addr string
	// DataDir holds the interaction-log SQLite file ("" = a temp dir).
	DataDir string
	Logger  *slog.Logger
}

// Host is a running embedded mockagents server.
type Host struct {
	srv      *server.Server
	logStore *storage.SQLiteStore
	url      string
	agents   int
	tmpDir   string
}

// Start loads the documents and starts serving.
func Start(opts Options) (*Host, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	docs, errs := config.LoadAllDocuments(opts.AgentsDir)
	for _, err := range errs {
		logger.Warn("embedded mock: skipping document", "error", err)
	}
	registry := engine.NewAgentRegistry()
	validator := &config.Validator{}
	loaded := 0
	for _, r := range docs.Agents {
		if r == nil || r.Definition == nil {
			continue
		}
		// Same order as `mockagents start`: defaults first (this is also
		// where chaos presets such as rate-limited expand), then validation.
		config.ApplyDefaults(r.Definition)
		if verr := validator.Validate(r.Definition, r.FilePath, r.Node); verr != nil {
			logger.Warn("embedded mock: invalid agent", "file", r.FilePath, "error", verr.Error())
			continue
		}
		registry.RegisterWithSource(r.Definition, r.FilePath)
		loaded++
	}
	if loaded == 0 {
		return nil, fmt.Errorf("embedded mock: no valid agents in %s", opts.AgentsDir)
	}
	pipelines := engine.NewPipelineRegistry()
	for _, p := range docs.Pipelines {
		if p == nil || p.Definition == nil {
			continue
		}
		if verr := config.ValidatePipeline(p.Definition, p.FilePath, p.Node); verr != nil {
			logger.Warn("embedded mock: invalid pipeline", "file", p.FilePath, "error", verr.Error())
			continue
		}
		pipelines.RegisterWithSource(p.Definition, p.FilePath)
	}

	eng := engine.NewEngine(registry, state.NewMemoryStore(state.DefaultSessionTTL), logger)

	h := &Host{agents: loaded}
	dataDir := opts.DataDir
	if dataDir == "" {
		tmp, err := os.MkdirTemp("", "playground-mock-")
		if err != nil {
			return nil, err
		}
		dataDir, h.tmpDir = tmp, tmp
	}
	logStore, err := storage.NewSQLiteStore(filepath.Join(dataDir, "mock-interactions.db"))
	if err != nil {
		logger.Warn("embedded mock: interaction logging disabled", "error", err)
	} else {
		h.logStore = logStore
	}

	prices := pricing.NewDefaultTable()
	if opts.PricingFile != "" {
		if err := prices.LoadYAML(opts.PricingFile); err != nil {
			logger.Warn("embedded mock: pricing file not loaded", "error", err)
		}
	}

	addr := opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("embedded mock: bad addr %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("embedded mock: bad port %q", portStr)
	}

	cfg := server.DefaultConfig()
	cfg.Host = host
	cfg.Port = port
	cfg.AgentsDir = opts.AgentsDir
	cfg.Version = "embedded"
	cfg.Pipelines = pipelines
	cfg.Prices = prices
	if h.logStore != nil {
		cfg.LogStore = h.logStore
	}
	h.srv = server.New(eng, cfg, logger)
	if err := h.srv.Listen(); err != nil {
		h.cleanup()
		return nil, fmt.Errorf("embedded mock: %w", err)
	}
	h.url = "http://" + h.srv.ListenAddr()
	go func() {
		if err := h.srv.Serve(); err != nil {
			logger.Error("embedded mock stopped", "error", err)
		}
	}()
	return h, nil
}

// URL is the server's base URL.
func (h *Host) URL() string { return h.url }

// Agents is the number of agents loaded.
func (h *Host) Agents() int { return h.agents }

// Close shuts the server down and removes temp data.
func (h *Host) Close() error {
	var errs []error
	if h.srv != nil {
		errs = append(errs, h.srv.Shutdown())
	}
	if h.logStore != nil {
		errs = append(errs, h.logStore.Close())
	}
	h.cleanup()
	return errors.Join(errs...)
}

func (h *Host) cleanup() {
	if h.tmpDir != "" {
		_ = os.RemoveAll(h.tmpDir)
	}
}

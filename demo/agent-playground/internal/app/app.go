// Package app wires the playground together: configuration, the mock
// (embedded or external), provider clients, the agent runtime, the workflow
// engine with its watchdog and persistence, and the HTTP API.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	playground "github.com/mockagents/mockagents/demo/agent-playground"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/api"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/metrics"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/mockctl"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/mockhost"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/tools"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflows"
)

// Options configure the application.
type Options struct {
	// Addr is the playground listen address.
	Addr string
	// ConfigPath is a playground.json; "" uses the embedded default.
	ConfigPath string
	// MockURL is an external mockagents base URL; "" or "embedded" starts
	// one in-process.
	MockURL string
	// MockAPIKey is sent to the mock (provider calls and management API).
	MockAPIKey string
	// MockAddr is the embedded mock's listen address (default 127.0.0.1:0).
	MockAddr string
	// FixturesDir overrides the embedded mockagents fixtures.
	FixturesDir string
	// StatePath enables run/review persistence.
	StatePath string
	// ReviewMode overrides defaults.review.mode.
	ReviewMode string
	// Token protects mutating API calls.
	Token string
	// WatchdogInterval is how often stale runs are checked (default 1s).
	WatchdogInterval time.Duration
	Logger           *slog.Logger
}

// App is a wired playground.
type App struct {
	Options  Options
	Server   *api.Server
	Engine   *workflow.Engine
	Config   *config.Store
	Metrics  *metrics.Registry
	Handler  http.Handler
	MockURL  string
	MockMode string

	host    *mockhost.Host
	tmpDirs []string
}

// New builds the application. Call Close when done.
func New(opts Options) (*App, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	a := &App{Options: opts}
	ok := false
	defer func() {
		if !ok {
			a.Close()
		}
	}()

	reg := tools.NewRegistry()
	var cfg *config.Config
	var err error
	if opts.ConfigPath != "" {
		cfg, err = config.Load(opts.ConfigPath, reg.Names())
	} else {
		raw, rerr := playground.FS.ReadFile("config/playground.json")
		if rerr != nil {
			return nil, rerr
		}
		cfg, err = config.Parse(raw, reg.Names())
	}
	if err != nil {
		return nil, err
	}
	if opts.ReviewMode != "" {
		cfg.Defaults.Review.Mode = opts.ReviewMode
		if err := cfg.Validate(reg.Names()); err != nil {
			return nil, err
		}
	}
	a.Config = config.NewStore(cfg, reg.Names())

	// The mock: embedded (default) or external.
	if opts.MockURL == "" || opts.MockURL == "embedded" {
		dir := opts.FixturesDir
		pricing := ""
		if dir == "" {
			dir, pricing, err = a.extractFixtures()
			if err != nil {
				return nil, err
			}
		}
		a.host, err = mockhost.Start(mockhost.Options{AgentsDir: dir, PricingFile: pricing, Addr: opts.MockAddr, Logger: logger.With("component", "mock")})
		if err != nil {
			return nil, err
		}
		a.MockURL, a.MockMode = a.host.URL(), "embedded"
		logger.Info("embedded mockagents started", "url", a.MockURL, "agents", a.host.Agents())
	} else {
		a.MockURL, a.MockMode = strings.TrimRight(opts.MockURL, "/"), "external"
	}

	a.Metrics = metrics.New()
	rt := &agents.Runtime{
		Config:    a.Config,
		Providers: agents.NewProviders(a.MockURL, opts.MockAPIKey),
		Tools:     reg,
		Metrics:   a.Metrics,
		Logger:    logger,
	}
	mock := mockctl.New(a.MockURL, opts.MockAPIKey)
	a.Engine = workflow.NewEngine(rt, a.Config, a.Metrics, logger)
	a.Engine.Register(workflows.All()...)
	a.Engine.Arm = mock.Arm
	a.Engine.StatePath = opts.StatePath
	if n, err := a.Engine.LoadState(); err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	} else if n > 0 {
		logger.Warn("runs interrupted by the previous shutdown were marked failed", "count", n)
	}

	a.Server = &api.Server{
		Engine: a.Engine, Config: a.Config, Tools: reg, Metrics: a.Metrics,
		Mock: mock, MockMode: a.MockMode, Logger: logger, Token: opts.Token,
	}
	a.Handler, err = a.Server.Handler()
	if err != nil {
		return nil, err
	}
	ok = true
	return a, nil
}

// extractFixtures writes the embedded mockagents fixtures and pricing file
// to a temp dir. mockagents reloads agents from their source files, so
// they must exist on disk.
func (a *App) extractFixtures() (dir, pricing string, err error) {
	tmp, err := os.MkdirTemp("", "playground-fixtures-")
	if err != nil {
		return "", "", err
	}
	a.tmpDirs = append(a.tmpDirs, tmp)
	agentsDir := filepath.Join(tmp, "mockagents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return "", "", err
	}
	entries, err := fs.ReadDir(playground.FS, "mockagents")
	if err != nil {
		return "", "", err
	}
	for _, e := range entries {
		raw, err := playground.FS.ReadFile("mockagents/" + e.Name())
		if err != nil {
			return "", "", err
		}
		if err := os.WriteFile(filepath.Join(agentsDir, e.Name()), raw, 0o644); err != nil {
			return "", "", err
		}
	}
	raw, err := playground.FS.ReadFile("config/mock-pricing.yaml")
	if err != nil {
		return "", "", err
	}
	pricing = filepath.Join(tmp, "mock-pricing.yaml")
	return agentsDir, pricing, os.WriteFile(pricing, raw, 0o644)
}

// Run serves until ctx is cancelled, then shuts down gracefully: the HTTP
// server drains, in_progress runs are failed with code "shutdown", and
// state is flushed.
func (a *App) Run(ctx context.Context, ln net.Listener) error {
	bg, stop := context.WithCancel(context.Background())
	defer stop()
	interval := a.Options.WatchdogInterval
	if interval <= 0 {
		interval = time.Second
	}
	go a.Engine.RunWatchdog(bg, interval)
	go a.Engine.RunPersister(bg, 2*time.Second)

	srv := &http.Server{Handler: a.Handler, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.Engine.Shutdown(shutdownCtx)
	return srv.Shutdown(shutdownCtx)
}

// Close releases the embedded mock and temp files.
func (a *App) Close() {
	if a.host != nil {
		_ = a.host.Close()
		a.host = nil
	}
	for _, d := range a.tmpDirs {
		_ = os.RemoveAll(d)
	}
	a.tmpDirs = nil
}

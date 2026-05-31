package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"context"
	"errors"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/server"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the mock agent server",
	Long: `Start the MockAgents HTTP server, loading all agent definitions from
the agents directory. The server serves multiple agents simultaneously
and supports hot-reload via the management API.`,
	RunE: runStart,
}

var (
	host     string
	port     int
	jsonLogs bool
	watchDir bool
)

func init() {
	defaultHost := server.DefaultHost
	if envHost := strings.TrimSpace(os.Getenv("MOCKAGENTS_HOST")); envHost != "" {
		defaultHost = envHost
	}
	defaultPort := server.DefaultPort
	if envPort := os.Getenv("MOCKAGENTS_PORT"); envPort != "" {
		if p, err := fmt.Sscanf(envPort, "%d", &defaultPort); p != 1 || err != nil {
			defaultPort = server.DefaultPort
		}
	}
	startCmd.Flags().StringVar(&host, "host", defaultHost, "HTTP server bind address")
	startCmd.Flags().IntVarP(&port, "port", "p", defaultPort, "HTTP server port")
	startCmd.Flags().BoolVar(&jsonLogs, "json-logs", false, "Output logs in JSON format")
	startCmd.Flags().BoolVarP(&watchDir, "watch", "w", false, "Auto-reload agent YAML files on change (fsnotify)")
}

func runStart(cmd *cobra.Command, args []string) error {
	// Configure structured logger.
	logLevel := parseLogLevel(cmd)
	logger := newLogger(logLevel, jsonLogs)

	// Resolve agents directory.
	agentsDir, _ := cmd.Flags().GetString("agents-dir")
	info, err := os.Stat(agentsDir)
	if err != nil {
		return fmt.Errorf("agents directory %q: %w", agentsDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", agentsDir)
	}

	// Load every document kind in the directory. Agents are required
	// (the server won't start without at least one), but pipelines,
	// test suites, and MCP server stubs are optional extras that add
	// surface area to the GUI / management API when present.
	docs, loadErrs := config.LoadAllDocuments(agentsDir)
	for _, e := range loadErrs {
		logger.Warn("failed to load document", "error", e)
	}
	results := docs.Agents
	if len(results) == 0 {
		return fmt.Errorf("no valid agent definitions found in %q", agentsDir)
	}

	// Build registry.
	registry := engine.NewAgentRegistry()
	validator := &config.Validator{}
	var validCount int

	for _, result := range results {
		config.ApplyDefaults(result.Definition)
		if errList := validator.Validate(result.Definition, result.FilePath, result.Node); errList != nil {
			logger.Warn("skipping invalid agent",
				"file", result.FilePath,
				"errors", errList.Error(),
			)
			continue
		}
		registry.Register(result.Definition)
		validCount++
		logger.Info("loaded agent",
			"name", result.Definition.Metadata.Name,
			"model", result.Definition.Spec.Model,
			"protocol", result.Definition.Spec.Protocol,
			"scenarios", len(result.Definition.Spec.Behavior.Scenarios),
		)
	}

	if validCount == 0 {
		return fmt.Errorf("all agent definitions in %q failed validation", agentsDir)
	}

	// Load pipeline definitions alongside the agent registry so the
	// GUI's /pipelines surface and the management API have something
	// to list.
	pipelineReg := registerPipelines(docs.Pipelines, logger)

	// Initialize engine.
	store := state.NewMemoryStore(state.DefaultSessionTTL)
	stopCleanup := store.StartCleanupTicker(5 * time.Minute)
	defer stopCleanup()

	eng := engine.NewEngine(registry, store, logger)

	// Initialize interaction log storage.
	logStore, err := storage.NewSQLiteStore(".mockagents.db")
	if err != nil {
		logger.Warn("interaction logging disabled", "error", err)
	} else {
		defer logStore.Close()
		logger.Info("interaction logging enabled", "db", ".mockagents.db")
	}

	// Configure and start server.
	cfg := server.DefaultConfig()
	cfg.Host = host
	cfg.Port = port
	cfg.AgentsDir = agentsDir
	cfg.Version = version
	cfg.LogStore = logStore
	cfg.Pipelines = pipelineReg

	// Audit log: always enabled. Costs a few KB of SQLite and a
	// handful of writes per control-plane mutation; the value is
	// high and the overhead is invisible.
	auditStore, auditErr := audit.NewSQLiteStore(".mockagents-audit.db")
	if auditErr != nil {
		logger.Warn("audit logging disabled", "error", auditErr)
	} else {
		defer auditStore.Close()
		cfg.AuditStore = auditStore
		logger.Info("audit logging enabled", "db", ".mockagents-audit.db")
	}

	// Cost estimation: always on with the built-in default price
	// table; overridden at runtime via MOCKAGENTS_PRICING pointing at
	// a YAML file of per-model overrides. Load failures are
	// non-fatal — we fall through to the defaults.
	prices, perr := pricing.FromEnv()
	if perr != nil {
		logger.Warn("custom pricing disabled, using defaults", "error", perr)
	}
	cfg.Prices = prices

	// Optional multi-tenant mode (experimental). Enabled by setting
	// MOCKAGENTS_MULTI_TENANT=1. On first boot we seed a "default"
	// tenant and an admin API key; the plaintext is printed to stderr
	// exactly once so the operator can capture it.
	if os.Getenv("MOCKAGENTS_MULTI_TENANT") == "1" {
		tenancyStore, err := tenancy.NewSQLiteStore(".mockagents-tenancy.db")
		if err != nil {
			return fmt.Errorf("multi-tenant mode: %w", err)
		}
		defer tenancyStore.Close()
		// Enable the auth cache so bcrypt runs at most once per
		// plaintext-key per TTL window. Mutations (delete, role
		// change) flush the cache so cached principals can never
		// outlive their backing row.
		tenancyStore.EnableAuthCache(5*time.Minute, 1024)
		cfg.TenancyStore = tenancyStore
		if err := bootstrapTenancy(cmd.Context(), tenancyStore, logger); err != nil {
			return fmt.Errorf("bootstrap tenancy: %w", err)
		}
	}

	srv := server.New(eng, cfg, logger)

	// Optional fsnotify auto-reload (US-2.3). When --watch is set we
	// observe agentsDir for YAML changes and push new definitions
	// into the registry in-place. Validation failures are logged and
	// the previous definition is kept.
	var watcher *server.AgentDirWatcher
	if watchDir {
		watcher = server.NewAgentDirWatcher(agentsDir, eng, logger)
		if err := watcher.Start(); err != nil {
			logger.Warn("agent watcher disabled", "error", err)
			watcher = nil
		}
	}
	defer func() {
		if watcher != nil {
			watcher.Stop()
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", "signal", sig)
		if err := srv.Shutdown(); err != nil {
			logger.Error("shutdown error", "error", err)
			return err
		}
		logger.Info("server stopped gracefully")
		return nil
	case err := <-errCh:
		return err
	}
}

// registerPipelines validates each loaded pipeline definition and
// registers the valid ones. Validation runs the same cycle +
// reachability checks `mockagents validate` performs (config.
// ValidatePipeline), so a malformed or cyclic pipeline is logged and
// skipped here at server start rather than silently registered and
// failing later in the executor — mirroring the agent path above.
// Failures are non-fatal: the server can still serve agent traffic
// even if every pipeline YAML is malformed.
//
// Note: pipelines are not live-reloadable (the watcher only reacts to
// Agent-kind documents), so this runs once at boot.
func registerPipelines(pipelines []*config.PipelineLoadResult, logger *slog.Logger) *engine.PipelineRegistry {
	reg := engine.NewPipelineRegistry()
	for _, pr := range pipelines {
		if pr == nil || pr.Definition == nil {
			continue
		}
		if errList := config.ValidatePipeline(pr.Definition, pr.FilePath, pr.Node); errList != nil {
			logger.Warn("skipping invalid pipeline",
				"file", pr.FilePath,
				"errors", errList.Error(),
			)
			continue
		}
		reg.Register(pr.Definition)
		logger.Info("loaded pipeline",
			"name", pr.Definition.Metadata.Name,
			"topology", pr.Definition.Spec.Topology,
			"agents", len(pr.Definition.Spec.Agents),
		)
	}
	return reg
}

// bootstrapTenancy creates a "default" tenant and an admin API key if
// none exist yet. The plaintext key is printed to stderr exactly once —
// after this run it is bcrypt-hashed and unrecoverable. Callers can
// preset a specific plaintext via MOCKAGENTS_BOOTSTRAP_KEY (useful in
// Helm deployments where the key is piped in from a Secret).
func bootstrapTenancy(ctx context.Context, store *tenancy.SQLiteStore, logger *slog.Logger) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tenant, err := store.GetTenantByName(ctx, "default")
	if err != nil && !errors.Is(err, tenancy.ErrNotFound) {
		return err
	}
	if tenant == nil {
		tenant, err = store.CreateTenant(ctx, "default")
		if err != nil {
			return err
		}
		logger.Info("tenancy: created default tenant", "id", tenant.ID)
	}
	existing, err := store.ListAPIKeys(ctx, tenant.ID)
	if err != nil {
		return err
	}
	for _, k := range existing {
		if k.Role == tenancy.RoleAdmin {
			logger.Info("tenancy: admin key already exists", "key_id", k.ID, "prefix", k.Prefix)
			return nil
		}
	}
	result, err := store.CreateAPIKey(ctx, tenant.ID, "bootstrap-admin", tenancy.RoleAdmin)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "================================================================")
	fmt.Fprintln(os.Stderr, "MockAgents multi-tenant mode enabled.")
	fmt.Fprintf(os.Stderr, "Bootstrap admin key (shown once): %s\n", result.Plaintext)
	fmt.Fprintln(os.Stderr, "Store this in your password manager. Use it via:")
	fmt.Fprintln(os.Stderr, "  Authorization: Bearer <key>   or   X-Api-Key: <key>")
	fmt.Fprintln(os.Stderr, "================================================================")
	return nil
}

func parseLogLevel(cmd *cobra.Command) slog.Level {
	level, _ := cmd.Flags().GetString("log-level")
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func newLogger(level slog.Level, jsonOutput bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if jsonOutput {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}

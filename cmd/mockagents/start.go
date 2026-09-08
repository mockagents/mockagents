package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"context"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/metrics"
	"github.com/mockagents/mockagents/internal/oidcauth"
	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/server"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/mockagents/mockagents/internal/vector"
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
	host      string
	port      int
	jsonLogs  bool
	watchDir  bool
	chaosRate string
	chaosSeed string
	chaosOff  bool
)

func init() {
	defaultHost := server.DefaultHost
	if envHost := strings.TrimSpace(os.Getenv("MOCKAGENTS_HOST")); envHost != "" {
		defaultHost = envHost
	}
	defaultPort := server.DefaultPort
	// A typo here used to fall back to 8080 silently, so the pod listened on
	// the wrong port with a healthy-looking log. Recorded now, reported by
	// runStart (init cannot return an error).
	if p, ok, err := envInt("MOCKAGENTS_PORT", 1, 65535); err != nil {
		recordStartupEnvError(err)
	} else if ok {
		defaultPort = p
	}
	chaosOffDefault, err := envBool("MOCKAGENTS_CHAOS_OFF")
	recordStartupEnvError(err)
	startCmd.Flags().StringVar(&host, "host", defaultHost, "HTTP server bind address")
	startCmd.Flags().IntVarP(&port, "port", "p", defaultPort, "HTTP server port")
	startCmd.Flags().BoolVar(&jsonLogs, "json-logs", false, "Output logs in JSON format")
	startCmd.Flags().BoolVarP(&watchDir, "watch", "w", false, "Auto-reload agent YAML files on change (fsnotify)")
	startCmd.Flags().StringVar(&chaosRate, "chaos-rate", strings.TrimSpace(os.Getenv("MOCKAGENTS_CHAOS_RATE")), "Lowest-precedence server-wide chaos rate (0.0-1.0; env MOCKAGENTS_CHAOS_RATE)")
	startCmd.Flags().StringVar(&chaosSeed, "chaos-seed", strings.TrimSpace(os.Getenv("MOCKAGENTS_CHAOS_SEED")), "Server-wide deterministic chaos seed (env MOCKAGENTS_CHAOS_SEED)")
	startCmd.Flags().BoolVar(&chaosOff, "chaos-off", chaosOffDefault, "Set the inherited server-wide chaos rate to zero (env MOCKAGENTS_CHAOS_OFF)")
}

func parseGlobalChaos(seedText, rateText string, off bool) (int64, *float64, error) {
	var seed int64
	var err error
	if seedText = strings.TrimSpace(seedText); seedText != "" {
		seed, err = strconv.ParseInt(seedText, 10, 64)
		if err != nil {
			return 0, nil, fmt.Errorf("invalid --chaos-seed %q: must be an integer", seedText)
		}
	}
	if off {
		zero := 0.0
		return seed, &zero, nil
	}
	if rateText = strings.TrimSpace(rateText); rateText == "" {
		return seed, nil, nil
	}
	rate, err := strconv.ParseFloat(rateText, 64)
	if err != nil || rate < 0 || rate > 1 {
		return 0, nil, fmt.Errorf("invalid --chaos-rate %q: must be between 0 and 1", rateText)
	}
	return seed, &rate, nil
}

func runStart(cmd *cobra.Command, args []string) error {
	if err := joinStartupEnvErrors(); err != nil {
		return err
	}
	globalSeed, globalRate, err := parseGlobalChaos(chaosSeed, chaosRate, chaosOff)
	if err != nil {
		return err
	}
	// Configure structured logger.
	logLevel, err := parseLogLevel(cmd)
	if err != nil {
		return err
	}
	logger := newLogger(logLevel, jsonLogs)
	// Quota defaults are read up front so a typo fails the start, not the
	// enforcement (an unparsable limit used to mean "unlimited").
	quotaDefaults, err := quotaDefaultsFromEnv()
	if err != nil {
		return err
	}

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
		registry.RegisterWithSource(result.Definition, result.FilePath)
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
	vectorStore, vectorCount := registerVectorCollections(docs.Vectors, logger)
	vectorStore.SetGlobalChaosPolicy(globalSeed, globalRate)
	if vectorCount > 0 {
		logger.Info("loaded vector collections", "collections", vectorCount)
	}
	searchService, serviceFaults := registerSearchServices(docs.SearchServices, logger)
	for provider, faults := range serviceFaults {
		faults.GlobalSeed = globalSeed
		faults.GlobalRate = globalRate
		serviceFaults[provider] = faults
	}

	// Initialize engine.
	store := state.NewMemoryStore(state.DefaultSessionTTL)
	stopCleanup := store.StartCleanupTicker(5 * time.Minute)
	defer stopCleanup()

	eng := engine.NewEngine(registry, store, logger)
	eng.Chaos.SetGlobalPolicy(globalSeed, globalRate)

	// Initialize interaction log storage.
	logDB := dataPath(".mockagents.db")
	logStore, err := storage.NewSQLiteStore(logDB)
	if err != nil {
		logger.Warn("interaction logging disabled",
			"db", logDB, "error", err,
			"hint", "SQLite could not open the file — usually the directory is not writable (SQLITE_CANTOPEN is misreported as 'out of memory (14)'); set MOCKAGENTS_DATA_DIR to a writable directory")
	} else {
		defer logStore.Close()
		logger.Info("interaction logging enabled", "db", logDB)
	}

	// Configure and start server.
	cfg := server.DefaultConfig()
	cfg.Host = host
	cfg.Port = port
	cfg.AgentsDir = agentsDir
	cfg.Version = version
	cfg.LogStore = logStore
	cfg.Pipelines = pipelineReg
	cfg.VectorStore = vectorStore
	cfg.SearchService = searchService
	cfg.ServiceFaults = serviceFaults

	// Stamp the real build version onto mockagents_build_info. The default
	// registry is created at package init, before the ldflags-set version is
	// readable, so it starts as "dev" until this call (FR-J02).
	metrics.SetVersion(version)

	// Interaction-log privacy + retention controls (SEC-05):
	//   MOCKAGENTS_LOG_BODIES   = full | sanitized | none  (default full)
	//   MOCKAGENTS_LOG_MAX_ROWS = <n>                       (default 0 = unlimited)
	cfg.LogBodyMode = server.NormalizeLogBodyMode(os.Getenv("MOCKAGENTS_LOG_BODIES"))
	if n, ok, err := envInt("MOCKAGENTS_LOG_MAX_ROWS", 0, 0); err != nil {
		return err
	} else if ok {
		cfg.LogMaxRows = n // 0 = unlimited
	}
	if cfg.LogBodyMode != server.LogBodyFull {
		logger.Info("interaction-log body capture mode", "mode", string(cfg.LogBodyMode))
	}
	if cfg.LogMaxRows > 0 {
		logger.Info("interaction-log retention enabled", "max_rows", cfg.LogMaxRows)
	}

	// Audit log: always enabled. Costs a few KB of SQLite and a
	// handful of writes per control-plane mutation; the value is
	// high and the overhead is invisible.
	auditDB := dataPath(".mockagents-audit.db")
	auditStore, auditErr := audit.NewSQLiteStore(auditDB)
	if auditErr != nil {
		logger.Warn("audit logging disabled",
			"db", auditDB, "error", auditErr,
			"hint", "SQLite could not open the file — usually the directory is not writable; set MOCKAGENTS_DATA_DIR to a writable directory")
	} else {
		defer auditStore.Close()
		cfg.AuditStore = auditStore
		logger.Info("audit logging enabled", "db", auditDB)
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
	// MOCKAGENTS_MULTI_TENANT=1 (or true/yes/on — an unrecognised value is a
	// startup error, because "true" used to be read as OFF and ran the
	// control plane unauthenticated). On first boot we seed a "default"
	// tenant and a platform API key; see bootstrapTenancy for how the
	// plaintext is handed to the operator.
	multiTenant, err := envBool("MOCKAGENTS_MULTI_TENANT")
	if err != nil {
		return err
	}
	if multiTenant {
		// Backend selection (REF-08 slice B): MOCKAGENTS_TENANCY_DSN opts into the
		// pluggable Postgres store; unset keeps the zero-dependency SQLite default.
		var tenancyStore tenancy.Store
		if dsn := os.Getenv("MOCKAGENTS_TENANCY_DSN"); dsn != "" {
			ps, err := tenancy.NewPostgresStore(dsn)
			if err != nil {
				return fmt.Errorf("multi-tenant mode (postgres): %w", err)
			}
			ps.EnableAuthCache(5*time.Minute, 1024)
			tenancyStore = ps
			logger.Info("tenancy: using Postgres store")
		} else {
			ss, err := tenancy.NewSQLiteStore(dataPath(".mockagents-tenancy.db"))
			if err != nil {
				return fmt.Errorf("multi-tenant mode: %w", err)
			}
			ss.EnableAuthCache(5*time.Minute, 1024)
			tenancyStore = ss
		}
		// Enable the auth cache so bcrypt runs at most once per plaintext-key per
		// TTL window. Mutations flush the cache so cached principals can never
		// outlive their backing row. (Done per-store above before the interface
		// assignment, since EnableAuthCache is a concrete method.)
		defer tenancyStore.Close()
		cfg.TenancyStore = tenancyStore
		keyFile, _ := envString("MOCKAGENTS_BOOTSTRAP_KEY_FILE")
		if keyFile == "" {
			keyFile = dataPath("bootstrap-admin.key")
		}
		preset, _ := envString("MOCKAGENTS_BOOTSTRAP_KEY")
		if err := bootstrapTenancy(cmd.Context(), tenancyStore, logger, preset, keyFile); err != nil {
			return fmt.Errorf("bootstrap tenancy: %w", err)
		}

		// Per-tenant quotas (REF-08 slice C). Defaults come from env; per-tenant
		// overrides are set at runtime via PUT /api/v1/tenants/{id}/quota. The
		// enforcer is always created in multi-tenant mode so the /api/v1/quota
		// endpoints exist; with zero defaults it simply enforces nothing.
		cfg.QuotaEnforcer = quota.NewEnforcer(quotaDefaults)
		if quotaDefaults.RatePerSec > 0 || quotaDefaults.MonthlySpendUSD > 0 {
			logger.Info("tenancy: per-tenant quota defaults",
				"rate_per_sec", quotaDefaults.RatePerSec,
				"rate_burst", quotaDefaults.RateBurst,
				"monthly_spend_usd", quotaDefaults.MonthlySpendUSD)
		}
		// Seed the enforcer with any persisted per-tenant overrides so they
		// survive restarts. Non-fatal: a load failure logs and falls back to
		// env defaults rather than blocking startup.
		if n, err := loadQuotaOverrides(cmd.Context(), tenancyStore, cfg.QuotaEnforcer); err != nil {
			logger.Warn("could not load persisted quota overrides", "error", err)
		} else if n > 0 {
			logger.Info("tenancy: loaded persisted quota overrides", "count", n)
		}
		// Back spend accounting with the tenancy store's shared ledger so the
		// monthly-spend cap is accurate across replicas (atomic increments) and
		// survives restarts, rather than per-process in-memory counters.
		cfg.QuotaEnforcer.SetSpendBackend(tenancyStore)

		// SSO / OIDC login (REF-08 slice D). Enabled only when the OIDC env vars
		// are set; a misconfiguration fails startup rather than silently
		// disabling SSO.
		sso, err := buildSSO(cmd.Context(), tenancyStore, logger)
		if err != nil {
			return fmt.Errorf("oidc/sso: %w", err)
		}
		cfg.SSO = sso
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

	printStartBanner(os.Stderr, host, port)

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

func registerSearchServices(results []*config.SearchServiceLoadResult, logger *slog.Logger) (*types.SearchServiceDefinition, map[string]types.SearchFaults) {
	var search *types.SearchServiceDefinition
	faults := make(map[string]types.SearchFaults)
	for _, result := range results {
		if result == nil || result.Definition == nil {
			continue
		}
		if errs := config.ValidateSearchService(result.Definition, result.FilePath, result.Node); errs != nil {
			logger.Warn("skipping invalid search service", "file", result.FilePath, "errors", errs.Error())
			continue
		}
		provider := strings.ToLower(result.Definition.Spec.Provider)
		faults[provider] = result.Definition.Spec.Faults
		if provider == "tavily" {
			search = result.Definition
		}
	}
	return search, faults
}

// registerVectorCollections validates and seeds declarative VectorMock
// fixtures. Invalid collections are skipped consistently with invalid agents.
func registerVectorCollections(results []*config.VectorCollectionLoadResult, logger *slog.Logger) (*vector.Store, int) {
	store := &vector.Store{}
	loaded := 0
	for _, result := range results {
		if result == nil || result.Definition == nil {
			continue
		}
		def := result.Definition
		if errs := config.ValidateVectorCollection(def, result.FilePath, result.Node); errs != nil {
			logger.Warn("skipping invalid vector collection", "file", result.FilePath, "errors", errs.Error())
			continue
		}
		name := vector.ScopedCollectionName(def.Metadata.TenantID, def.Metadata.Name)
		if err := store.CreateCollection(name, def.Spec.Dimension, vector.Metric(strings.ToLower(def.Spec.Metric))); err != nil {
			logger.Warn("skipping vector collection", "file", result.FilePath, "error", err)
			continue
		}
		points := make([]vector.Point, 0, len(def.Spec.Points))
		for _, fixture := range def.Spec.Points {
			id, _ := config.VectorPointKey(fixture.ID)
			points = append(points, vector.Point{ID: id, ExternalID: fixture.ID, Vector: fixture.Vector, Metadata: fixture.Metadata})
		}
		if err := store.Upsert(name, points); err != nil {
			logger.Warn("skipping vector collection points", "file", result.FilePath, "error", err)
			_ = store.DeleteCollection(name)
			continue
		}
		if partial := def.Spec.Faults.PartialResults; partial != nil {
			if err := store.SetPartialResultScopedPolicy(name, &partial.MaxResults, def.Spec.Faults.Seed, def.Spec.Faults.Rate, def.Spec.Faults.OperationRates); err != nil {
				logger.Warn("skipping vector collection fault", "file", result.FilePath, "error", err)
				_ = store.DeleteCollection(name)
				continue
			}
		}
		loaded++
	}
	return store, loaded
}

// dataPath resolves where MockAgents keeps its on-disk state (interaction
// logs, audit trail, tenancy DB). MOCKAGENTS_DATA_DIR relocates all of it —
// the escape hatch when the working directory is not writable (read-only
// containers, `docker run` without a workdir). Unset, it preserves the
// original behavior: the file lands in the current working directory.
func dataPath(filename string) string {
	dir := strings.TrimSpace(os.Getenv("MOCKAGENTS_DATA_DIR"))
	if dir == "" {
		return filename
	}
	// Best-effort create; if this fails the SQLite open below reports the
	// path-specific error with the actionable hint attached.
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, filename)
}

// printStartBanner shows the base-URL-swap "hero" so a developer's first move
// is obvious: point your existing SDK at MockAgents, change nothing else.
func printStartBanner(w io.Writer, host string, port int) {
	disp := host
	if disp == "" || disp == "0.0.0.0" || disp == "::" {
		disp = "localhost"
	}
	// net.JoinHostPort brackets IPv6 literals (e.g. [::1]:8080) so the printed
	// URLs are valid for any bind address.
	base := "http://" + net.JoinHostPort(disp, strconv.Itoa(port))

	const line = "────────────────────────────────────────────────────────────"
	fmt.Fprintln(w, line)
	fmt.Fprintf(w, "MockAgents is mocking OpenAI + Anthropic + Gemini at %s\n", base)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Point your app at it — no code changes, just the base URL:")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  export OPENAI_BASE_URL=%s/v1\n", base)
	fmt.Fprintln(w, "  export OPENAI_API_KEY=mock")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  curl %s/v1/chat/completions \\\n", base)
	fmt.Fprintln(w, "    -H 'content-type: application/json' \\")
	fmt.Fprintln(w, `    -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}'`)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Anthropic   ANTHROPIC_BASE_URL=%s\n", base)
	fmt.Fprintf(w, "  Gemini      %s/v1beta/models/<model>:generateContent\n", base)
	fmt.Fprintf(w, "  Health      %s/api/v1/health\n", base)
	fmt.Fprintln(w, line)
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
// Note: the file watcher only reacts to Agent-kind documents, so this
// boot-time pass is the only file-driven pipeline load. Pipelines can still
// be updated at runtime through PUT /api/v1/pipelines/{name} (the GUI editor,
// REF-07), which re-registers in place.
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
		reg.RegisterWithSource(pr.Definition, pr.FilePath)
		logger.Info("loaded pipeline",
			"name", pr.Definition.Metadata.Name,
			"topology", pr.Definition.Spec.Topology,
			"agents", len(pr.Definition.Spec.Agents),
		)
	}
	return reg
}

// bootstrapTenancy creates a "default" tenant and a platform API key if none
// exist yet. The plaintext is never written to the log stream (audit H-09:
// stderr is the pod log, shipped to every aggregator). Two ways to receive it:
//
//   - preset (MOCKAGENTS_BOOTSTRAP_KEY): the operator supplies the plaintext,
//     e.g. from a Kubernetes Secret; it is hashed and registered as the
//     platform key. Nothing secret is printed.
//   - otherwise a key is generated and written, mode 0600, to keyFile
//     (MOCKAGENTS_BOOTSTRAP_KEY_FILE, default <data dir>/bootstrap-admin.key);
//     stderr shows only the path and the public prefix. If the file cannot be
//     written the generated key is discarded again and startup fails with
//     instructions, rather than falling back to printing the secret.
//
// After bootstrap the key is bcrypt-hashed and unrecoverable.
func bootstrapTenancy(ctx context.Context, store tenancy.Store, logger *slog.Logger, preset, keyFile string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Find the "default" tenant via the Store interface. GetTenantByName is a
	// SQLite-only helper, so we scan ListTenants instead — the tenant set is
	// tiny at bootstrap, so the linear scan is free and keeps bootstrap
	// backend-agnostic (works against Postgres too).
	tenants, err := store.ListTenants(ctx)
	if err != nil {
		return err
	}
	var tenant *tenancy.Tenant
	for _, t := range tenants {
		if t.Name == "default" {
			tenant = t
			break
		}
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
		if k.Role == tenancy.RolePlatform {
			logger.Info("tenancy: platform key already exists", "key_id", k.ID, "prefix", k.Prefix)
			if preset != "" {
				if p, err := tenancy.ValidatePresetAPIKey(preset); err == nil && p != k.Prefix {
					logger.Warn("tenancy: MOCKAGENTS_BOOTSTRAP_KEY does not match the existing platform key and was ignored",
						"existing_prefix", k.Prefix, "preset_prefix", p)
				}
			}
			return nil
		}
	}
	// The bootstrap key is the platform operator: it can manage the tenant
	// collection. The management API cannot mint this role (X-TN-001), so the
	// bootstrap path is the only source of a platform credential.
	if preset != "" {
		prefix, err := tenancy.ValidatePresetAPIKey(preset)
		if err != nil {
			return fmt.Errorf("MOCKAGENTS_BOOTSTRAP_KEY: %w", err)
		}
		pc, ok := store.(tenancy.PresetKeyCreator)
		if !ok {
			return errors.New("MOCKAGENTS_BOOTSTRAP_KEY: this tenancy store cannot register a preset key")
		}
		key, err := pc.CreateAPIKeyWithPlaintext(ctx, tenant.ID, "bootstrap-admin", tenancy.RolePlatform, preset)
		if err != nil {
			return err
		}
		logger.Info("tenancy: platform key registered from MOCKAGENTS_BOOTSTRAP_KEY", "key_id", key.ID, "prefix", prefix)
		return nil
	}
	result, err := store.CreateAPIKey(ctx, tenant.ID, "bootstrap-admin", tenancy.RolePlatform)
	if err != nil {
		return err
	}
	if err := writeSecretFile(keyFile, result.Plaintext); err != nil {
		// Discard the key we cannot hand over; otherwise a platform credential
		// nobody knows would exist and block every future bootstrap.
		_ = store.DeleteAPIKey(ctx, tenant.ID, result.Key.ID)
		return fmt.Errorf("could not write the platform key to %s: %w — set MOCKAGENTS_BOOTSTRAP_KEY to supply the key from a secret, or point MOCKAGENTS_BOOTSTRAP_KEY_FILE / MOCKAGENTS_DATA_DIR at a writable location", keyFile, err)
	}
	fmt.Fprintln(os.Stderr, "================================================================")
	fmt.Fprintln(os.Stderr, "MockAgents multi-tenant mode enabled.")
	fmt.Fprintf(os.Stderr, "Bootstrap platform key (prefix %s) written to:\n  %s\n", result.Key.Prefix, keyFile)
	fmt.Fprintln(os.Stderr, "Read it once, store it in your password manager, then delete the file.")
	fmt.Fprintln(os.Stderr, "Use it via:  Authorization: Bearer <key>   or   X-Api-Key: <key>")
	fmt.Fprintln(os.Stderr, "================================================================")
	return nil
}

// writeSecretFile writes value to path, creating or truncating it with mode
// 0600 (owner read/write only; advisory on Windows).
func writeSecretFile(path, value string) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(value + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// quotaDefaultsFromEnv reads the default per-tenant quota from the environment.
// Unset means 0 (= unlimited for that dimension); a value that is set but not
// a valid non-negative number is an error, so a typo cannot silently disable
// enforcement (audit M-35).
func quotaDefaultsFromEnv() (quota.Config, error) {
	var cfg quota.Config
	var err error
	if cfg.RatePerSec, _, err = envFloat("MOCKAGENTS_DEFAULT_RATE_PER_SEC", 0); err != nil {
		return quota.Config{}, err
	}
	if cfg.RateBurst, _, err = envInt("MOCKAGENTS_DEFAULT_RATE_BURST", 0, 0); err != nil {
		return quota.Config{}, err
	}
	if cfg.MonthlySpendUSD, _, err = envFloat("MOCKAGENTS_DEFAULT_MONTHLY_SPEND_USD", 0); err != nil {
		return quota.Config{}, err
	}
	return cfg, nil
}

// buildSSO constructs the OIDC SSO handlers from the environment, or returns
// (nil, nil) when SSO is not configured. A configured-but-broken setup (missing
// domain map, bad role, unreachable issuer) returns an error so startup fails
// loudly rather than silently disabling login.
func buildSSO(ctx context.Context, store tenancy.Store, logger *slog.Logger) (*server.SSOHandlers, error) {
	issuer := os.Getenv("MOCKAGENTS_OIDC_ISSUER")
	clientID := os.Getenv("MOCKAGENTS_OIDC_CLIENT_ID")
	clientSecret := os.Getenv("MOCKAGENTS_OIDC_CLIENT_SECRET")
	redirect := os.Getenv("MOCKAGENTS_OIDC_REDIRECT_URL")
	if issuer == "" || clientID == "" || clientSecret == "" || redirect == "" {
		return nil, nil // SSO not configured
	}

	domainMap := parseDomainMap(os.Getenv("MOCKAGENTS_OIDC_DOMAIN_MAP"))
	if len(domainMap) == 0 {
		return nil, fmt.Errorf(`MOCKAGENTS_OIDC_DOMAIN_MAP is required when OIDC is configured (e.g. "acme.com=ten_acme")`)
	}
	role := tenancy.Role(os.Getenv("MOCKAGENTS_OIDC_DEFAULT_ROLE"))
	if role == "" {
		role = tenancy.RoleViewer
	}
	// SSO users are provisioned at viewer/editor/admin only — never the
	// bootstrap-only platform role.
	if !role.IsAssignableViaAPI() {
		return nil, fmt.Errorf("MOCKAGENTS_OIDC_DEFAULT_ROLE %q is invalid (use viewer/editor/admin)", role)
	}
	ttl := 24 * time.Hour
	// "24" (no unit) used to be silently ignored and keep the 24h default.
	if d, ok, err := envDuration("MOCKAGENTS_OIDC_SESSION_TTL"); err != nil {
		return nil, err
	} else if ok {
		ttl = d
	}
	secureCookies, err := envBool("MOCKAGENTS_OIDC_SECURE_COOKIES")
	if err != nil {
		return nil, err
	}

	auth, err := oidcauth.New(ctx, oidcauth.Settings{
		Issuer:       issuer,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirect,
	})
	if err != nil {
		return nil, err
	}
	secure := strings.HasPrefix(strings.ToLower(redirect), "https://") || secureCookies
	logger.Info("SSO/OIDC login enabled",
		"issuer", issuer, "mapped_domains", len(domainMap), "default_role", string(role))
	return &server.SSOHandlers{
		Auth:        auth,
		Store:       store,
		DomainMap:   domainMap,
		DefaultRole: role,
		SessionTTL:  ttl,
		Secure:      secure,
	}, nil
}

// loadQuotaOverrides seeds the enforcer with every tenant's persisted quota
// override at startup. Returns the number applied.
func loadQuotaOverrides(ctx context.Context, store tenancy.Store, enf *quota.Enforcer) (int, error) {
	tenants, err := store.ListTenants(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range tenants {
		q, err := store.GetTenantQuota(ctx, t.ID)
		if err != nil {
			return n, err
		}
		if q != nil {
			enf.SetOverride(t.ID, *q)
			n++
		}
	}
	return n, nil
}

// parseDomainMap parses "acme.com=ten_acme,beta.com=ten_beta" into a
// lowercase-domain → tenant-id map.
func parseDomainMap(s string) map[string]string {
	m := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) != 2 {
			continue
		}
		domain := strings.ToLower(strings.TrimSpace(kv[0]))
		tenant := strings.TrimSpace(kv[1])
		if domain != "" && tenant != "" {
			m[domain] = tenant
		}
	}
	return m
}

func parseLogLevel(cmd *cobra.Command) (slog.Level, error) {
	level, _ := cmd.Flags().GetString("log-level")
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("unknown log level %q (use debug, info, warn, error)", level)
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

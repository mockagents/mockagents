package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
)

// AgentDirWatcher watches an agents directory for YAML changes and
// re-registers any agent whose definition file is created, written,
// or moved into place. It does NOT react to Pipeline or TestSuite
// kinds — those are consumed by one-shot commands (mockagents test)
// rather than the long-running server.
//
// The watcher debounces fsnotify events so saves that produce a
// Create+Write pair (the typical "atomic save" pattern from editors)
// don't trigger two reloads.
//
// Reload model (X-05): this is an incremental, add/replace-only reload.
// Each file change re-registers exactly one agent via a single
// Registry.Register call. Register holds the registry's write lock while
// every read holds the read lock, so a concurrent request never observes a
// half-updated agent — no transactional bulk-replace is needed because
// there is no bulk replace (pipelines are not reloaded at all; they are
// boot-only). Known limitation: the watcher reacts only to
// Create/Write/Rename, so an agent whose file is DELETED or whose
// metadata.name is RENAMED stays registered under its old key until the
// server restarts (Registry.Remove exists but is not wired to fsnotify
// Remove events, which would require tracking file→agent-name mappings).
type AgentDirWatcher struct {
	Dir      string
	Engine   *engine.Engine
	Logger   *slog.Logger
	Debounce time.Duration

	fsw     *fsnotify.Watcher
	cancel  context.CancelFunc
	done    chan struct{}
	pending map[string]*time.Timer
	mu      sync.Mutex
}

// NewAgentDirWatcher constructs a watcher but does not start it.
// Call Start to begin observing filesystem events.
func NewAgentDirWatcher(dir string, eng *engine.Engine, logger *slog.Logger) *AgentDirWatcher {
	return &AgentDirWatcher{
		Dir:      dir,
		Engine:   eng,
		Logger:   logger,
		Debounce: 150 * time.Millisecond,
		pending:  make(map[string]*time.Timer),
	}
}

// Start begins watching in a background goroutine. It returns once the
// fsnotify watcher is registered, so callers can race-free check for
// startup errors. The watcher runs until Stop is called or the engine
// shuts down.
func (w *AgentDirWatcher) Start() error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	if err := fsw.Add(w.Dir); err != nil {
		_ = fsw.Close()
		return fmt.Errorf("watch %s: %w", w.Dir, err)
	}
	w.fsw = fsw
	w.done = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.loop(ctx)

	w.Logger.Info("watching agents directory", "dir", w.Dir)
	return nil
}

// Stop terminates the watcher and releases the OS-level handles.
// Idempotent.
func (w *AgentDirWatcher) Stop() {
	if w.cancel == nil {
		return
	}
	w.cancel()
	w.cancel = nil
	<-w.done
	if w.fsw != nil {
		_ = w.fsw.Close()
		w.fsw = nil
	}
}

// loop drains fsnotify events and dispatches them to schedule().
// Runs until the context is cancelled.
func (w *AgentDirWatcher) loop(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if !isAgentFile(event.Name) {
				continue
			}
			// Only react to events that mean "this file now has new
			// content on disk": Create, Write, and Rename (editor
			// atomic-save rename-into-place).
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			w.schedule(event.Name)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.Logger.Warn("agent dir watcher error", "error", err)
		}
	}
}

// schedule queues a reload for path, collapsing bursts of events that
// land inside the debounce window into a single reload.
func (w *AgentDirWatcher) schedule(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if timer, ok := w.pending[path]; ok {
		timer.Stop()
	}
	w.pending[path] = time.AfterFunc(w.Debounce, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()
		w.reloadFile(path)
	})
}

// reloadFile parses and validates a single YAML file and registers the
// resulting agent with the engine's registry. Pipeline and TestSuite
// documents are ignored here; they have no server-side effect.
func (w *AgentDirWatcher) reloadFile(path string) {
	result, err := config.LoadFile(path)
	if err != nil {
		// A missing file is normal during editor atomic-save (write
		// temp → rename into place); fsnotify reports the temp name
		// which vanishes before we get here.
		if isTransientMissing(err) {
			return
		}
		w.Logger.Warn("watcher: failed to load file", "file", path, "error", err)
		return
	}
	// Only Agent-kind documents are live-reloadable. Pipelines and
	// TestSuites are static inputs to one-shot commands.
	if result.Definition.Kind != "" && result.Definition.Kind != "Agent" {
		return
	}
	config.ApplyDefaults(result.Definition)

	validator := &config.Validator{}
	if errList := validator.Validate(result.Definition, result.FilePath, result.Node); errList != nil {
		w.Logger.Warn("watcher: validation failed, keeping previous definition",
			"file", path,
			"errors", errList.Error(),
		)
		return
	}
	w.Engine.Registry.Register(result.Definition)
	w.Logger.Info("watcher: agent reloaded",
		"agent", result.Definition.Metadata.Name,
		"file", filepath.Base(path),
	)
}

// isAgentFile reports whether the given path looks like a document
// the watcher should consider: YAML or JSON, and not a hidden or
// editor-temp file.
func isAgentFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(base))
	return ext == ".yaml" || ext == ".yml" || ext == ".json"
}

// isTransientMissing recognizes the "file disappeared between the
// fsnotify event and our read" case. fsnotify does not give us a typed
// error we can match, so we check the wrapped error text.
func isTransientMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "cannot find the file") ||
		errors.Is(err, fsnotify.ErrEventOverflow)
}

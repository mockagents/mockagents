package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
// Reload model (X-05): this is an incremental reload. Each Create/Write/
// Rename re-registers exactly one agent via a single Registry.Register
// call, and each Remove (or rename-away) unregisters the agent that file
// last declared via Registry.Remove. Register/Remove hold the registry's
// write lock while every read holds the read lock, so a concurrent request
// never observes a half-updated agent — and no transactional bulk-replace
// is needed because there is no bulk replace (pipelines are boot-only and
// not reloaded). The watcher tracks a file→agent-name mapping (fileAgents)
// so a deletion can find the right agent once the file's content is gone;
// it only removes an agent when no remaining tracked file still declares
// that name, which keeps a file *rename* (old path removed, new path added,
// processed in either order) from dropping the agent.
type AgentDirWatcher struct {
	Dir      string
	Engine   *engine.Engine
	Logger   *slog.Logger
	Debounce time.Duration

	fsw     *fsnotify.Watcher
	cancel  context.CancelFunc
	done    chan struct{}
	pending map[string]*time.Timer
	// fileAgents maps a watched file path to the agent name it last
	// registered, so a delete or rename-away can unregister the right agent
	// (the file's content is gone by the time we react). Guarded by mu.
	fileAgents map[string]string
	mu         sync.Mutex
	// closed is set by Stop under mu so no new debounce timer is armed and
	// any timer firing during/after Stop bails instead of mutating the
	// registry post-teardown (F-WT-001). stopOnce makes Stop safe under
	// concurrent calls (F-WT-003); wg tracks in-flight reloadFile callbacks
	// so Stop can wait for them to finish.
	closed   bool
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewAgentDirWatcher constructs a watcher but does not start it.
// Call Start to begin observing filesystem events.
func NewAgentDirWatcher(dir string, eng *engine.Engine, logger *slog.Logger) *AgentDirWatcher {
	return &AgentDirWatcher{
		Dir:        dir,
		Engine:     eng,
		Logger:     logger,
		Debounce:   150 * time.Millisecond,
		pending:    make(map[string]*time.Timer),
		fileAgents: make(map[string]string),
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

// Stop terminates the watcher and releases the OS-level handles. It is
// safe to call concurrently and more than once (F-WT-003): the teardown
// body runs exactly once under stopOnce.
//
// Ordering guarantees so nothing touches the registry after Stop returns
// (F-WT-001): under mu we set closed (no new debounce timer is armed and
// any timer that fires from here on bails) and Stop every pending timer;
// then we cancel the loop, wait for it to exit, and wg.Wait() for any
// reloadFile callback that had already started before closed was set.
// Only then is the fsnotify handle closed.
func (w *AgentDirWatcher) Stop() {
	w.stopOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		for path, timer := range w.pending {
			timer.Stop()
			delete(w.pending, path)
		}
		w.mu.Unlock()

		if w.cancel != nil {
			w.cancel()
		}
		if w.done != nil {
			<-w.done
		}
		// Drain reload callbacks that slipped past the closed check before
		// it was set, so the registry is quiescent before we release the
		// OS handle.
		w.wg.Wait()
		if w.fsw != nil {
			_ = w.fsw.Close()
		}
	})
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
			// React to events that change what is on disk: Create/Write/
			// Rename mean "new content to (re)register", and Remove (or the
			// old name of a rename) means "file gone, maybe unregister".
			// reloadFile distinguishes the two by whether the file loads.
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
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
	// Normalize so the same file referenced via different separators or a
	// "./" prefix collapses to one pending entry / fileAgents key (F-WT-004).
	path = filepath.Clean(path)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return // teardown in progress; do not arm new reloads
	}
	if timer, ok := w.pending[path]; ok {
		timer.Stop()
	}
	w.pending[path] = time.AfterFunc(w.Debounce, func() {
		w.mu.Lock()
		delete(w.pending, path)
		if w.closed {
			// Stop() ran after this timer fired but before it took mu.
			// Skip the reload so we never mutate the registry post-teardown.
			w.mu.Unlock()
			return
		}
		// Register the in-flight reload before releasing mu so Stop's
		// wg.Wait() cannot miss it. The Add is under the same lock that
		// guards closed, so it strictly precedes Stop setting closed.
		w.wg.Add(1)
		w.mu.Unlock()
		defer w.wg.Done()
		w.reloadFile(path)
	})
}

// reloadFile parses and validates a single YAML file and registers the
// resulting agent with the engine's registry. Pipeline and TestSuite
// documents are ignored here; they have no server-side effect.
func (w *AgentDirWatcher) reloadFile(path string) {
	// Canonicalize so a path arriving via a direct call matches the keys
	// schedule()/rememberFile() use for pending and fileAgents (F-WT-004).
	path = filepath.Clean(path)
	result, err := config.LoadFile(path)
	if err != nil {
		if isTransientMissing(err) {
			// The file is gone. If we had registered an agent from this
			// path, this is a real deletion (or rename-away), so unregister
			// it. An untracked path — e.g. an editor temp file that vanished
			// mid atomic-save — has no mapping and is left alone.
			w.unregisterFile(path)
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
	w.rememberFile(path, result.Definition.Metadata.Name)
	w.Logger.Info("watcher: agent reloaded",
		"agent", result.Definition.Metadata.Name,
		"file", filepath.Base(path),
	)
}

// rememberFile records that path now declares agent `name`. If the path
// previously declared a different name (an in-file metadata.name rename),
// the old registration is dropped unless another file still declares it.
func (w *AgentDirWatcher) rememberFile(path, name string) {
	w.mu.Lock()
	prev, had := w.fileAgents[path]
	w.fileAgents[path] = name
	w.mu.Unlock()
	if had && prev != name {
		w.removeIfUnclaimed(prev, path)
	}
}

// unregisterFile handles a file that has disappeared: it drops the path's
// mapping and unregisters the agent it declared, unless another tracked
// file still declares that same name.
func (w *AgentDirWatcher) unregisterFile(path string) {
	w.mu.Lock()
	name, ok := w.fileAgents[path]
	if ok {
		delete(w.fileAgents, path)
	}
	w.mu.Unlock()
	if !ok {
		return
	}
	w.removeIfUnclaimed(name, path)
}

// removeIfUnclaimed removes `name` from the registry unless some other
// tracked file still maps to it. This makes a file rename safe regardless of
// event ordering: if the new path registered the agent before the old path's
// removal is processed, the new path still claims the name and we keep it.
// Caller must not hold w.mu.
func (w *AgentDirWatcher) removeIfUnclaimed(name, viaPath string) {
	w.mu.Lock()
	claimed := false
	for _, n := range w.fileAgents {
		if n == name {
			claimed = true
			break
		}
	}
	w.mu.Unlock()
	if claimed {
		return
	}
	if err := w.Engine.Registry.Remove(name); err != nil {
		// Already gone (e.g. removed by another path under the same name).
		return
	}
	w.Logger.Info("watcher: agent removed", "agent", name, "file", filepath.Base(viaPath))
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
	// errors.Is(fs.ErrNotExist) is the locale- and wrapping-robust way to
	// detect "file is gone": config.LoadFile wraps os.ReadFile's *PathError
	// with %w, and ENOENT (Unix) / ERROR_FILE_NOT_FOUND (Windows) both
	// satisfy fs.ErrNotExist — so this replaces the old fragile substring
	// match on "no such file" / "cannot find the file" (F-WT-005).
	return errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, fsnotify.ErrEventOverflow)
}

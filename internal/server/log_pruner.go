package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
)

// DefaultLogPruneInterval is how often the retention pruner runs when
// Config.LogMaxRows is set.
const DefaultLogPruneInterval = 1 * time.Minute

// logPruner enforces the interaction-log retention bound (Config.LogMaxRows) by
// periodically deleting the oldest rows beyond the newest N. It runs on its own
// goroutine with an explicit Stop so the server lifecycle controls it cleanly,
// independent of the async write worker (SEC-05).
type logPruner struct {
	store    *storage.SQLiteStore
	maxRows  int
	interval time.Duration
	logger   *slog.Logger
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newLogPruner(store *storage.SQLiteStore, maxRows int, interval time.Duration, logger *slog.Logger) *logPruner {
	if interval <= 0 {
		interval = DefaultLogPruneInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &logPruner{
		store:    store,
		maxRows:  maxRows,
		interval: interval,
		logger:   logger,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// start launches the prune loop. It prunes once immediately so a restart that
// inherits an already-oversized table trims it at boot, then on every tick.
func (p *logPruner) start() {
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		p.pruneOnce()
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				p.pruneOnce()
			}
		}
	}()
}

func (p *logPruner) pruneOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := p.store.PruneToMaxRows(ctx, p.maxRows)
	if err != nil {
		p.logger.Warn("interaction-log retention prune failed", "error", err, "max_rows", p.maxRows)
		return
	}
	if n > 0 {
		p.logger.Debug("pruned old interaction logs", "deleted", n, "max_rows", p.maxRows)
	}
}

// Stop signals the loop to exit and waits for it. Safe to call more than once.
func (p *logPruner) Stop() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() { close(p.stop) })
	<-p.done
}

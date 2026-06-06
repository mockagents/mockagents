// Package quota enforces per-tenant request-rate and monthly-spend caps
// (REF-08 slice C). It is enforcement-only — no payment provider, no invoices.
//
// The Enforcer holds, per tenant, a token bucket for the rate limit and a
// running spend counter for the current UTC month. Both are in-memory and
// single-process: counters reset on restart and are not shared across replicas.
// Accurate cross-restart / multi-replica accounting is a documented follow-on
// that backs the counters with Postgres atomic increments (see the REF-08
// design). Per-tenant config overrides are likewise in-memory for now; the
// persistent tenant_quotas table is the same follow-on.
//
// A tenant id of "" (single-tenant / anonymous traffic) is never rate- or
// spend-limited, so single-tenant deployments are unaffected.
package quota

import (
	"context"
	"math"
	"sync"
	"time"
)

// SpendBackend is a shared, atomic per-(tenant, month) spend ledger. When an
// Enforcer has one, spend accounting goes through it so the cap is enforced
// across replicas (each AddSpend is an atomic increment over a shared row), not
// per-process. nil leaves spend in-memory/single-process. The tenancy store
// implements this against SQLite/Postgres.
type SpendBackend interface {
	// AddSpend atomically increments the tenant's spend for the given UTC month
	// and returns the new running total (reflecting other replicas' increments).
	AddSpend(ctx context.Context, tenantID, month string, usd float64) (float64, error)
	// GetSpend returns the tenant's current total for the month (0 if none).
	GetSpend(ctx context.Context, tenantID, month string) (float64, error)
}

// spendCacheTTL bounds how long a replica serves CheckSpend from its cached view
// of the shared total before re-reading the backend. Small enough that the cap
// is enforced within a few seconds across replicas, large enough to keep the
// per-request DB read off the hot path. Write-through on AddSpend keeps the
// active replica's view fresh between refreshes.
const spendCacheTTL = 5 * time.Second

// Config is the quota for one tenant. A zero RatePerSec or MonthlySpendUSD
// means "unlimited" for that dimension, so the zero Config enforces nothing.
type Config struct {
	// RatePerSec is the sustained request rate. 0 = unlimited.
	RatePerSec float64 `json:"rate_per_sec"`
	// RateBurst is the token-bucket capacity (max burst). When <= 0 it derives
	// from RatePerSec (at least 1), so a caller can set just the rate.
	RateBurst int `json:"rate_burst"`
	// MonthlySpendUSD is the spend cap for the current UTC month. 0 = unlimited.
	MonthlySpendUSD float64 `json:"monthly_spend_usd"`
}

// Usage is a tenant's current consumption, returned by the quota endpoint.
type Usage struct {
	Month    string  `json:"month"`     // "2006-01" (UTC)
	SpendUSD float64 `json:"spend_usd"` // accrued spend this month (this process)
}

type bucket struct {
	tokens float64
	last   time.Time
}

type counter struct {
	month string
	usd   float64
	// expiry is when this cached total goes stale (backend mode only).
	expiry time.Time
}

// Enforcer is the per-tenant quota state. The zero value is not usable; use
// NewEnforcer. All methods are safe for concurrent use.
type Enforcer struct {
	mu        sync.Mutex
	defaults  Config
	overrides map[string]Config
	buckets   map[string]*bucket
	// spendCache holds each tenant's current-month spend: the authoritative
	// counter in in-memory mode, or a short-TTL cache of the shared total in
	// backend mode.
	spendCache map[string]*counter
	// spendBackend, when non-nil, is the shared spend ledger (multi-replica).
	spendBackend SpendBackend
	// now is injectable so tests can advance time deterministically.
	now func() time.Time
}

// NewEnforcer returns an Enforcer with the given default config applied to
// every tenant that has no override.
func NewEnforcer(defaults Config) *Enforcer {
	return &Enforcer{
		defaults:   defaults,
		overrides:  make(map[string]Config),
		buckets:    make(map[string]*bucket),
		spendCache: make(map[string]*counter),
		now:        time.Now,
	}
}

// SetSpendBackend installs a shared spend ledger so spend is accounted across
// replicas. Nil (the default) keeps spend in-memory/single-process. Call once
// at startup before serving.
func (e *Enforcer) SetSpendBackend(b SpendBackend) {
	e.mu.Lock()
	e.spendBackend = b
	e.mu.Unlock()
}

// SetOverride sets a per-tenant config that takes precedence over the defaults.
// The change applies to the next request (the bucket re-reads the rate).
func (e *Enforcer) SetOverride(tenantID string, c Config) {
	if tenantID == "" {
		return
	}
	e.mu.Lock()
	e.overrides[tenantID] = c
	e.mu.Unlock()
}

// Effective returns the config in force for a tenant: its override if set, else
// the defaults.
func (e *Enforcer) Effective(tenantID string) Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.effectiveLocked(tenantID)
}

func (e *Enforcer) effectiveLocked(tenantID string) Config {
	if c, ok := e.overrides[tenantID]; ok {
		return c
	}
	return e.defaults
}

func capacityFor(c Config) float64 {
	if c.RateBurst > 0 {
		return float64(c.RateBurst)
	}
	return math.Max(1, c.RatePerSec)
}

// AllowRequest applies the rate limit for a tenant. It returns (true, 0) when a
// token was available (and consumes it), or (false, retryAfter) when the bucket
// is empty. An empty tenant id or an unlimited rate always allows.
func (e *Enforcer) AllowRequest(tenantID string) (bool, time.Duration) {
	if tenantID == "" {
		return true, 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg := e.effectiveLocked(tenantID)
	if cfg.RatePerSec <= 0 {
		return true, 0
	}
	cap := capacityFor(cfg)
	now := e.now()
	b := e.buckets[tenantID]
	if b == nil {
		// A new tenant starts with a full bucket.
		b = &bucket{tokens: cap, last: now}
		e.buckets[tenantID] = b
	}
	// Refill since the last check, clamped to capacity.
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(cap, b.tokens+elapsed*cfg.RatePerSec)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	need := 1 - b.tokens
	retry := time.Duration(need / cfg.RatePerSec * float64(time.Second))
	return false, retry
}

// CheckSpend reports whether a tenant is still under its monthly spend cap. It
// does NOT consume anything — call AddSpend after a response's cost is known.
// An empty tenant id or an unlimited cap always passes. In backend mode the
// total reflects all replicas (refreshed within spendCacheTTL).
func (e *Enforcer) CheckSpend(tenantID string) bool {
	if tenantID == "" {
		return true
	}
	if e.Effective(tenantID).MonthlySpendUSD <= 0 {
		return true
	}
	total, _ := e.currentSpend(tenantID)
	return total < e.Effective(tenantID).MonthlySpendUSD
}

// AddSpend accrues usd against a tenant's current-month total. In backend mode
// it is an atomic shared increment whose returned global total refreshes this
// replica's cache; a backend error falls back to a best-effort local bump.
// Non-positive amounts and empty tenant ids are ignored.
func (e *Enforcer) AddSpend(tenantID string, usd float64) {
	if tenantID == "" || usd <= 0 {
		return
	}
	month := e.monthKey()

	e.mu.Lock()
	backend := e.spendBackend
	e.mu.Unlock()

	if backend != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		total, err := backend.AddSpend(ctx, tenantID, month, usd)
		cancel()
		e.mu.Lock()
		c := e.counterForMonthLocked(tenantID, month)
		if err == nil {
			c.usd = total // authoritative cross-replica total
			c.expiry = e.now().Add(spendCacheTTL)
		} else {
			c.usd += usd // best-effort local fallback so we don't lose the charge
		}
		e.mu.Unlock()
		return
	}

	e.mu.Lock()
	e.counterForMonthLocked(tenantID, month).usd += usd
	e.mu.Unlock()
}

// Usage returns a tenant's current-month spend snapshot.
func (e *Enforcer) Usage(tenantID string) Usage {
	month := e.monthKey()
	total, _ := e.currentSpend(tenantID)
	return Usage{Month: month, SpendUSD: total}
}

// currentSpend returns the tenant's current-month total. In in-memory mode it
// reads the local counter; in backend mode it serves a fresh cache or refreshes
// from the shared ledger (without holding the lock during the DB read).
func (e *Enforcer) currentSpend(tenantID string) (float64, error) {
	month := e.monthKey()

	e.mu.Lock()
	backend := e.spendBackend
	c := e.counterForMonthLocked(tenantID, month)
	if backend == nil || e.now().Before(c.expiry) {
		v := c.usd
		e.mu.Unlock()
		return v, nil
	}
	e.mu.Unlock()

	// Cache stale: refresh from the shared ledger off the lock.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	total, err := backend.GetSpend(ctx, tenantID, month)
	cancel()

	e.mu.Lock()
	defer e.mu.Unlock()
	c = e.counterForMonthLocked(tenantID, month)
	if err == nil {
		c.usd = total
		c.expiry = e.now().Add(spendCacheTTL)
	}
	// On error, serve the last-known (stale) value rather than fail open/closed.
	return c.usd, err
}

// monthKey is the current UTC month bucket key, "2006-01".
func (e *Enforcer) monthKey() string {
	return e.now().UTC().Format("2006-01")
}

// counterForMonthLocked returns the tenant's spend counter for the given month,
// rolling it over (resetting) when the cached month differs. Caller holds e.mu.
func (e *Enforcer) counterForMonthLocked(tenantID, month string) *counter {
	c := e.spendCache[tenantID]
	if c == nil || c.month != month {
		c = &counter{month: month}
		e.spendCache[tenantID] = c
	}
	return c
}

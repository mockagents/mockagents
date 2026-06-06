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
	"math"
	"sync"
	"time"
)

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
}

// Enforcer is the per-tenant quota state. The zero value is not usable; use
// NewEnforcer. All methods are safe for concurrent use.
type Enforcer struct {
	mu        sync.Mutex
	defaults  Config
	overrides map[string]Config
	buckets   map[string]*bucket
	spend     map[string]*counter
	// now is injectable so tests can advance time deterministically.
	now func() time.Time
}

// NewEnforcer returns an Enforcer with the given default config applied to
// every tenant that has no override.
func NewEnforcer(defaults Config) *Enforcer {
	return &Enforcer{
		defaults:  defaults,
		overrides: make(map[string]Config),
		buckets:   make(map[string]*bucket),
		spend:     make(map[string]*counter),
		now:       time.Now,
	}
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
// An empty tenant id or an unlimited cap always passes.
func (e *Enforcer) CheckSpend(tenantID string) bool {
	if tenantID == "" {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg := e.effectiveLocked(tenantID)
	if cfg.MonthlySpendUSD <= 0 {
		return true
	}
	return e.counterLocked(tenantID).usd < cfg.MonthlySpendUSD
}

// AddSpend accrues usd against a tenant's current-month counter. Non-positive
// amounts and empty tenant ids are ignored.
func (e *Enforcer) AddSpend(tenantID string, usd float64) {
	if tenantID == "" || usd <= 0 {
		return
	}
	e.mu.Lock()
	e.counterLocked(tenantID).usd += usd
	e.mu.Unlock()
}

// Usage returns a tenant's current-month spend snapshot.
func (e *Enforcer) Usage(tenantID string) Usage {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.counterLocked(tenantID)
	return Usage{Month: c.month, SpendUSD: c.usd}
}

// counterLocked returns the tenant's spend counter for the current UTC month,
// rolling it over (resetting to zero) at a month boundary. Caller holds e.mu.
func (e *Enforcer) counterLocked(tenantID string) *counter {
	month := e.now().UTC().Format("2006-01")
	c := e.spend[tenantID]
	if c == nil || c.month != month {
		c = &counter{month: month}
		e.spend[tenantID] = c
	}
	return c
}

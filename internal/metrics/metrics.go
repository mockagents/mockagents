// Package metrics implements the process-wide Prometheus surface that
// GET /metrics renders (FR-J02).
//
// It is deliberately dependency-free: the exposition format is a small,
// stable text protocol, and hand-writing it keeps the static binary the
// project's differentiator rather than pulling the full client_golang
// dependency tree into a mock server. Correctness is not taken on faith —
// exposition_test.go parses this package's own output with the upstream
// Prometheus text parser (a test-only dependency), so a format regression
// fails the build.
//
// The package is a LEAF: it imports only the standard library, so both
// internal/engine (scenario + chaos counters) and internal/server (request
// counters, the handler) can depend on it without an import cycle.
package metrics

import (
	"math"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Namespace prefixes every metric family this package exports.
const Namespace = "mockagents"

// DefaultMaxSeries bounds the number of distinct label combinations a single
// metric family will track. Agent and scenario names come from fixtures (a
// small, static set), so this ceiling is unreachable in normal use — it exists
// so a pathological config, or a future label sourced from request data, can
// never turn the registry into an unbounded memory leak. Rejected observations
// are counted by mockagents_metrics_series_dropped_total rather than silently
// discarded, so the gap is visible to whoever reads the dashboard.
const DefaultMaxSeries = 1000

// durationBuckets are the request-latency histogram bounds, in seconds.
//
// The lower half is dense because an un-chaosed mock answers in tens of
// microseconds — bucketing that traffic at Prometheus' stock 5ms floor would
// put every normal request in the first bucket and make the histogram useless.
// The upper half stays wide so injected chaos latency (capped at 60s) still
// lands somewhere meaningful before +Inf.
var durationBuckets = []float64{
	0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025,
	0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30,
}

// Scenario-match kinds recorded by RecordScenarioMatch. The three are
// mutually exclusive and together cover every request that reached matching,
// so rule / (rule + default + fallback) is the scenario-match rate.
const (
	// MatchRule — an explicit match rule selected the scenario.
	MatchRule = "rule"
	// MatchDefault — no rule matched, so the agent's default scenario
	// (the one declared with no match block) answered.
	MatchDefault = "default"
	// MatchFallback — no rule matched AND the agent declares no default,
	// so the engine's built-in placeholder answered. A non-zero rate here
	// means the fixtures do not cover the traffic under test.
	MatchFallback = "fallback"
)

// Chaos injection kinds recorded by RecordChaos.
const (
	ChaosLatency    = "latency"
	ChaosError      = "error"
	ChaosRateLimit  = "rate_limit"
	ChaosConnection = "connection"
)

// goVersion is the Go toolchain that built this binary, used as a
// build_info label. Wrapped so New and SetVersion cannot drift apart on it.
func goVersion() string { return runtime.Version() }

// unknown replaces an empty label value. A request rejected before the
// adapter resolved an agent still has to land in some series, and Prometheus
// treats an empty label value as an absent label — which would silently merge
// those requests into a different, wrong series.
const unknown = "unknown"

func orUnknown(s string) string {
	if s == "" {
		return unknown
	}
	return s
}

// gaugeFunc is a gauge sampled at scrape time rather than accumulated.
type gaugeFunc struct {
	name   string
	help   string
	fn     func() float64
	labels []string
	values []string
}

// Registry holds every metric family for one process.
//
// The zero value is not usable — construct with New. All methods are safe for
// concurrent use; the recording paths take a read lock and one atomic add for
// an already-known series, so steady-state cost is a map lookup plus an
// atomic increment per observation.
type Registry struct {
	start time.Time

	requests  *counterVec[reqKey]
	scenarios *counterVec[scenarioKey]
	chaos     *counterVec[chaosKey]
	durations *histogramVec[protoKey]

	dropped atomic.Uint64

	mu     sync.RWMutex
	gauges []gaugeFunc
}

type reqKey struct{ protocol, agent, status string }
type scenarioKey struct{ agent, scenario, kind string }
type chaosKey struct{ agent, kind string }
type protoKey struct{ protocol string }

// New returns an empty Registry stamped with the build version. version is
// rendered as a label on mockagents_build_info so a dashboard can tell which
// binary produced a series.
func New(version string) *Registry {
	if version == "" {
		version = "dev"
	}
	r := &Registry{start: time.Now()}

	r.requests = newCounterVec(
		Namespace+"_requests_total",
		"Total mock LLM/engine requests served, by wire protocol, resolved agent, and HTTP status.",
		[]string{"protocol", "agent", "status"},
		func(k reqKey) []string { return []string{k.protocol, k.agent, k.status} },
		&r.dropped,
	)
	r.scenarios = newCounterVec(
		Namespace+"_scenario_matches_total",
		"Scenario match outcomes, by agent, matched scenario, and how it was selected (rule|default|fallback).",
		[]string{"agent", "scenario", "kind"},
		func(k scenarioKey) []string { return []string{k.agent, k.scenario, k.kind} },
		&r.dropped,
	)
	r.chaos = newCounterVec(
		Namespace+"_chaos_injections_total",
		"Faults injected by the chaos engine, by agent and fault kind.",
		[]string{"agent", "kind"},
		func(k chaosKey) []string { return []string{k.agent, k.kind} },
		&r.dropped,
	)
	r.durations = newHistogramVec(
		Namespace+"_request_duration_seconds",
		"Wall-clock latency of mock LLM/engine requests, including any injected chaos latency.",
		[]string{"protocol"},
		durationBuckets,
		func(k protoKey) []string { return []string{k.protocol} },
		&r.dropped,
	)

	r.RegisterGauge(Namespace+"_build_info",
		"Build metadata; always 1, carrying version and Go toolchain as labels.",
		[]string{"version", "go_version"}, []string{version, goVersion()},
		func() float64 { return 1 })
	r.RegisterGauge(Namespace+"_uptime_seconds",
		"Seconds since this process started serving.",
		nil, nil,
		func() float64 { return time.Since(r.start).Seconds() })

	return r
}

// RegisterGauge adds a gauge sampled at scrape time. labelNames/labelValues
// must be the same length; pass nil for an unlabelled gauge. Registering the
// same name twice REPLACES the previous entry, so a caller re-wiring a
// provider (as the server does once it knows its agent registry) cannot leak
// a duplicate family into the exposition — duplicate families are a hard
// parse error for Prometheus.
func (r *Registry) RegisterGauge(name, help string, labelNames, labelValues []string, fn func() float64) {
	if fn == nil {
		return
	}
	g := gaugeFunc{name: name, help: help, fn: fn, labels: labelNames, values: labelValues}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.gauges {
		if r.gauges[i].name == name {
			r.gauges[i] = g
			return
		}
	}
	r.gauges = append(r.gauges, g)
}

// RecordRequest counts one served request and observes its latency. An empty
// protocol or agent (a request rejected before the adapter resolved either)
// becomes the label value "unknown".
func (r *Registry) RecordRequest(protocol, agent string, status int, d time.Duration) {
	p, a := orUnknown(protocol), orUnknown(agent)
	r.requests.inc(reqKey{protocol: p, agent: a, status: statusLabel(status)})
	r.durations.observe(protoKey{protocol: p}, d.Seconds())
}

// RecordScenarioMatch counts one scenario-selection outcome. kind is one of
// MatchRule, MatchDefault, MatchFallback.
func (r *Registry) RecordScenarioMatch(agent, scenario, kind string) {
	r.scenarios.inc(scenarioKey{
		agent:    orUnknown(agent),
		scenario: orUnknown(scenario),
		kind:     orUnknown(kind),
	})
}

// RecordChaos counts one injected fault. kind is one of the Chaos* constants.
func (r *Registry) RecordChaos(agent, kind string) {
	r.chaos.inc(chaosKey{agent: orUnknown(agent), kind: orUnknown(kind)})
}

// Reset drops every accumulated series, keeping registered gauges. Intended
// for tests that need to assert on exact counts.
func (r *Registry) Reset() {
	r.requests.reset()
	r.scenarios.reset()
	r.chaos.reset()
	r.durations.reset()
	r.dropped.Store(0)
}

// statusLabel renders an HTTP status without allocating for the codes a mock
// server actually returns; strconv.Itoa is the fallback for anything else.
func statusLabel(code int) string {
	switch code {
	case 200:
		return "200"
	case 201:
		return "201"
	case 400:
		return "400"
	case 401:
		return "401"
	case 402:
		return "402"
	case 403:
		return "403"
	case 404:
		return "404"
	case 408:
		return "408"
	case 429:
		return "429"
	case 500:
		return "500"
	case 502:
		return "502"
	case 503:
		return "503"
	case 0:
		// A hijacked connection (a chaos connection-layer fault) never wrote a
		// status line. Give it a label of its own rather than reporting 200.
		return "none"
	}
	return strconv.Itoa(code)
}

// ---- counter vectors -------------------------------------------------------

type counterVec[K comparable] struct {
	name    string
	help    string
	labels  []string
	keyVals func(K) []string
	max     int
	dropped *atomic.Uint64

	mu     sync.RWMutex
	series map[K]*atomic.Uint64
}

func newCounterVec[K comparable](name, help string, labels []string, keyVals func(K) []string, dropped *atomic.Uint64) *counterVec[K] {
	return &counterVec[K]{
		name: name, help: help, labels: labels, keyVals: keyVals,
		max: DefaultMaxSeries, dropped: dropped,
		series: make(map[K]*atomic.Uint64),
	}
}

func (c *counterVec[K]) inc(k K) {
	// Fast path: the series already exists, so a read lock plus one atomic
	// increment is the whole cost. That is steady state after the first
	// request per label combination.
	c.mu.RLock()
	if v, ok := c.series[k]; ok {
		v.Add(1)
		c.mu.RUnlock()
		return
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.series[k]; ok { // lost the race; another goroutine created it
		v.Add(1)
		return
	}
	if len(c.series) >= c.max {
		c.dropped.Add(1)
		return
	}
	v := new(atomic.Uint64)
	v.Add(1)
	c.series[k] = v
}

func (c *counterVec[K]) reset() {
	c.mu.Lock()
	c.series = make(map[K]*atomic.Uint64)
	c.mu.Unlock()
}

// snapshot returns the vector's series with label values resolved, sorted so
// the exposition output is byte-stable across scrapes.
func (c *counterVec[K]) snapshot() []labelledValue {
	c.mu.RLock()
	out := make([]labelledValue, 0, len(c.series))
	for k, v := range c.series {
		out = append(out, labelledValue{values: c.keyVals(k), value: float64(v.Load())})
	}
	c.mu.RUnlock()
	sortLabelled(out)
	return out
}

type labelledValue struct {
	values []string
	value  float64
}

func sortLabelled(s []labelledValue) {
	sort.Slice(s, func(i, j int) bool { return lessValues(s[i].values, s[j].values) })
}

func lessValues(a, b []string) bool {
	for i := range a {
		if i >= len(b) {
			return false
		}
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// ---- histogram vectors -----------------------------------------------------

type histogramSeries struct {
	buckets []atomic.Uint64 // per-bucket counts; cumulated at render time
	count   atomic.Uint64
	sumBits atomic.Uint64 // float64 bits, added via compare-and-swap
}

func (h *histogramSeries) observe(bounds []float64, v float64) {
	// SearchFloat64s returns the first index whose bound is >= v. Prometheus
	// buckets are "less than or equal", so that index IS the bucket. An index
	// past the last bound belongs only to +Inf, which h.count already carries.
	if i := sort.SearchFloat64s(bounds, v); i < len(h.buckets) {
		h.buckets[i].Add(1)
	}
	h.count.Add(1)
	for {
		old := h.sumBits.Load()
		next := math.Float64bits(math.Float64frombits(old) + v)
		if h.sumBits.CompareAndSwap(old, next) {
			return
		}
	}
}

type histogramVec[K comparable] struct {
	name    string
	help    string
	labels  []string
	bounds  []float64
	keyVals func(K) []string
	max     int
	dropped *atomic.Uint64

	mu     sync.RWMutex
	series map[K]*histogramSeries
}

func newHistogramVec[K comparable](name, help string, labels []string, bounds []float64, keyVals func(K) []string, dropped *atomic.Uint64) *histogramVec[K] {
	return &histogramVec[K]{
		name: name, help: help, labels: labels, bounds: bounds, keyVals: keyVals,
		max: DefaultMaxSeries, dropped: dropped,
		series: make(map[K]*histogramSeries),
	}
}

func (h *histogramVec[K]) observe(k K, v float64) {
	h.mu.RLock()
	if s, ok := h.series[k]; ok {
		h.mu.RUnlock()
		s.observe(h.bounds, v)
		return
	}
	h.mu.RUnlock()

	h.mu.Lock()
	s, ok := h.series[k]
	if !ok {
		if len(h.series) >= h.max {
			h.mu.Unlock()
			h.dropped.Add(1)
			return
		}
		s = &histogramSeries{buckets: make([]atomic.Uint64, len(h.bounds))}
		h.series[k] = s
	}
	h.mu.Unlock()
	s.observe(h.bounds, v)
}

func (h *histogramVec[K]) reset() {
	h.mu.Lock()
	h.series = make(map[K]*histogramSeries)
	h.mu.Unlock()
}

type histogramSnapshot struct {
	values     []string
	cumulative []uint64 // len == len(bounds); running total, as Prometheus wants
	count      uint64
	sum        float64
}

func (h *histogramVec[K]) snapshot() []histogramSnapshot {
	h.mu.RLock()
	out := make([]histogramSnapshot, 0, len(h.series))
	for k, s := range h.series {
		snap := histogramSnapshot{
			values:     h.keyVals(k),
			cumulative: make([]uint64, len(h.bounds)),
			count:      s.count.Load(),
			sum:        math.Float64frombits(s.sumBits.Load()),
		}
		var running uint64
		for i := range s.buckets {
			running += s.buckets[i].Load()
			snap.cumulative[i] = running
		}
		out = append(out, snap)
	}
	h.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return lessValues(out[i].values, out[j].values) })
	return out
}

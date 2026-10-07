// Package metrics is a minimal Prometheus-text counter registry (no client
// library, to keep the demo dependency-free). GET /metrics renders it.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// Registry holds labelled counters and gauges.
type Registry struct {
	mu       sync.Mutex
	counters map[string]map[string]float64 // name -> labelset -> value
	help     map[string]string
	kinds    map[string]string
}

// New returns an empty registry with the playground's metric help texts.
func New() *Registry {
	r := &Registry{counters: map[string]map[string]float64{}, help: map[string]string{}, kinds: map[string]string{}}
	for name, h := range map[string]string{
		"playground_runs_started_total":     "Workflow runs started.",
		"playground_runs_finished_total":    "Workflow runs that reached a terminal state.",
		"playground_llm_calls_total":        "Model calls (one per agent call, after retries and fallbacks).",
		"playground_llm_attempts_total":     "Individual model HTTP attempts.",
		"playground_retries_total":          "Retries performed, by reason.",
		"playground_fallbacks_total":        "Calls served by a fallback model.",
		"playground_escalations_total":      "SLM results escalated to the LLM tier.",
		"playground_tool_executions_total":  "Tool executions, by outcome.",
		"playground_reviews_total":          "Review checkpoint decisions.",
		"playground_watchdog_kills_total":   "Runs failed by the watchdog.",
		"playground_guard_violations_total": "Guard (grounding) violations detected.",
		"playground_cost_usd_total":         "Estimated model spend in USD.",
		"playground_tokens_total":           "Tokens consumed, by direction.",
	} {
		r.help[name] = h
		r.kinds[name] = "counter"
	}
	r.help["playground_runs_in_progress"] = "Runs currently in_progress."
	r.kinds["playground_runs_in_progress"] = "gauge"
	return r
}

// Add increments a counter by v. labels are alternating key, value.
func (r *Registry) Add(name string, v float64, labels ...string) {
	if r == nil {
		return
	}
	key := labelKey(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.counters[name]
	if m == nil {
		m = map[string]float64{}
		r.counters[name] = m
	}
	m[key] += v
}

// Inc increments a counter by 1.
func (r *Registry) Inc(name string, labels ...string) { r.Add(name, 1, labels...) }

// Set sets a gauge.
func (r *Registry) Set(name string, v float64, labels ...string) {
	if r == nil {
		return
	}
	key := labelKey(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.counters[name]
	if m == nil {
		m = map[string]float64{}
		r.counters[name] = m
	}
	m[key] = v
}

// Value returns a counter value (tests).
func (r *Registry) Value(name string, labels ...string) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counters[name][labelKey(labels)]
}

// Sum returns the sum of a counter across all label sets.
func (r *Registry) Sum(name string) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var s float64
	for _, v := range r.counters[name] {
		s += v
	}
	return s
}

// Write renders the Prometheus text exposition format.
func (r *Registry) Write(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.counters))
	for n := range r.counters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if h := r.help[n]; h != "" {
			fmt.Fprintf(w, "# HELP %s %s\n", n, h)
		}
		kind := r.kinds[n]
		if kind == "" {
			kind = "counter"
		}
		fmt.Fprintf(w, "# TYPE %s %s\n", n, kind)
		keys := make([]string, 0, len(r.counters[n]))
		for k := range r.counters[n] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "%s%s %g\n", n, k, r.counters[n][k])
		}
	}
}

func labelKey(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(labels); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		v := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(labels[i+1])
		fmt.Fprintf(&b, `%s="%s"`, labels[i], v)
	}
	b.WriteByte('}')
	return b.String()
}

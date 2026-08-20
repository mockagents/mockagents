package metrics

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// newParser builds the upstream parser in LEGACY name-validation mode on
// purpose: legacy validation is the strict one ([a-zA-Z_:][a-zA-Z0-9_:]*), so
// passing it also proves our names work with the classic
// metric_name{label="v"} selector syntax and with pre-UTF-8 Prometheus.
func newParser() expfmt.TextParser {
	return expfmt.NewTextParser(model.LegacyValidation)
}

// parse runs the upstream Prometheus text parser over a body and fails the
// test if it rejects it.
//
// This is the whole point of the file: this package hand-writes the exposition
// format instead of depending on client_golang, so "the output is valid" has
// to be checked by something that is not this package. expfmt is the parser
// Prometheus itself uses, and it is a TEST-ONLY dependency — it is not
// reachable from any non-test file, so the shipped binary stays free of it.
func parse(t *testing.T, body string) map[string]float64 {
	t.Helper()
	p := newParser()
	families, err := p.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("upstream Prometheus parser rejected our output: %v\n---\n%s", err, body)
	}

	// Flatten to name{label="v",...} -> value so assertions read like a
	// PromQL selector.
	out := map[string]float64{}
	for name, mf := range families {
		for _, m := range mf.GetMetric() {
			var labels []string
			for _, lp := range m.GetLabel() {
				labels = append(labels, lp.GetName()+`="`+lp.GetValue()+`"`)
			}
			// The parser preserves emission order; sort so a test key is
			// canonical and does not encode this package's label ordering.
			sort.Strings(labels)
			key := name
			if len(labels) > 0 {
				key += "{" + strings.Join(labels, ",") + "}"
			}
			switch {
			case m.GetCounter() != nil:
				out[key] = m.GetCounter().GetValue()
			case m.GetGauge() != nil:
				out[key] = m.GetGauge().GetValue()
			case m.GetHistogram() != nil:
				h := m.GetHistogram()
				out[key+" _count"] = float64(h.GetSampleCount())
				out[key+" _sum"] = h.GetSampleSum()
				for _, b := range h.GetBucket() {
					out[key+" le="+formatFloat(b.GetUpperBound())] = float64(b.GetCumulativeCount())
				}
			}
		}
	}
	return out
}

// TestParserRejectsMalformedExposition proves the check in parse() can
// actually fail. Without it, a parser that silently accepted anything would
// make every other test in this file vacuous — the same fire-drill discipline
// the repo applies to CI gates (see scripts/install-paths-report.test.sh).
func TestParserRejectsMalformedExposition(t *testing.T) {
	bad := []struct {
		name, body string
	}{
		{"unterminated label value", "# TYPE x counter\nx{a=\"unterminated} 1\n"},
		{"type declared twice", "# TYPE x counter\n# TYPE x gauge\nx 1\n"},
		{"value is not a number", "# TYPE x counter\nx not_a_number\n"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			p := newParser()
			if _, err := p.TextToMetricFamilies(strings.NewReader(tc.body)); err == nil {
				t.Fatalf("parser accepted malformed body %q; the validity check in this file is vacuous", tc.body)
			}
		})
	}
}

func TestRenderIsValidExposition(t *testing.T) {
	r := New("1.2.3")
	r.RecordRequest("openai-chat-completions", "support-bot", 200, 3*time.Millisecond)
	r.RecordRequest("openai-chat-completions", "support-bot", 200, 3*time.Millisecond)
	r.RecordRequest("anthropic-messages", "support-bot", 429, 40*time.Millisecond)
	r.RecordScenarioMatch("support-bot", "refund", MatchRule)
	r.RecordScenarioMatch("support-bot", "catch-all", MatchDefault)
	r.RecordChaos("support-bot", ChaosRateLimit)

	got := parse(t, r.Render())

	cases := map[string]float64{
		`mockagents_requests_total{agent="support-bot",protocol="openai-chat-completions",status="200"}`: 2,
		`mockagents_requests_total{agent="support-bot",protocol="anthropic-messages",status="429"}`:      1,
		`mockagents_scenario_matches_total{agent="support-bot",kind="rule",scenario="refund"}`:           1,
		`mockagents_scenario_matches_total{agent="support-bot",kind="default",scenario="catch-all"}`:     1,
		`mockagents_chaos_injections_total{agent="support-bot",kind="rate_limit"}`:                       1,
		`mockagents_build_info{go_version="` + goVersion() + `",version="1.2.3"}`:                        1,
		`mockagents_metrics_series_dropped_total`:                                                        0,
	}
	for key, want := range cases {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if _, ok := got["mockagents_uptime_seconds"]; !ok {
		t.Error("mockagents_uptime_seconds missing from exposition")
	}
}

// TestEmptyRegistryStillDeclaresEveryFamily guards the "curl | grep chaos"
// affordance: a fresh process must still advertise every family so a zero
// reading is distinguishable from a missing feature.
func TestEmptyRegistryStillDeclaresEveryFamily(t *testing.T) {
	body := New("dev").Render()
	parse(t, body) // must still be valid with no series at all
	for _, name := range []string{
		"mockagents_requests_total",
		"mockagents_scenario_matches_total",
		"mockagents_chaos_injections_total",
		"mockagents_request_duration_seconds",
		"mockagents_metrics_series_dropped_total",
		"mockagents_build_info",
		"mockagents_uptime_seconds",
	} {
		if !strings.Contains(body, "# TYPE "+name+" ") {
			t.Errorf("empty registry omits the %s family header", name)
		}
	}
}

func TestHistogramBucketsAreCumulative(t *testing.T) {
	r := New("dev")
	// 100µs, 1ms, 1s: three different buckets, so a non-cumulative
	// implementation would report 1 in each rather than 1, 2, 3.
	for _, d := range []time.Duration{100 * time.Microsecond, time.Millisecond, time.Second} {
		r.RecordRequest("openai-chat-completions", "a", 200, d)
	}
	got := parse(t, r.Render())

	base := `mockagents_request_duration_seconds{protocol="openai-chat-completions"}`
	if n := got[base+" _count"]; n != 3 {
		t.Errorf("_count = %v, want 3", n)
	}
	// 0.0001 + 0.001 + 1 = 1.0011s
	if sum := got[base+" _sum"]; sum < 1.0010 || sum > 1.0012 {
		t.Errorf("_sum = %v, want ~1.0011", sum)
	}
	for _, tc := range []struct {
		le   string
		want float64
	}{
		{"0.0001", 1}, // just the 100µs observation
		{"0.001", 2},  // + the 1ms one
		{"0.5", 2},    // still two: 1s has not been reached
		{"1", 3},      // all three
		{"30", 3},
	} {
		if n := got[base+" le="+tc.le]; n != tc.want {
			t.Errorf("bucket le=%s = %v, want %v", tc.le, n, tc.want)
		}
	}
}

// TestHostileLabelValuesStayParseable: agent and scenario names come from
// user-authored YAML, so a name containing a quote, a backslash, or a newline
// must not be able to produce output Prometheus cannot read.
func TestHostileLabelValuesStayParseable(t *testing.T) {
	r := New("dev")
	nasty := "he said \"hi\"\\ and\nnewlined"
	r.RecordScenarioMatch(nasty, `back\slash`, MatchRule)
	r.RecordRequest(`quote"protocol`, nasty, 200, time.Millisecond)

	got := parse(t, r.Render())
	key := `mockagents_scenario_matches_total{agent="` + nasty + `",kind="rule",scenario="back\slash"}`
	if got[key] != 1 {
		t.Errorf("hostile label round-trip failed; got map: %v", got)
	}
}

func TestEmptyLabelsBecomeUnknown(t *testing.T) {
	r := New("dev")
	r.RecordRequest("", "", 500, time.Millisecond)
	got := parse(t, r.Render())
	key := `mockagents_requests_total{agent="unknown",protocol="unknown",status="500"}`
	if got[key] != 1 {
		t.Errorf("%s = %v, want 1", key, got[key])
	}
}

// TestOrderingIsDeterministic keeps the exposition diffable: two consecutive
// scrapes of an idle registry must differ only in the uptime gauge.
func TestOrderingIsDeterministic(t *testing.T) {
	r := New("dev")
	for _, agent := range []string{"zeta", "alpha", "mu"} {
		r.RecordRequest("openai-chat-completions", agent, 200, time.Millisecond)
		r.RecordScenarioMatch(agent, "s", MatchRule)
	}
	strip := func(s string) string {
		var keep []string
		for _, line := range strings.Split(s, "\n") {
			if !strings.Contains(line, "uptime_seconds") {
				keep = append(keep, line)
			}
		}
		return strings.Join(keep, "\n")
	}
	if a, b := strip(r.Render()), strip(r.Render()); a != b {
		t.Errorf("two scrapes of an idle registry differ:\n--- first ---\n%s\n--- second ---\n%s", a, b)
	}
	// Sorted by label value, so alpha precedes mu precedes zeta.
	body := r.Render()
	ia, im, iz := strings.Index(body, `agent="alpha"`), strings.Index(body, `agent="mu"`), strings.Index(body, `agent="zeta"`)
	if !(ia < im && im < iz) {
		t.Errorf("series are not sorted by label value: alpha@%d mu@%d zeta@%d", ia, im, iz)
	}
}

func TestCardinalityCeilingDropsAndCounts(t *testing.T) {
	r := New("dev")
	r.requests.max = 3 // shrink the ceiling instead of making 1000 series
	for _, agent := range []string{"a", "b", "c", "d", "e"} {
		r.RecordRequest("p", agent, 200, time.Millisecond)
	}
	got := parse(t, r.Render())
	if n := got["mockagents_metrics_series_dropped_total"]; n != 2 {
		t.Errorf("dropped = %v, want 2 (5 agents, ceiling 3)", n)
	}
	// The three that fit still count normally.
	if got[`mockagents_requests_total{agent="a",protocol="p",status="200"}`] != 1 {
		t.Error("an accepted series was lost when the ceiling was hit")
	}
	// And the ones past the ceiling are absent rather than silently merged.
	if _, ok := got[`mockagents_requests_total{agent="e",protocol="p",status="200"}`]; ok {
		t.Error("a series past the ceiling was recorded anyway")
	}
}

func TestResetClearsSeriesButKeepsGauges(t *testing.T) {
	r := New("dev")
	r.RecordRequest("p", "a", 200, time.Millisecond)
	r.Reset()
	got := parse(t, r.Render())
	if _, ok := got[`mockagents_requests_total{agent="a",protocol="p",status="200"}`]; ok {
		t.Error("Reset left a counter series behind")
	}
	if _, ok := got["mockagents_uptime_seconds"]; !ok {
		t.Error("Reset dropped a registered gauge")
	}
}

func TestRegisterGaugeReplacesRatherThanDuplicates(t *testing.T) {
	r := New("dev")
	r.RegisterGauge("mockagents_agents_loaded", "n", nil, nil, func() float64 { return 1 })
	r.RegisterGauge("mockagents_agents_loaded", "n", nil, nil, func() float64 { return 7 })
	body := r.Render()
	if n := strings.Count(body, "# TYPE mockagents_agents_loaded "); n != 1 {
		t.Fatalf("family declared %d times, want 1 (a duplicate is a parse error)", n)
	}
	if got := parse(t, body)["mockagents_agents_loaded"]; got != 7 {
		t.Errorf("gauge = %v, want 7 (the replacement)", got)
	}
}

package metrics

import (
	"io"
	"sort"
	"strconv"
	"strings"
)

// ContentType is the Prometheus text exposition format's media type. Scrapers
// content-negotiate, but they all accept this and it is what a bare
// `curl localhost:8080/metrics` should render as plain text in a terminal.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// WriteTo renders the registry in the Prometheus text exposition format.
//
// Ordering is fully deterministic — families in a fixed order, series sorted
// by label value — so a diff between two scrapes shows behavior changes, not
// map-iteration noise, and so golden tests are possible at all.
func (r *Registry) WriteTo(w io.Writer) (int64, error) {
	var b strings.Builder

	// Gauges first: build_info and uptime are the orientation metrics an
	// operator looks at before anything else.
	r.mu.RLock()
	gauges := make([]gaugeFunc, len(r.gauges))
	copy(gauges, r.gauges)
	r.mu.RUnlock()
	sort.Slice(gauges, func(i, j int) bool { return gauges[i].name < gauges[j].name })
	for _, g := range gauges {
		writeHeader(&b, g.name, g.help, "gauge")
		b.WriteString(g.name)
		writeLabels(&b, g.labels, g.values)
		b.WriteByte(' ')
		writeFloat(&b, g.fn())
		b.WriteByte('\n')
	}

	writeCounterVec(&b, r.requests.name, r.requests.help, r.requests.labels, r.requests.snapshot())
	writeCounterVec(&b, r.scenarios.name, r.scenarios.help, r.scenarios.labels, r.scenarios.snapshot())
	writeCounterVec(&b, r.chaos.name, r.chaos.help, r.chaos.labels, r.chaos.snapshot())
	writeHistogramVec(&b, r.durations.name, r.durations.help, r.durations.labels, r.durations.bounds, r.durations.snapshot())

	// Self-observability: a non-zero value here means the cardinality ceiling
	// (DefaultMaxSeries) is truncating the other families, so their totals are
	// undercounts. Always emitted, so a dashboard can alert on it existing at
	// all rather than on it appearing.
	dropName := Namespace + "_metrics_series_dropped_total"
	writeHeader(&b, dropName,
		"Observations discarded because a metric family hit its cardinality ceiling.", "counter")
	b.WriteString(dropName)
	b.WriteByte(' ')
	writeFloat(&b, float64(r.dropped.Load()))
	b.WriteByte('\n')

	n, err := io.WriteString(w, b.String())
	return int64(n), err
}

// Render returns the exposition output as a string. Convenience for tests and
// for the CLI; the HTTP handler uses WriteTo.
func (r *Registry) Render() string {
	var b strings.Builder
	_, _ = r.WriteTo(&b)
	return b.String()
}

func writeCounterVec(b *strings.Builder, name, help string, labels []string, series []labelledValue) {
	// A family with no series still gets its HELP/TYPE header. Prometheus
	// accepts that, and it means `curl /metrics | grep chaos` answers "zero
	// injections" instead of "does this build even have chaos metrics?".
	writeHeader(b, name, help, "counter")
	for _, s := range series {
		b.WriteString(name)
		writeLabels(b, labels, s.values)
		b.WriteByte(' ')
		writeFloat(b, s.value)
		b.WriteByte('\n')
	}
}

func writeHistogramVec(b *strings.Builder, name, help string, labels []string, bounds []float64, series []histogramSnapshot) {
	writeHeader(b, name, help, "histogram")
	for _, s := range series {
		for i, bound := range bounds {
			b.WriteString(name)
			b.WriteString("_bucket")
			writeLabelsWithLE(b, labels, s.values, formatFloat(bound))
			b.WriteByte(' ')
			writeFloat(b, float64(s.cumulative[i]))
			b.WriteByte('\n')
		}
		// The +Inf bucket must equal _count, per the exposition format.
		b.WriteString(name)
		b.WriteString("_bucket")
		writeLabelsWithLE(b, labels, s.values, "+Inf")
		b.WriteByte(' ')
		writeFloat(b, float64(s.count))
		b.WriteByte('\n')

		b.WriteString(name)
		b.WriteString("_sum")
		writeLabels(b, labels, s.values)
		b.WriteByte(' ')
		writeFloat(b, s.sum)
		b.WriteByte('\n')

		b.WriteString(name)
		b.WriteString("_count")
		writeLabels(b, labels, s.values)
		b.WriteByte(' ')
		writeFloat(b, float64(s.count))
		b.WriteByte('\n')
	}
}

func writeHeader(b *strings.Builder, name, help, typ string) {
	b.WriteString("# HELP ")
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(escapeHelp(help))
	b.WriteString("\n# TYPE ")
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(typ)
	b.WriteByte('\n')
}

func writeLabels(b *strings.Builder, names, values []string) {
	if len(names) == 0 {
		return
	}
	b.WriteByte('{')
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(n)
		b.WriteString(`="`)
		if i < len(values) {
			b.WriteString(escapeLabelValue(values[i]))
		}
		b.WriteByte('"')
	}
	b.WriteByte('}')
}

// writeLabelsWithLE appends the histogram's le label after the vector's own
// labels. le is already a formatted float (or "+Inf") and needs no escaping.
func writeLabelsWithLE(b *strings.Builder, names, values []string, le string) {
	b.WriteByte('{')
	for i, n := range names {
		b.WriteString(n)
		b.WriteString(`="`)
		if i < len(values) {
			b.WriteString(escapeLabelValue(values[i]))
		}
		b.WriteString(`",`)
	}
	b.WriteString(`le="`)
	b.WriteString(le)
	b.WriteString(`"}`)
}

// escapeHelp escapes a HELP string: backslash and newline only. A double
// quote is legal unescaped in HELP (unlike in a label value).
func escapeHelp(s string) string {
	if !strings.ContainsAny(s, "\\\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeLabelValue escapes a label value: backslash, double quote, newline.
// Agent and scenario names come from user YAML, so this is the boundary that
// keeps a hostile-looking fixture name from producing unparseable output.
func escapeLabelValue(s string) string {
	if !strings.ContainsAny(s, "\\\"\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func writeFloat(b *strings.Builder, v float64) {
	b.WriteString(formatFloat(v))
}

func formatFloat(v float64) string {
	// 'g' with -1 precision round-trips exactly and keeps whole numbers free of
	// a trailing ".0" — Prometheus accepts both, but counters read better as
	// integers in a terminal.
	return strconv.FormatFloat(v, 'g', -1, 64)
}

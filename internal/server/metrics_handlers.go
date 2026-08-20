package server

import (
	"bufio"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/metrics"
)

// MetricsHandler serves the Prometheus text exposition at GET /metrics
// (FR-J02). The chart's ServiceMonitor has always pointed here; until this
// slice there was nothing behind it.
func MetricsHandler(reg *metrics.Registry) http.Handler {
	if reg == nil {
		reg = metrics.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", metrics.ContentType)
		// Scrapes are point-in-time; a cached body would report stale counters
		// through any proxy sitting in front of the server.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = reg.WriteTo(w)
	})
}

// MetricsCapture meters LLM/engine requests into reg.
//
// Placement in the chain matters, and is asserted by
// TestMetricsCaptureSeesQuotaRejections / TestMetricsCaptureWithoutLogStore:
//
//   - INSIDE InteractionCapture, so it reads the *engine.RequestMeta that the
//     adapters stamp with the resolved protocol and agent. When no log store
//     is configured InteractionCapture is absent entirely, so this middleware
//     attaches its own RequestMeta rather than reporting every request as
//     protocol="unknown".
//   - OUTSIDE QuotaEnforce, so a 429 (over rate) or 402 (over spend) is
//     counted. Those are exactly the responses an operator scrapes for.
//
// The Realtime WebSocket surface is deliberately NOT metered here: it
// generates responses in-process on an established socket, so there is no
// per-request status or latency at this seam. Realtime traffic still appears
// in mockagents_scenario_matches_total (recorded by the engine) and in the
// interaction log.
func MetricsCapture(reg *metrics.Registry) func(http.Handler) http.Handler {
	if reg == nil {
		reg = metrics.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Management-API and static traffic is not what "requests by
			// protocol/agent/status" means. isLoggablePath is the same
			// classifier the interaction log uses, so the two surfaces can
			// never disagree about which routes count.
			if !isLoggablePath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			meta := engine.RequestMetaFromContext(r.Context())
			if meta == nil {
				r, meta = engine.WithRequestMeta(r)
			}

			start := time.Now()
			mw := acquireMetricsWriter(w)
			defer releaseMetricsWriter(mw)

			next.ServeHTTP(mw, r)

			reg.RecordRequest(meta.Protocol, meta.AgentName, mw.statusForMetrics(), time.Since(start))
		})
	}
}

// metricsWriter captures the response status for MetricsCapture.
//
// It is a separate wrapper from statusWriter (used by StructuredLogger) for
// one reason: a chaos connection-layer fault HIJACKS the connection and never
// writes a status line, and reporting that as a 200 would make the metrics lie
// about exactly the failure mode this project exists to simulate. Intercepting
// Hijack is the only way to see it from middleware.
type metricsWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	hijacked    bool
}

var metricsWriterPool = sync.Pool{New: func() any { return &metricsWriter{} }}

func acquireMetricsWriter(w http.ResponseWriter) *metricsWriter {
	mw := metricsWriterPool.Get().(*metricsWriter)
	mw.ResponseWriter = w
	mw.status = http.StatusOK
	mw.wroteHeader = false
	mw.hijacked = false
	return mw
}

// releaseMetricsWriter nils the embedded writer so a pooled entry can never
// pin a finished request's connection state, mirroring statusWriter's pool.
func releaseMetricsWriter(mw *metricsWriter) {
	mw.ResponseWriter = nil
	metricsWriterPool.Put(mw)
}

func (w *metricsWriter) WriteHeader(code int) {
	// First call wins, matching statusWriter: a second WriteHeader is a
	// stdlib "superfluous" no-op and must not clobber the recorded status.
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush satisfies http.Flusher. The SSE writer asserts the interface directly
// (internal/streaming/sse_writer.go), so a wrapper that only embedded the
// interface would break every streaming response.
func (w *metricsWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController descend past this wrapper (for
// SetWriteDeadline on SSE responses, and for the interfaces not implemented
// here), matching statusWriter's contract.
func (w *metricsWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack records that the handler took over the connection — a chaos
// connection fault — then delegates down the wrapper chain. Delegation goes
// through ResponseController rather than a direct type assertion because the
// writers below this one (captureWriter, statusWriter) are wrappers too.
func (w *metricsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buf, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, buf, err
}

// statusForMetrics reports 0 for a hijacked connection, which the metrics
// registry labels status="none" — no HTTP status was ever sent.
func (w *metricsWriter) statusForMetrics() int {
	if w.hijacked {
		return 0
	}
	return w.status
}

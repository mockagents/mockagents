// Package observability provides OpenTelemetry wiring for MockAgents.
// Tracing is disabled by default; callers opt in via environment variables:
//
//   OTEL_EXPORTER_OTLP_ENDPOINT — enables the OTLP/HTTP trace exporter
//   MOCKAGENTS_OTEL_STDOUT=1    — enables the stdout exporter (local dev)
//
// When neither is set NewTracerProvider returns a NoOp provider, so
// importing observability is free at runtime.
package observability

import (
	"context"
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	noopTrace "go.opentelemetry.io/otel/trace/noop"
)

// TracerName is the instrumentation name used for every span emitted by
// MockAgents. Consumers can filter by this in their backend.
const TracerName = "github.com/mockagents/mockagents"

// tracingEnabled reports whether a real exporter is installed. The
// default is false (noop provider) and the engine hot path checks this
// flag before building span attributes, which is a measurable win: the
// variadic `[]attribute.KeyValue` allocation + interface dispatch in
// `otel.Tracer(...).Start(...)` is skipped entirely when the user is
// not actively tracing.
//
// Read-only after NewTracerProvider returns.
var tracingEnabled bool

// IsEnabled reports whether tracing is actively exporting to a
// configured backend. Hot-path callers should guard span construction
// with this flag to avoid paying the noop cost per request.
func IsEnabled() bool { return tracingEnabled }

// Shutdown is returned by NewTracerProvider; call it once at program
// exit to flush pending spans. A noop Shutdown is returned when the
// provider is the noop provider.
type Shutdown func(context.Context) error

// NewTracerProvider picks an exporter based on environment variables
// and returns a fully configured TracerProvider plus a Shutdown func.
// The returned provider is also installed as the global otel provider.
func NewTracerProvider(ctx context.Context, serviceName, version string) (trace.TracerProvider, Shutdown, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" &&
		os.Getenv("MOCKAGENTS_OTEL_STDOUT") != "1" {
		tp := noopTrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		return tp, func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, nil, err
	}

	var exporter sdktrace.SpanExporter
	if os.Getenv("MOCKAGENTS_OTEL_STDOUT") == "1" {
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	} else {
		exporter, err = otlptracehttp.New(ctx)
	}
	if err != nil {
		return nil, nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tracingEnabled = true

	return tp, tp.Shutdown, nil
}

// StartSpan is a thin convenience wrapper so callers don't need to
// import otel directly just to start a span.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(TracerName).Start(ctx, name, trace.WithAttributes(attrs...))
}

// RecordError sets the span status to error and attaches the message.
// Safe to call with a nil span.
func RecordError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// HTTPMiddleware wraps an http.Handler with a server span. Minimal
// implementation deliberately: we own the instrumentation surface and
// don't need otelhttp's feature set for the MockAgents endpoints.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := StartSpan(r.Context(), "http.request",
			attribute.String("http.method", r.Method),
			attribute.String("http.route", r.URL.Path),
		)
		defer span.End()
		srw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(srw, r.WithContext(ctx))
		span.SetAttributes(attribute.Int("http.status_code", srw.status))
		if srw.status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(srw.status))
		}
	})
}

// statusRecorder is a thin ResponseWriter wrapper that remembers the
// status code a handler wrote so the middleware can tag the span.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the wrapped writer so SSE streaming keeps working through
// this middleware.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController descend past this wrapper to reach the
// net.Conn (used by the SSE handlers' SetWriteDeadline, F-SV-004).
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// installPropagator sets the same propagator NewTracerProvider installs, and
// restores the previous one afterwards.
func installPropagator(t *testing.T) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

// TestHTTPMiddleware_JoinsIncomingTrace is the audit H-08 guard for
// propagation: a request carrying a W3C `traceparent` must continue that
// trace, not start a fresh root span. Without Extract, a distributed trace
// stopped dead at the MockAgents boundary.
func TestHTTPMiddleware_JoinsIncomingTrace(t *testing.T) {
	rec := newTestTracerProvider(t)
	installPropagator(t)

	const (
		traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		spanID  = "00f067aa0ba902b7"
	)

	var seen trace.SpanContext
	h := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = trace.SpanContextFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-"+spanID+"-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !seen.IsValid() {
		t.Fatal("handler saw no span context; the middleware did not start a span")
	}
	if got := seen.TraceID().String(); got != traceID {
		t.Errorf("trace id = %s, want %s (the incoming traceparent was not joined)", got, traceID)
	}

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	if got := spans[0].SpanContext().TraceID().String(); got != traceID {
		t.Errorf("server span trace id = %s, want %s", got, traceID)
	}
	if got := spans[0].Parent().SpanID().String(); got != spanID {
		t.Errorf("server span parent = %s, want the caller's span %s", got, spanID)
	}
}

// TestHTTPMiddleware_StartsRootSpanWithoutTraceparent: a plain request still
// gets a span, just an unparented one.
func TestHTTPMiddleware_StartsRootSpanWithoutTraceparent(t *testing.T) {
	rec := newTestTracerProvider(t)
	installPropagator(t)

	h := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	if spans[0].Parent().IsValid() {
		t.Errorf("span should be a root, got parent %s", spans[0].Parent().SpanID())
	}
}

// TestNewTracerProvider_EnablesAndShutdownDisables covers the flag the whole
// tracing path hangs on: with an exporter configured, IsEnabled must report
// true (so HTTPMiddleware wraps the handler at all) and Shutdown must clear it.
func TestNewTracerProvider_EnablesAndShutdownDisables(t *testing.T) {
	if IsEnabled() {
		t.Skip("tracing already enabled in this run")
	}
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	// The stdout exporter needs no network and no collector.
	t.Setenv("MOCKAGENTS_OTEL_STDOUT", "1")
	_, shutdown, err := NewTracerProvider(context.Background(), "mockagents-test", "v0.0.0-test")
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	if !IsEnabled() {
		t.Fatal("IsEnabled() = false after configuring an exporter; the documented env vars would stay inert")
	}
	// The middleware must now actually wrap.
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if wrapped := HTTPMiddleware(h); sameHandler(wrapped, h) {
		t.Error("HTTPMiddleware returned the handler unwrapped while tracing is enabled")
	}
	// And a traceparent is understood, which requires the propagator to be set.
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.HeaderCarrier(http.Header{
		"Traceparent": []string{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	}))
	if !trace.SpanContextFromContext(ctx).IsValid() {
		t.Error("no W3C propagator installed: an incoming traceparent is ignored")
	}

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if IsEnabled() {
		t.Error("IsEnabled() = true after shutdown")
	}
}

// sameHandler reports whether two handlers are the same function value.
func sameHandler(a, b http.Handler) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newTestTracerProvider installs an in-memory span recorder as the
// global tracer provider and returns it so individual tests can assert
// on captured spans.
func newTestTracerProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return rec
}

func TestStartSpanCapturesAttributes(t *testing.T) {
	rec := newTestTracerProvider(t)

	_, span := StartSpan(context.Background(), "unit.test",
		attribute.String("agent.name", "demo"),
	)
	span.End()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "unit.test" {
		t.Errorf("name = %s", spans[0].Name())
	}
	var found bool
	for _, kv := range spans[0].Attributes() {
		if kv.Key == "agent.name" && kv.Value.AsString() == "demo" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected agent.name attribute, got %+v", spans[0].Attributes())
	}
}

func TestRecordErrorSetsStatus(t *testing.T) {
	rec := newTestTracerProvider(t)

	_, span := StartSpan(context.Background(), "failing")
	RecordError(span, errors.New("boom"))
	span.End()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status().Code.String() != "Error" {
		t.Errorf("status = %s, want Error", spans[0].Status().Code.String())
	}
}

func TestHTTPMiddlewareEmitsServerSpan(t *testing.T) {
	rec := newTestTracerProvider(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(HTTPMiddleware(inner))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/probe")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("middleware did not emit a span")
	}
	var serverSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "http.request" {
			serverSpan = s
			break
		}
	}
	if serverSpan == nil {
		t.Fatalf("http.request span missing, got %v", spanNames(spans))
	}
	seen := map[string]attribute.Value{}
	for _, kv := range serverSpan.Attributes() {
		seen[string(kv.Key)] = kv.Value
	}
	if seen["http.method"].AsString() != "GET" {
		t.Errorf("http.method = %v", seen["http.method"])
	}
	if seen["http.status_code"].AsInt64() != 200 {
		t.Errorf("http.status_code = %v", seen["http.status_code"])
	}
}

func TestHTTPMiddleware5xxMarksError(t *testing.T) {
	rec := newTestTracerProvider(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewServer(HTTPMiddleware(inner))
	defer ts.Close()

	resp, _ := http.Get(ts.URL)
	_ = resp.Body.Close()

	spans := rec.Ended()
	if spans[0].Status().Code.String() != "Error" {
		t.Errorf("5xx should mark span Error, got %s", spans[0].Status().Code.String())
	}
}

func TestNewTracerProviderDefaultsToNoOp(t *testing.T) {
	// Guard against leaking OTEL envs from the test host.
	for _, k := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "MOCKAGENTS_OTEL_STDOUT"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, "") // empty string is different from unset; both disable.
			defer os.Setenv(k, v)
		}
	}
	tp, shutdown, err := NewTracerProvider(context.Background(), "mockagents", "test")
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()
	if tp == nil {
		t.Fatal("nil provider")
	}
	// The NoOp provider's tracer is fast and never records.
	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "noop")
	span.End()
	// No assertions to make — the point is the call succeeds without
	// emitting anywhere and without panicking.
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = s.Name()
	}
	return out
}

// TestStatusRecorder_UnwrapAndFlush guards the SSE plumbing (F-SV-004): the
// span-recording wrapper must expose Unwrap (so http.ResponseController can
// reach the net.Conn to reset the write deadline) and forward Flush (so SSE
// chunks actually reach the client through this outermost middleware).
func TestStatusRecorder_UnwrapAndFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	if got := sr.Unwrap(); got != rec {
		t.Errorf("Unwrap() = %p, want the wrapped recorder %p", got, rec)
	}
	if _, ok := any(sr).(http.Flusher); !ok {
		t.Fatal("statusRecorder must implement http.Flusher for SSE")
	}
	sr.Flush()
	if !rec.Flushed {
		t.Error("Flush() was not forwarded to the wrapped writer")
	}
}

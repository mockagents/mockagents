package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/observability"
)

// tracerShutdownTimeout bounds the final span flush so a wedged collector
// cannot hold the process open after it has already stopped serving.
const tracerShutdownTimeout = 5 * time.Second

// defaultOTelServiceName is the service name reported to the tracing backend
// when OTEL_SERVICE_NAME is not set.
const defaultOTelServiceName = "mockagents"

// otelServiceName resolves the service name spans are tagged with, honoring
// the standard OTEL_SERVICE_NAME variable so MockAgents looks like any other
// service in the operator's backend.
func otelServiceName() string {
	if v := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); v != "" {
		return v
	}
	return defaultOTelServiceName
}

// setupTracing installs the OpenTelemetry tracer provider and returns its
// shutdown func (never nil).
//
// This call is what made the documented tracing knobs real (audit H-08):
// `observability.NewTracerProvider` existed but nothing called it, so
// `OTEL_EXPORTER_OTLP_ENDPOINT` produced no spans and no error. It must run
// BEFORE server.New, because HTTPMiddleware decides at construction time
// whether to wrap the handler at all — wiring it afterwards would leave the
// HTTP layer permanently untraced.
//
// With neither exporter variable set the provider is a no-op, so the only cost
// on the default path is this function call.
func setupTracing(ctx context.Context, logger *slog.Logger) observability.Shutdown {
	_, shutdown, err := observability.NewTracerProvider(ctx, otelServiceName(), version)
	if err != nil {
		// A tracing backend is an observability aid, not a dependency: a bad
		// endpoint must not stop the mock server from serving. Say so loudly
		// instead, since the operator asked for spans and won't get any.
		logger.Warn("tracing disabled: could not build the tracer provider",
			"error", err,
			"hint", "check OTEL_EXPORTER_OTLP_ENDPOINT (and OTEL_EXPORTER_OTLP_HEADERS) or unset it to silence this")
		return func(context.Context) error { return nil }
	}
	if observability.IsEnabled() {
		logger.Info("tracing enabled",
			"service", otelServiceName(),
			"exporter", tracingExporterName(),
			"spans", "http.request, engine.process_request")
	}
	return shutdown
}

// tracingExporterName reports which exporter the env selected, for the startup
// log line. It mirrors the precedence in observability.NewTracerProvider.
func tracingExporterName() string {
	if os.Getenv("MOCKAGENTS_OTEL_STDOUT") == "1" {
		return "stdout"
	}
	return "otlp/http"
}

// flushTraces runs the tracer shutdown under a bounded context.
func flushTraces(shutdown observability.Shutdown, logger *slog.Logger) {
	if shutdown == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), tracerShutdownTimeout)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		logger.Warn("could not flush pending trace spans", "error", err)
	}
}

package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bufLogger() (*slog.Logger, *strings.Builder) {
	var b strings.Builder
	return slog.New(slog.NewTextHandler(&b, nil)), &b
}

func TestOTelServiceNameAndExporter(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	assert.Equal(t, "mockagents", otelServiceName())
	t.Setenv("OTEL_SERVICE_NAME", "  checkout-mock ")
	assert.Equal(t, "checkout-mock", otelServiceName())

	t.Setenv("MOCKAGENTS_OTEL_STDOUT", "1")
	assert.Equal(t, "stdout", tracingExporterName())
	t.Setenv("MOCKAGENTS_OTEL_STDOUT", "")
	assert.Equal(t, "otlp/http", tracingExporterName())
}

func TestSetupTracing(t *testing.T) {
	ctx := context.Background()

	t.Run("off by default", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
		t.Setenv("MOCKAGENTS_OTEL_STDOUT", "")
		logger, logs := bufLogger()
		shutdown := setupTracing(ctx, logger)
		require.NotNil(t, shutdown)
		assert.False(t, observability.IsEnabled())
		assert.NoError(t, shutdown(ctx))
		assert.Empty(t, logs.String(), "nothing to say when tracing is not configured")
	})

	t.Run("otlp endpoint enables tracing until flushed", func(t *testing.T) {
		// Nothing listens here; the exporter only dials when spans are
		// exported, and flushing an empty batch sends nothing.
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
		t.Setenv("MOCKAGENTS_OTEL_STDOUT", "")
		t.Setenv("OTEL_SERVICE_NAME", "svc-under-test")
		logger, logs := bufLogger()
		shutdown := setupTracing(ctx, logger)
		t.Cleanup(func() { _ = shutdown(ctx) })
		assert.True(t, observability.IsEnabled())
		assert.Contains(t, logs.String(), "tracing enabled")
		assert.Contains(t, logs.String(), "service=svc-under-test")
		assert.Contains(t, logs.String(), "exporter=otlp/http")

		flushTraces(shutdown, logger)
		assert.False(t, observability.IsEnabled(), "the flush stops new spans")
	})

	t.Run("a provider that cannot be built degrades to a warning", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
		t.Setenv("MOCKAGENTS_OTEL_STDOUT", "")
		// A cancelled context makes building the tracer resource fail.
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		logger, logs := bufLogger()
		shutdown := setupTracing(cancelled, logger)
		require.NotNil(t, shutdown)
		assert.NoError(t, shutdown(ctx), "the fallback shutdown is a no-op")
		assert.Contains(t, logs.String(), "tracing disabled")
		assert.False(t, observability.IsEnabled())
	})
}

func TestFlushTraces(t *testing.T) {
	logger, logs := bufLogger()
	flushTraces(nil, logger) // nil is tolerated
	assert.Empty(t, logs.String())

	var gotDeadline bool
	flushTraces(func(ctx context.Context) error {
		_, gotDeadline = ctx.Deadline()
		return errors.New("collector unreachable")
	}, logger)
	assert.True(t, gotDeadline, "the flush is bounded")
	assert.Contains(t, logs.String(), "could not flush pending trace spans")
	assert.Contains(t, logs.String(), "collector unreachable")
}

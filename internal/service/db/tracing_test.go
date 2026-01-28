package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/otel"
)

// newTestTracerProvider creates a tracer provider with in-memory exporter for testing.
func newTestTracerProvider(t *testing.T) (*tracetest.InMemoryExporter, trace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return exporter, tp
}

// TestDBServiceStartSpan_AddsDBSystemAttribute verifies that the database service's
// startSpan method automatically adds the db.system attribute per OTEL semantic conventions.
// This is the db-specific behavior that differentiates it from the common otel.StartSpan.
func TestDBServiceStartSpan_AddsDBSystemAttribute(t *testing.T) {
	t.Parallel()

	exporter, tp := newTestTracerProvider(t)
	tracer := tp.Tracer(ServiceTracerName)
	svc := &dbService{tracer: tracer}

	ctx := context.Background()
	spanName := "dbService.TestOperation"

	_, span := svc.startSpan(ctx, spanName,
		trace.WithAttributes(otel.AttrRegistryName.String("test-registry")),
	)
	span.End()

	// Verify span was recorded with correct name
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, spanName, spans[0].Name)

	// Verify db.system attribute was automatically added (db-specific behavior)
	var hasDBSystem, hasRegistryName bool
	for _, attr := range spans[0].Attributes {
		if attr.Key == "db.system" && attr.Value.AsString() == "postgresql" {
			hasDBSystem = true
		}
		if attr.Key == "registry.name" && attr.Value.AsString() == "test-registry" {
			hasRegistryName = true
		}
	}
	assert.True(t, hasDBSystem, "db service spans should have db.system attribute")
	assert.True(t, hasRegistryName, "should have custom attribute passed via options")
}

// TestDBServiceStartSpan_NilTracer verifies graceful degradation when tracer is nil.
func TestDBServiceStartSpan_NilTracer(t *testing.T) {
	t.Parallel()

	svc := &dbService{tracer: nil}
	ctx := context.Background()

	resultCtx, span := svc.startSpan(ctx, "test.operation")

	require.NotNil(t, resultCtx)
	require.NotNil(t, span)
	assert.False(t, span.SpanContext().IsValid(), "nil tracer should return no-op span")
	assert.NotPanics(t, func() { span.End() })
}

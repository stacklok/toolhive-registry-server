package database

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// newTestTracerProvider creates a tracer provider with in-memory exporter for testing.
// The provider is automatically shut down when the test completes.
func newTestTracerProvider(t *testing.T) (*tracetest.InMemoryExporter, trace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return exporter, tp
}

func TestStartSpan_NilTracer(t *testing.T) {
	t.Parallel()

	svc := &dbService{tracer: nil}
	ctx := context.Background()

	resultCtx, span := svc.startSpan(ctx, "test.operation")

	// Should return valid context and no-op span without panicking
	require.NotNil(t, resultCtx)
	require.NotNil(t, span)
	assert.False(t, span.SpanContext().IsValid(), "nil tracer should return no-op span")

	// End should not panic
	assert.NotPanics(t, func() { span.End() })
}

func TestStartSpan_ValidTracer(t *testing.T) {
	t.Parallel()

	exporter, tp := newTestTracerProvider(t)
	tracer := tp.Tracer(ServiceTracerName)
	svc := &dbService{tracer: tracer}

	ctx := context.Background()
	spanName := "dbService.TestOperation"

	resultCtx, span := svc.startSpan(ctx, spanName,
		trace.WithAttributes(AttrRegistryName.String("test-registry")),
	)

	require.NotNil(t, resultCtx)
	require.NotNil(t, span)
	assert.True(t, span.SpanContext().IsValid())

	span.End()

	// Verify span was recorded with correct name and attributes
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, spanName, spans[0].Name)

	// Verify db.system attribute was automatically added
	var hasDBSystem, hasRegistryName bool
	for _, attr := range spans[0].Attributes {
		if attr.Key == "db.system" && attr.Value.AsString() == "postgresql" {
			hasDBSystem = true
		}
		if attr.Key == "registry.name" && attr.Value.AsString() == "test-registry" {
			hasRegistryName = true
		}
	}
	assert.True(t, hasDBSystem, "should have db.system attribute")
	assert.True(t, hasRegistryName, "should have custom attribute")
}

func TestRecordError_NilSafety(t *testing.T) {
	t.Parallel()

	testErr := errors.New("test error")

	// All nil combinations should not panic
	assert.NotPanics(t, func() { recordError(nil, testErr) }, "nil span should not panic")
	assert.NotPanics(t, func() { recordError(nil, nil) }, "both nil should not panic")

	// Nil error with valid span should not record error
	exporter, tp := newTestTracerProvider(t)
	tracer := tp.Tracer(ServiceTracerName)
	_, span := tracer.Start(context.Background(), "test")

	recordError(span, nil)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Unset, spans[0].Status.Code, "nil error should not set error status")
	assert.Empty(t, spans[0].Events, "nil error should not record events")
}

func TestRecordError_RecordsErrorCorrectly(t *testing.T) {
	t.Parallel()

	exporter, tp := newTestTracerProvider(t)
	tracer := tp.Tracer(ServiceTracerName)
	_, span := tracer.Start(context.Background(), "test.operation")

	testErr := errors.New("database connection failed")
	recordError(span, testErr)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Verify error status
	assert.Equal(t, codes.Error, spans[0].Status.Code)
	assert.Equal(t, testErr.Error(), spans[0].Status.Description)

	// Verify exception event was recorded
	var hasException bool
	for _, event := range spans[0].Events {
		if event.Name == "exception" {
			hasException = true
			break
		}
	}
	assert.True(t, hasException, "should record exception event")
}

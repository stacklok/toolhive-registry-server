package otel

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
func newTestTracerProvider(t *testing.T) (*tracetest.InMemoryExporter, trace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return exporter, tp
}

func TestStartSpan_NilTracer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	resultCtx, span := StartSpan(ctx, nil, "test.operation")

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
	tracer := tp.Tracer("test-tracer")

	ctx := context.Background()
	spanName := "test.TestOperation"

	resultCtx, span := StartSpan(ctx, tracer, spanName,
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

	// Verify custom attribute was added
	var hasRegistryName bool
	for _, attr := range spans[0].Attributes {
		if attr.Key == "registry.name" && attr.Value.AsString() == "test-registry" {
			hasRegistryName = true
		}
	}
	assert.True(t, hasRegistryName, "should have custom attribute")
}

func TestRecordError_NilSafety(t *testing.T) {
	t.Parallel()

	testErr := errors.New("test error")

	// All nil combinations should not panic
	assert.NotPanics(t, func() { RecordError(nil, testErr) }, "nil span should not panic")
	assert.NotPanics(t, func() { RecordError(nil, nil) }, "both nil should not panic")

	// Nil error with valid span should not record error
	exporter, tp := newTestTracerProvider(t)
	tracer := tp.Tracer("test-tracer")
	_, span := tracer.Start(context.Background(), "test")

	RecordError(span, nil)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Unset, spans[0].Status.Code, "nil error should not set error status")
	assert.Empty(t, spans[0].Events, "nil error should not record events")
}

func TestRecordError_RecordsErrorCorrectly(t *testing.T) {
	t.Parallel()

	exporter, tp := newTestTracerProvider(t)
	tracer := tp.Tracer("test-tracer")
	_, span := tracer.Start(context.Background(), "test.operation")

	testErr := errors.New("database connection failed")
	RecordError(span, testErr)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Verify error status uses generic message
	assert.Equal(t, codes.Error, spans[0].Status.Code)
	assert.Equal(t, "operation failed", spans[0].Status.Description)

	// Verify exception event was recorded with actual error
	var hasException bool
	for _, event := range spans[0].Events {
		if event.Name == "exception" {
			hasException = true
			break
		}
	}
	assert.True(t, hasException, "should record exception event")
}

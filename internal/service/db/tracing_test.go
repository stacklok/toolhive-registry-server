package database

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestStartSpan_NilTracer(t *testing.T) {
	t.Parallel()

	// Create a dbService with nil tracer
	svc := &dbService{
		pool:   nil,
		tracer: nil,
	}

	ctx := context.Background()
	spanName := "test.operation"

	// Call startSpan
	resultCtx, span := svc.startSpan(ctx, spanName)

	// Verify the returned context is valid (not nil)
	require.NotNil(t, resultCtx, "returned context should not be nil")

	// Verify the span is not nil (should be a no-op span from context)
	require.NotNil(t, span, "returned span should not be nil")

	// Verify the span is a no-op span (SpanContext should be invalid)
	// No-op spans from trace.SpanFromContext have invalid span context
	spanCtx := span.SpanContext()
	assert.False(t, spanCtx.IsValid(), "no-op span should have invalid span context")

	// Verify that calling End() doesn't panic
	assert.NotPanics(t, func() {
		span.End()
	}, "calling End() on no-op span should not panic")
}

func TestStartSpan_ValidTracer(t *testing.T) {
	t.Parallel()

	// Create an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	defer func() {
		err := tp.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	// Create a tracer from the provider
	tracer := tp.Tracer(ServiceTracerName)

	// Create a dbService with the valid tracer
	svc := &dbService{
		pool:   nil,
		tracer: tracer,
	}

	ctx := context.Background()
	spanName := "dbService.TestOperation"

	// Call startSpan
	resultCtx, span := svc.startSpan(ctx, spanName)
	defer span.End()

	// Verify the returned context is valid
	require.NotNil(t, resultCtx, "returned context should not be nil")

	// Verify the span is valid
	require.NotNil(t, span, "returned span should not be nil")
	spanCtx := span.SpanContext()
	assert.True(t, spanCtx.IsValid(), "span should have valid span context")
	assert.True(t, spanCtx.HasTraceID(), "span should have a trace ID")
	assert.True(t, spanCtx.HasSpanID(), "span should have a span ID")

	// End the span to flush it to the exporter
	span.End()

	// Verify the span was recorded with the correct name
	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "should have exactly one span recorded")
	assert.Equal(t, spanName, spans[0].Name, "span name should match")
}

func TestStartSpan_WithOptions(t *testing.T) {
	t.Parallel()

	// Create an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	defer func() {
		err := tp.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	tracer := tp.Tracer(ServiceTracerName)
	svc := &dbService{
		pool:   nil,
		tracer: tracer,
	}

	ctx := context.Background()
	spanName := "dbService.TestWithOptions"

	// Call startSpan with additional options
	resultCtx, span := svc.startSpan(ctx, spanName,
		trace.WithAttributes(attribute.String("test.key", "test.value")),
	)
	defer span.End()

	require.NotNil(t, resultCtx)
	require.NotNil(t, span)

	// End and check the recorded span
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, spanName, spans[0].Name)

	// Check that the attribute was set
	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "test.key" && attr.Value.AsString() == "test.value" {
			found = true
			break
		}
	}
	assert.True(t, found, "should have the test attribute set")
}

func TestRecordError_NilSpan(t *testing.T) {
	t.Parallel()

	testErr := errors.New("test error")

	// Should not panic when span is nil
	assert.NotPanics(t, func() {
		recordError(nil, testErr)
	}, "recordError should not panic with nil span")
}

func TestRecordError_NilError(t *testing.T) {
	t.Parallel()

	// Create an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	defer func() {
		err := tp.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	tracer := tp.Tracer(ServiceTracerName)
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test.operation")

	// Call recordError with nil error
	recordError(span, nil)

	span.End()

	// Verify no error was recorded on the span
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Status should not be error (should be Unset by default)
	assert.Equal(t, codes.Unset, spans[0].Status.Code, "status should be Unset when error is nil")

	// No events (errors are recorded as events)
	assert.Empty(t, spans[0].Events, "should have no events when error is nil")
}

func TestRecordError_BothNil(t *testing.T) {
	t.Parallel()

	// Should not panic when both are nil
	assert.NotPanics(t, func() {
		recordError(nil, nil)
	}, "recordError should not panic with both span and error nil")
}

func TestRecordError_RecordsErrorCorrectly(t *testing.T) {
	t.Parallel()

	// Create an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	defer func() {
		err := tp.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	tracer := tp.Tracer(ServiceTracerName)
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test.operation")

	testErr := errors.New("database connection failed")

	// Call recordError with a valid error
	recordError(span, testErr)

	span.End()

	// Verify the error was recorded on the span
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Check that the status was set to Error
	assert.Equal(t, codes.Error, spans[0].Status.Code, "status should be Error")
	assert.Equal(t, testErr.Error(), spans[0].Status.Description, "status description should match error message")

	// Check that an error event was recorded
	require.NotEmpty(t, spans[0].Events, "should have at least one event")

	// Find the exception event
	foundException := false
	for _, event := range spans[0].Events {
		if event.Name == "exception" {
			foundException = true
			// Check for exception.message attribute
			for _, attr := range event.Attributes {
				if attr.Key == "exception.message" {
					assert.Equal(t, testErr.Error(), attr.Value.AsString())
				}
			}
		}
	}
	assert.True(t, foundException, "should have an exception event recorded")
}

func TestAttributeKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		attrKey      attribute.Key
		expectedName string
	}{
		{
			name:         "AttrRegistryName has correct key name",
			attrKey:      AttrRegistryName,
			expectedName: "registry.name",
		},
		{
			name:         "AttrServerName has correct key name",
			attrKey:      AttrServerName,
			expectedName: "server.name",
		},
		{
			name:         "AttrServerVersion has correct key name",
			attrKey:      AttrServerVersion,
			expectedName: "server.version",
		},
		{
			name:         "AttrPageSize has correct key name",
			attrKey:      AttrPageSize,
			expectedName: "pagination.limit",
		},
		{
			name:         "AttrResultCount has correct key name",
			attrKey:      AttrResultCount,
			expectedName: "result.count",
		},
		{
			name:         "AttrHasCursor has correct key name",
			attrKey:      AttrHasCursor,
			expectedName: "pagination.has_cursor",
		},
		{
			name:         "AttrSearchQuery has correct key name",
			attrKey:      AttrSearchQuery,
			expectedName: "query.search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expectedName, string(tt.attrKey), "attribute key name mismatch")
		})
	}
}

func TestServiceTracerName(t *testing.T) {
	t.Parallel()

	expectedName := "github.com/stacklok/toolhive-registry-server/service/db"
	assert.Equal(t, expectedName, ServiceTracerName, "tracer name should match expected value")
}

func TestDBSystemPostgres(t *testing.T) {
	t.Parallel()

	// Verify DBSystemPostgres is set to the PostgreSQL semantic convention
	// The semconv package defines DBSystemPostgreSQL as "postgresql"
	assert.Equal(t, "db.system", string(DBSystemPostgres.Key), "attribute key should be db.system")
	assert.Equal(t, "postgresql", DBSystemPostgres.Value.AsString(), "attribute value should be postgresql")
}

func TestAttributeKeys_UsageWithSpan(t *testing.T) {
	t.Parallel()

	// Create an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	defer func() {
		err := tp.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	tracer := tp.Tracer(ServiceTracerName)
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test.operation")

	// Set attributes using the defined keys
	span.SetAttributes(
		AttrRegistryName.String("test-registry"),
		AttrServerName.String("test-server"),
		AttrServerVersion.String("1.0.0"),
		AttrPageSize.Int(50),
		AttrResultCount.Int(10),
		AttrHasCursor.Bool(true),
		AttrSearchQuery.String("search term"),
	)

	span.End()

	// Verify all attributes were set correctly
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Build a map of attributes for easier verification
	attrMap := make(map[string]interface{})
	for _, attr := range spans[0].Attributes {
		//nolint:exhaustive // Only testing STRING, INT64, and BOOL types in this test
		switch attr.Value.Type() {
		case attribute.STRING:
			attrMap[string(attr.Key)] = attr.Value.AsString()
		case attribute.INT64:
			attrMap[string(attr.Key)] = attr.Value.AsInt64()
		case attribute.BOOL:
			attrMap[string(attr.Key)] = attr.Value.AsBool()
		}
	}

	assert.Equal(t, "test-registry", attrMap["registry.name"])
	assert.Equal(t, "test-server", attrMap["server.name"])
	assert.Equal(t, "1.0.0", attrMap["server.version"])
	assert.Equal(t, int64(50), attrMap["pagination.limit"])
	assert.Equal(t, int64(10), attrMap["result.count"])
	assert.Equal(t, true, attrMap["pagination.has_cursor"])
	assert.Equal(t, "search term", attrMap["query.search"])
}

func TestStartSpan_ContextPropagation(t *testing.T) {
	t.Parallel()

	// Create an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	defer func() {
		err := tp.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	tracer := tp.Tracer(ServiceTracerName)
	svc := &dbService{
		pool:   nil,
		tracer: tracer,
	}

	// Start a parent span
	ctx := context.Background()
	ctx, parentSpan := tracer.Start(ctx, "parent.operation")

	// Start a child span using startSpan
	childCtx, childSpan := svc.startSpan(ctx, "child.operation")

	// Verify child has the parent's trace ID
	parentSpanCtx := parentSpan.SpanContext()
	childSpanCtx := childSpan.SpanContext()

	assert.Equal(t, parentSpanCtx.TraceID(), childSpanCtx.TraceID(),
		"child span should have the same trace ID as parent")
	assert.NotEqual(t, parentSpanCtx.SpanID(), childSpanCtx.SpanID(),
		"child span should have different span ID than parent")

	childSpan.End()
	parentSpan.End()

	// Verify both spans were recorded
	spans := exporter.GetSpans()
	require.Len(t, spans, 2, "should have two spans recorded")

	// Verify the child span correctly references the parent
	var foundChild, foundParent bool
	for _, s := range spans {
		if s.Name == "child.operation" {
			foundChild = true
			assert.Equal(t, parentSpanCtx.SpanID(), s.Parent.SpanID(),
				"child span should have parent span ID in parent reference")
		}
		if s.Name == "parent.operation" {
			foundParent = true
		}
	}
	assert.True(t, foundChild, "should find child span")
	assert.True(t, foundParent, "should find parent span")

	// Verify the returned context contains the child span
	spanFromCtx := trace.SpanFromContext(childCtx)
	assert.Equal(t, childSpanCtx, spanFromCtx.SpanContext(),
		"context should contain the child span")
}

func TestRecordError_MultipleErrors(t *testing.T) {
	t.Parallel()

	// Create an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	defer func() {
		err := tp.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	tracer := tp.Tracer(ServiceTracerName)
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test.operation")

	// Record multiple errors
	err1 := errors.New("first error")
	err2 := errors.New("second error")

	recordError(span, err1)
	recordError(span, err2)

	span.End()

	// Verify multiple events were recorded
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Count exception events
	exceptionCount := 0
	for _, event := range spans[0].Events {
		if event.Name == "exception" {
			exceptionCount++
		}
	}
	assert.Equal(t, 2, exceptionCount, "should have two exception events")

	// Status should reflect the last error
	assert.Equal(t, codes.Error, spans[0].Status.Code)
	assert.Equal(t, err2.Error(), spans[0].Status.Description,
		"status description should match the last error")
}

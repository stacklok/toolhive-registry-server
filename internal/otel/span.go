// Package otel provides OpenTelemetry instrumentation utilities for the registry server.
package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Common attribute keys for business context used across the application.
// Using shared keys ensures consistent attribute naming in traces.
const (
	AttrRegistryName  = attribute.Key("registry.name")
	AttrRegistryType  = attribute.Key("registry.type")
	AttrServerName    = attribute.Key("server.name")
	AttrServerVersion = attribute.Key("server.version")
	AttrPageSize      = attribute.Key("pagination.limit")
	AttrResultCount   = attribute.Key("result.count")
	AttrHasCursor     = attribute.Key("pagination.has_cursor")
)

// StartSpan starts a new span if the tracer is non-nil, otherwise returns a no-op span.
// This provides graceful degradation when tracing is disabled.
func StartSpan(
	ctx context.Context,
	tracer trace.Tracer,
	name string,
	opts ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, name, opts...)
}

// RecordError records an error on a span and sets the span status to error.
// It safely handles nil spans and nil errors.
// Note: The status description is intentionally generic to prevent sensitive
// information (e.g., SQL queries, connection strings) from appearing in trace
// status. The full error details are still available via span events for debugging.
func RecordError(span trace.Span, err error) {
	if err != nil && span != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "operation failed")
	}
}

// Package database provides a database-backed implementation of the RegistryService interface
package database

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// ServiceTracerName is the name used for the database service tracer
	ServiceTracerName = "github.com/stacklok/toolhive-registry-server/service/db"
)

// Custom attribute keys for business context
const (
	AttrRegistryName  = attribute.Key("registry.name")
	AttrServerName    = attribute.Key("server.name")
	AttrServerVersion = attribute.Key("server.version")
	AttrPageSize      = attribute.Key("pagination.limit")
	AttrResultCount   = attribute.Key("result.count")
	AttrHasCursor     = attribute.Key("pagination.has_cursor")
	AttrSearchQuery   = attribute.Key("query.search")
)

// Database semantic convention attributes
var (
	// DBSystemPostgres is the database system attribute for PostgreSQL
	DBSystemPostgres = semconv.DBSystemPostgreSQL
)

// startSpan starts a new span for database operations.
// If the tracer is nil, it returns a no-op span from the context.
func (s *dbService) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if s.tracer == nil {
		// Return a no-op span from context
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, name, opts...)
}

// recordError records an error on a span and sets the span status to error.
// It safely handles nil spans and nil errors.
func recordError(span trace.Span, err error) {
	if err != nil && span != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

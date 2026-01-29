// Package database provides a database-backed implementation of the RegistryService interface
package database

import (
	"context"

	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/otel"
)

const (
	// ServiceTracerName is the name used for the database service tracer
	ServiceTracerName = "github.com/stacklok/toolhive-registry-server/service/db"
)

// Database semantic convention attributes
var (
	// DBSystemPostgres is the database system attribute for PostgreSQL
	DBSystemPostgres = semconv.DBSystemPostgreSQL
)

// startSpan starts a new span for database operations.
// If the tracer is nil, it returns a no-op span from the context.
// All database spans automatically include the db.system attribute per OTEL semantic conventions.
func (s *dbService) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	// Prepend db.system attribute to ensure all database spans have it per OTEL semantic conventions
	opts = append([]trace.SpanStartOption{trace.WithAttributes(DBSystemPostgres)}, opts...)
	return otel.StartSpan(ctx, s.tracer, name, opts...)
}

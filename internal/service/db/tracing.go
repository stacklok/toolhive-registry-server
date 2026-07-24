// Package database provides a database-backed implementation of the RegistryService interface
package database

import (
	"context"
	"time"

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

// startSpan starts a new span for database operations and returns the span
// alongside a done func that both ends the span and records the operation's
// duration into stacklok.registry.db.query.duration (operation=name). This is
// the single seam every DB method passes through, so instrumenting it here
// covers all queries. Callers use `defer done()` in place of the previous
// `defer span.End()`; the returned span is still available for otel.RecordError.
//
// If the tracer is nil, the span is a no-op from the context. All database
// spans automatically include the db.system attribute per OTEL semantic
// conventions. name is a bounded query-name literal (never raw SQL), safe as
// the operation label.
func (s *dbService) startSpan(
	ctx context.Context, name string, opts ...trace.SpanStartOption,
) (context.Context, trace.Span, func()) {
	// Prepend db.system attribute to ensure all database spans have it per OTEL semantic conventions
	opts = append([]trace.SpanStartOption{trace.WithAttributes(DBSystemPostgres)}, opts...)
	spanCtx, span := otel.StartSpan(ctx, s.tracer, name, opts...)
	start := time.Now()
	done := func() {
		s.dbMetrics.RecordQueryDuration(ctx, name, time.Since(start))
		span.End()
	}
	return spanCtx, span, done
}

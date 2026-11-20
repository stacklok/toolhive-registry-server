// Package storage provides factory functions for creating storage-dependent components.
// It implements the Abstract Factory pattern so the library adapters and the
// registry service share one storage backend and connection pool.
package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

//go:generate mockgen -destination=mocks/mock_factory.go -package=mocks -source=factory.go Factory

// Catalog groups the pkg/registry adapters the server runs on. All of them
// borrow the factory's pool; the factory's Cleanup closes it.
type Catalog struct {
	Pool        *pgxpool.Pool
	Definitions *postgres.Definitions
	// Entries and Jobs carry the entry write hook, if any; in claims mode it
	// keeps registry_entry.claims in step with library writes.
	Entries *postgres.Entries
	Jobs    *postgres.Jobs
}

// Factory creates storage-dependent components as a family and manages the
// lifecycle of storage resources (e.g., database connections).
type Factory interface {
	// CreateCatalog opens the library adapters. hook, when not nil, runs
	// around every entry write (claims mode installs its ClaimsHook; Cedar
	// mode passes nil).
	CreateCatalog(ctx context.Context, hook postgres.EntryWriteHook) (*Catalog, error)

	// CreateRegistryService creates the admin registry service. publisher
	// performs managed publish and delete writes; opts add optional wiring
	// such as database.WithSourcesChanged.
	CreateRegistryService(
		ctx context.Context, publisher persistence.ManagedPublisher, opts ...database.Option,
	) (service.RegistryService, error)

	// Cleanup releases any resources held by this factory.
	// For database factories, this closes the connection pool.
	// Should be called when the application shuts down.
	Cleanup()
}

// FactoryOption is a functional option for configuring the storage factory
type FactoryOption func(*storageFactoryOptions)

// storageFactoryOptions holds configuration options for the storage factory
type storageFactoryOptions struct {
	tracerProvider trace.TracerProvider
}

// WithTracerProvider sets the OpenTelemetry tracer provider for the storage factory.
// If set, the factory will create a tracer and pass it to storage components
// that support tracing (e.g., database service).
func WithTracerProvider(tp trace.TracerProvider) FactoryOption {
	return func(opts *storageFactoryOptions) {
		opts.tracerProvider = tp
	}
}

// NewStorageFactory creates a database-backed storage factory.
// Database configuration is required.
func NewStorageFactory(ctx context.Context, cfg *config.Config, opts ...FactoryOption) (Factory, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if cfg.Database == nil {
		return nil, fmt.Errorf("database configuration is required")
	}

	// Apply options
	options := &storageFactoryOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Build database factory options
	var dbOpts []DatabaseFactoryOption
	if options.tracerProvider != nil {
		tracer := options.tracerProvider.Tracer(database.ServiceTracerName)
		dbOpts = append(dbOpts, WithTracer(tracer))
	}
	return NewDatabaseFactory(ctx, cfg, dbOpts...)
}

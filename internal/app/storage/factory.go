// Package storage provides factory functions for creating storage-dependent components.
// It implements the Abstract Factory pattern to ensure related components (state service,
// sync writer, registry service) are created with compatible storage backends.
package storage

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/internal/sync/state"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

//go:generate mockgen -destination=mocks/mock_factory.go -package=mocks -source=factory.go Factory

// Factory creates storage-dependent components as a family.
// Implementations ensure all components are compatible with each other.
//
// The factory encapsulates the creation of:
// - RegistryStateService: Tracks sync status
// - SyncWriter: Writes synced registry data
// - RegistryService: Serves registry API requests
//
// It also manages the lifecycle of storage resources (e.g., database connections).
type Factory interface {
	// CreateStateService creates a state service for sync status tracking.
	CreateStateService(ctx context.Context) (state.RegistryStateService, error)

	// CreateSyncWriter creates a writer for storing synced registry data.
	CreateSyncWriter(ctx context.Context) (writer.SyncWriter, error)

	// CreateRegistryService creates the main registry service.
	CreateRegistryService(ctx context.Context) (service.RegistryService, error)

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

// NewStorageFactory creates a storage factory based on the configured storage type.
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

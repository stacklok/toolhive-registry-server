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
// Implementations ensure all components are compatible with each other
// (e.g., all use database or all use file storage).
//
// The factory encapsulates the creation of:
// - RegistryStateService: Tracks sync status
// - SyncWriter: Writes synced registry data
// - RegistryService: Serves registry API requests
//
// It also manages the lifecycle of storage resources (e.g., database connections).
type Factory interface {
	// CreateStateService creates a state service for sync status tracking.
	// The returned service uses storage appropriate for this factory's type
	// (file-based or database-backed).
	CreateStateService(ctx context.Context) (state.RegistryStateService, error)

	// CreateSyncWriter creates a writer for storing synced registry data.
	// The returned writer uses storage appropriate for this factory's type.
	CreateSyncWriter(ctx context.Context) (writer.SyncWriter, error)

	// CreateRegistryService creates the main registry service.
	// The returned service uses storage appropriate for this factory's type.
	CreateRegistryService(ctx context.Context) (service.RegistryService, error)

	// Cleanup releases any resources held by this factory.
	// For database factories, this closes the connection pool.
	// For file factories, this is a no-op.
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
// Returns a FileFactory for file-based storage or a DatabaseFactory for database storage.
func NewStorageFactory(ctx context.Context, cfg *config.Config, dataDir string, opts ...FactoryOption) (Factory, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Apply options
	options := &storageFactoryOptions{}
	for _, opt := range opts {
		opt(options)
	}

	switch cfg.GetStorageType() {
	case config.StorageTypeDatabase:
		// Build database factory options
		var dbOpts []DatabaseFactoryOption
		if options.tracerProvider != nil {
			tracer := options.tracerProvider.Tracer(database.ServiceTracerName)
			dbOpts = append(dbOpts, WithTracer(tracer))
		}
		return NewDatabaseFactory(ctx, cfg, dbOpts...)
	case config.StorageTypeFile:
		return NewFileFactory(cfg, dataDir)
	default:
		return nil, fmt.Errorf("unknown storage type: %s", cfg.GetStorageType())
	}
}

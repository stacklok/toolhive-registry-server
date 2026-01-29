package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/internal/sync/state"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

// DatabaseFactory creates database-backed storage components.
// All components created by this factory use PostgreSQL for persistence.
type DatabaseFactory struct {
	config *config.Config
	pool   *pgxpool.Pool
	tracer trace.Tracer
}

var _ Factory = (*DatabaseFactory)(nil)

// DatabaseFactoryOption is a functional option for configuring the DatabaseFactory
type DatabaseFactoryOption func(*DatabaseFactory)

// WithTracer sets the OpenTelemetry tracer for the database service.
// If not set, tracing will be disabled (no-op).
func WithTracer(tracer trace.Tracer) DatabaseFactoryOption {
	return func(f *DatabaseFactory) {
		f.tracer = tracer
	}
}

// NewDatabaseFactory creates a new database-backed storage factory.
// It establishes a connection pool to the configured PostgreSQL database.
func NewDatabaseFactory(ctx context.Context, cfg *config.Config, opts ...DatabaseFactoryOption) (*DatabaseFactory, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if cfg.Database == nil {
		return nil, fmt.Errorf("database configuration is required for database storage type")
	}

	slog.Info("Creating database-backed storage factory")

	pool, err := buildDatabaseConnectionPool(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection pool: %w", err)
	}

	factory := &DatabaseFactory{
		config: cfg,
		pool:   pool,
	}

	// Apply options
	for _, opt := range opts {
		opt(factory)
	}

	return factory, nil
}

// CreateStateService creates a database-backed state service for sync status tracking.
func (d *DatabaseFactory) CreateStateService(_ context.Context) (state.RegistryStateService, error) {
	slog.Debug("Creating database-backed state service")
	return state.NewStateService(d.config, nil, d.pool)
}

// CreateSyncWriter creates a database-backed sync writer for storing registry data.
func (d *DatabaseFactory) CreateSyncWriter(_ context.Context) (writer.SyncWriter, error) {
	slog.Debug("Creating database-backed sync writer")
	return writer.NewSyncWriter(d.config, nil, d.pool)
}

// CreateRegistryService creates a database-backed registry service.
// The service reads and writes registry data directly to PostgreSQL.
func (d *DatabaseFactory) CreateRegistryService(_ context.Context) (service.RegistryService, error) {
	slog.Debug("Creating database-backed registry service")

	// Build database service options
	opts := []database.Option{
		database.WithConnectionPool(d.pool),
	}

	// Add tracer if configured
	if d.tracer != nil {
		opts = append(opts, database.WithTracer(d.tracer))
		slog.Debug("Database service tracing enabled")
	}

	return database.New(opts...)
}

// Cleanup releases resources held by the database factory.
// This closes the database connection pool and any active connections.
func (d *DatabaseFactory) Cleanup() {
	if d.pool != nil {
		slog.Info("Closing database connection pool")
		d.pool.Close()
	}
}

// buildDatabaseConnectionPool creates a database connection pool with proper configuration.
func buildDatabaseConnectionPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	// Get connection string from config (for application user)
	connStr := cfg.Database.GetConnectionString()

	// Parse connection string into config
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database connection string: %w", err)
	}

	// Configure pool settings from config
	if cfg.Database.MaxOpenConns > 0 {
		poolConfig.MaxConns = int32(cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns > 0 {
		poolConfig.MinConns = int32(cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime != "" {
		lifetime, err := time.ParseDuration(cfg.Database.ConnMaxLifetime)
		if err != nil {
			return nil, fmt.Errorf("failed to parse connMaxLifetime: %w", err)
		}
		poolConfig.MaxConnLifetime = lifetime
	}

	// Register custom type codecs after connection is established
	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// Register array codecs for all custom enum types
		return registerCustomArrayCodecs(ctx, conn)
	}

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection pool: %w", err)
	}

	slog.Info("Database connection pool created successfully")
	return pool, nil
}

// registerCustomArrayCodecs registers codecs for all custom enum array types.
// This is needed because pgx doesn't automatically know how to encode Go slices
// of custom enum types into PostgreSQL array types.
func registerCustomArrayCodecs(ctx context.Context, conn *pgx.Conn) error {
	// List of enum types that need array codec registration
	enumTypes := []string{"registry_type", "sync_status", "icon_theme", "creation_type"}

	for _, enumName := range enumTypes {
		// Get the OID for the enum from the database
		var enumOID uint32
		err := conn.QueryRow(ctx, "SELECT oid FROM pg_type WHERE typname = $1", enumName).Scan(&enumOID)
		if err != nil {
			return fmt.Errorf("failed to get %s OID: %w", enumName, err)
		}

		// Get the OID for the array type (PostgreSQL prefixes array types with _)
		var arrayOID uint32
		err = conn.QueryRow(ctx, "SELECT oid FROM pg_type WHERE typname = $1", "_"+enumName).Scan(&arrayOID)
		if err != nil {
			return fmt.Errorf("failed to get %s[] array OID: %w", enumName, err)
		}

		// Register the array codec with proper element type codec
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name: enumName + "[]",
			OID:  arrayOID,
			Codec: &pgtype.ArrayCodec{
				ElementType: &pgtype.Type{
					Name:  enumName,
					OID:   enumOID,
					Codec: pgtype.TextCodec{},
				},
			},
		})
	}

	return nil
}

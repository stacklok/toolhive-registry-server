package storage

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	corepg "github.com/stacklok/toolhive-core/postgres"
	"go.opentelemetry.io/otel/trace"

	schemadb "github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
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
// It establishes a connection pool to the configured PostgreSQL database via
// the shared toolhive-core postgres package. Dynamic authentication (e.g. AWS
// RDS IAM) is wired automatically by postgres.NewPool when configured; this
// factory only supplies the AfterConnect hook that registers the schema's
// custom enum-array codecs.
func NewDatabaseFactory(ctx context.Context, cfg *config.Config, opts ...DatabaseFactoryOption) (*DatabaseFactory, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if cfg.Database == nil {
		return nil, fmt.Errorf("database configuration is required")
	}

	slog.Info("Creating database-backed storage factory")

	corePgCfg, err := cfg.Database.ToCorePostgresConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build database configuration: %w", err)
	}

	pool, err := corepg.NewPool(ctx, corePgCfg,
		corepg.WithAfterConnect(schemadb.RegisterEnumArrayCodecs),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection pool: %w", err)
	}

	// Emit periodic pool statistics at DEBUG using the package defaults
	// (60s cadence, slog.Default()).
	corepg.StartPoolStatsLogger(ctx, pool, nil, 0)

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

// CreateCatalog opens the library adapters over the factory's pool. It
// checks the schema version and runs a pending latest-pointer repair.
func (d *DatabaseFactory) CreateCatalog(ctx context.Context, hook postgres.EntryWriteHook) (*Catalog, error) {
	maxMeta := d.config.Database.GetMaxMetaSize()
	var opts []postgres.Option
	if hook != nil {
		opts = append(opts, postgres.WithEntryWriteHook(hook))
	}
	definitions, err := postgres.NewDefinitions(d.pool)
	if err != nil {
		return nil, fmt.Errorf("failed to open definitions: %w", err)
	}
	entries, err := postgres.NewEntries(ctx, d.pool, maxMeta, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to open entries: %w", err)
	}
	jobs, err := postgres.NewJobs(d.pool, maxMeta, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to open sync jobs: %w", err)
	}
	return &Catalog{Pool: d.pool, Definitions: definitions, Entries: entries, Jobs: jobs}, nil
}

// CreateRegistryService creates a database-backed registry service.
func (d *DatabaseFactory) CreateRegistryService(
	_ context.Context, publisher persistence.ManagedPublisher, extra ...database.Option,
) (service.RegistryService, error) {
	slog.Debug("Creating database-backed registry service")

	opts := []database.Option{
		database.WithConnectionPool(d.pool),
		database.WithMaxMetaSize(d.config.Database.GetMaxMetaSize()),
		database.WithPublisher(publisher),
	}
	if d.tracer != nil {
		opts = append(opts, database.WithTracer(d.tracer))
		slog.Debug("Database service tracing enabled")
	}

	// When authz is not configured, skip per-entry claims filtering and
	// registry-level claims gating (auth-only mode).
	if d.config.Auth == nil || d.config.Auth.Authz == nil {
		if d.config.AuthorizationMode() == config.AuthorizationModeClaims {
			slog.Warn("Authorization not configured, per-entry claims filtering disabled (auth-only mode)")
		}
		opts = append(opts, database.WithSkipAuthz())
	}

	return database.New(append(opts, extra...)...)
}

// Cleanup releases resources held by the database factory.
// This closes the database connection pool and any active connections.
func (d *DatabaseFactory) Cleanup() {
	if d.pool != nil {
		slog.Info("Closing database connection pool")
		d.pool.Close()
	}
}

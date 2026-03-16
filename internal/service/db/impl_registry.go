package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// ListRegistries returns all configured registries
func (s *dbService) ListRegistries(ctx context.Context) ([]service.RegistryInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListRegistries")
	defer span.End()
	start := time.Now()

	// Begin a read-only transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	registries, err := querier.ListRegistries(ctx)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}

	result := make([]service.RegistryInfo, 0, len(registries))
	for _, reg := range registries {
		sources, err := querier.ListRegistrySources(ctx, reg.ID)
		if err != nil {
			otel.RecordError(span, err)
			return nil, fmt.Errorf("failed to list sources for registry %s: %w", reg.Name, err)
		}
		result = append(result, *registryToInfo(reg, sources))
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(result)))
	slog.DebugContext(ctx, "ListRegistries completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"count", len(result),
		"request_id", middleware.GetReqID(ctx))
	return result, nil
}

// GetRegistryByName returns a single registry by name
func (s *dbService) GetRegistryByName(ctx context.Context, name string) (*service.RegistryInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.GetRegistryByName")
	defer span.End()
	start := time.Now()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Begin a read-only transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	reg, err := querier.GetRegistryByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	sources, err := querier.ListRegistrySources(ctx, reg.ID)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to list sources for registry %s: %w", name, err)
	}

	slog.DebugContext(ctx, "GetRegistryByName completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"registry", name,
		"request_id", middleware.GetReqID(ctx))
	return registryToInfo(reg, sources), nil
}

// CreateRegistry creates a new lightweight registry (name + ordered sources).
func (s *dbService) CreateRegistry(
	ctx context.Context, name string, req *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.CreateRegistry")
	defer span.End()
	start := time.Now()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Validate configuration
	if err := service.ValidateRegistryConfig(req); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("%w: %v", service.ErrInvalidRegistryConfig, err)
	}

	// Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	// Check if registry already exists
	_, err = querier.GetRegistryByName(ctx, name)
	if err == nil {
		err = fmt.Errorf("%w: %s", service.ErrRegistryAlreadyExists, name)
		otel.RecordError(span, err)
		return nil, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to check registry existence: %w", err)
	}

	// Resolve source names to IDs
	sourceIDs, err := resolveSourceIDs(ctx, querier, req.Sources)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Insert the registry row
	now := time.Now()
	inserted, err := querier.UpsertAPIRegistry(ctx, sqlc.UpsertAPIRegistryParams{
		Name:      name,
		CreatedAt: &now,
		UpdatedAt: &now,
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to insert registry: %w", err)
	}

	// Link sources at their ordered positions
	if err := linkSources(ctx, querier, inserted.ID, sourceIDs); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to link sources: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Registry created",
		"duration_ms", time.Since(start).Milliseconds(),
		"name", name,
		"sources", req.Sources,
		"request_id", middleware.GetReqID(ctx))

	return newRegistryInfo(inserted, req.Sources), nil
}

// UpdateRegistry updates an existing lightweight registry.
func (s *dbService) UpdateRegistry(
	ctx context.Context, name string, req *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.UpdateRegistry")
	defer span.End()
	start := time.Now()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Validate configuration
	if err := service.ValidateRegistryConfig(req); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("%w: %v", service.ErrInvalidRegistryConfig, err)
	}

	// Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	// Look up existing API-created registry (returns error for not-found or CONFIG type)
	existing, err := getExistingAPIRegistry(ctx, querier, name)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Replace source links: resolve, unlink old, link new
	if err := replaceRegistrySources(ctx, querier, existing.ID, req.Sources); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Upsert registry row to bump updated_at. UpsertAPIRegistry only updates
	// updated_at on conflict — all other columns (claims, creation_type, created_at)
	// are preserved. The WHERE creation_type = 'API' guard prevents overwriting
	// CONFIG registries. Source links are the mutable part and are updated above.
	now := time.Now()
	upserted, err := querier.UpsertAPIRegistry(ctx, sqlc.UpsertAPIRegistryParams{
		Name:      name,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: &now,
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to update registry: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Registry updated",
		"duration_ms", time.Since(start).Milliseconds(),
		"name", name,
		"sources", req.Sources,
		"request_id", middleware.GetReqID(ctx))

	return newRegistryInfo(upserted, req.Sources), nil
}

// DeleteRegistry deletes a lightweight registry.
func (s *dbService) DeleteRegistry(ctx context.Context, name string) error {
	ctx, span := s.startSpan(ctx, "dbService.DeleteRegistry")
	defer span.End()
	start := time.Now()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	// Look up existing API-created registry (returns error for not-found or CONFIG type)
	if _, err := getExistingAPIRegistry(ctx, querier, name); err != nil {
		otel.RecordError(span, err)
		return err
	}

	// Delete the registry (cascades to registry_source junction).
	// DeleteAPIRegistry filters on creation_type='API' as defense-in-depth.
	if _, err := querier.DeleteAPIRegistry(ctx, name); err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to delete registry: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Registry deleted",
		"duration_ms", time.Since(start).Milliseconds(),
		"name", name,
		"request_id", middleware.GetReqID(ctx))

	return nil
}

// =============================================================================
// Helper functions for registry CRUD operations
// =============================================================================

// getExistingAPIRegistry looks up a registry by name and validates that it is
// API-created. It returns appropriate sentinel errors for not-found and CONFIG registries.
func getExistingAPIRegistry(
	ctx context.Context, querier *sqlc.Queries, name string,
) (sqlc.Registry, error) {
	existing, err := querier.GetRegistryByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Registry{}, fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
		}
		return sqlc.Registry{}, fmt.Errorf("failed to get registry: %w", err)
	}
	if existing.CreationType == sqlc.CreationTypeCONFIG {
		return sqlc.Registry{}, fmt.Errorf("%w: %s", service.ErrConfigRegistry, name)
	}
	return existing, nil
}

// replaceRegistrySources resolves source names, unlinks all current sources, and links the new set.
func replaceRegistrySources(
	ctx context.Context, querier *sqlc.Queries, registryID uuid.UUID, sourceNames []string,
) error {
	sourceIDs, err := resolveSourceIDs(ctx, querier, sourceNames)
	if err != nil {
		return err
	}

	currentSources, err := querier.ListRegistrySources(ctx, registryID)
	if err != nil {
		return fmt.Errorf("failed to list current sources: %w", err)
	}

	if err := unlinkAllSources(ctx, querier, registryID, currentSources); err != nil {
		return fmt.Errorf("failed to unlink sources: %w", err)
	}

	if err := linkSources(ctx, querier, registryID, sourceIDs); err != nil {
		return fmt.Errorf("failed to link sources: %w", err)
	}

	return nil
}

// newRegistryInfo constructs a service.RegistryInfo from a database Registry row
// and a list of source names.
func newRegistryInfo(reg sqlc.Registry, sourceNames []string) *service.RegistryInfo {
	info := &service.RegistryInfo{
		Name:         reg.Name,
		CreationType: service.CreationType(reg.CreationType),
		Sources:      sourceNames,
	}
	if reg.CreatedAt != nil {
		info.CreatedAt = *reg.CreatedAt
	}
	if reg.UpdatedAt != nil {
		info.UpdatedAt = *reg.UpdatedAt
	}
	return info
}

// registryToInfo converts a database Registry and its linked source rows into a service RegistryInfo.
func registryToInfo(reg sqlc.Registry, sources []sqlc.ListRegistrySourcesRow) *service.RegistryInfo {
	sourceNames := make([]string, len(sources))
	for i, src := range sources {
		sourceNames[i] = src.Name
	}
	return newRegistryInfo(reg, sourceNames)
}

// resolveSourceIDs resolves an ordered list of source names into their corresponding UUIDs.
func resolveSourceIDs(ctx context.Context, querier *sqlc.Queries, names []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(names))
	for _, name := range names {
		src, err := querier.GetSourceByName(ctx, name)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
			}
			return nil, fmt.Errorf("failed to resolve source %s: %w", name, err)
		}
		ids = append(ids, src.ID)
	}
	return ids, nil
}

// linkSources links an ordered set of sources to a registry at sequential positions.
func linkSources(ctx context.Context, querier *sqlc.Queries, registryID uuid.UUID, sourceIDs []uuid.UUID) error {
	for i, sourceID := range sourceIDs {
		err := querier.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
			RegistryID: registryID,
			SourceID:   sourceID,
			Position:   int32(i),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// unlinkAllSources removes all source links from a registry.
func unlinkAllSources(
	ctx context.Context, querier *sqlc.Queries, registryID uuid.UUID, current []sqlc.ListRegistrySourcesRow,
) error {
	for _, src := range current {
		err := querier.UnlinkRegistrySource(ctx, sqlc.UnlinkRegistrySourceParams{
			RegistryID: registryID,
			SourceID:   src.ID,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

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

	"github.com/stacklok/toolhive-registry-server/internal/db"
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
	callerClaims := claimsFromCtx(ctx)

	registries, err := streamRegistryRows(ctx, querier, callerClaims)
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

	// Validate caller's JWT covers the registry's claims (hide existence on failure)
	if err := validateClaimsSubset(ctx, claimsFromCtx(ctx), db.DeserializeClaims(reg.Claims)); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
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

	// Validate registry claims are a subset of the caller's JWT claims
	callerClaims := claimsFromCtx(ctx)
	if err := validateClaimsSubset(ctx, callerClaims, req.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Resolve source names to IDs, validating caller covers each source's claims
	sourceIDs, err := resolveSourceIDsWithGate(ctx, querier, req.Sources, callerClaims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Insert the registry row
	now := time.Now()
	inserted, err := querier.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
		Name:         name,
		Claims:       db.SerializeClaims(req.Claims),
		CreationType: sqlc.CreationTypeAPI,
		CreatedAt:    &now,
		UpdatedAt:    &now,
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

	// Validate caller's JWT covers both the existing and new registry claims
	callerClaims := claimsFromCtx(ctx)
	if err := validateClaimsSubsetBytes(ctx, callerClaims, existing.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	if err := validateClaimsSubset(ctx, callerClaims, req.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Replace source links: resolve with claim gate, unlink old, link new
	if err := replaceRegistrySourcesWithGate(ctx, querier, existing.ID, req.Sources, callerClaims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Upsert registry row to bump updated_at. The Go-level getExistingAPIRegistry
	// check above already verified this is an API registry.
	// Source links are the mutable part and are updated above.
	now := time.Now()
	upserted, err := querier.UpsertRegistry(ctx, sqlc.UpsertRegistryParams{
		Name:         name,
		Claims:       db.SerializeClaims(req.Claims),
		CreationType: sqlc.CreationTypeAPI,
		CreatedAt:    existing.CreatedAt,
		UpdatedAt:    &now,
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
	existing, err := getExistingAPIRegistry(ctx, querier, name)
	if err != nil {
		otel.RecordError(span, err)
		return err
	}

	// Validate caller's JWT covers the registry's claims
	if err := validateClaimsSubsetBytes(ctx, claimsFromCtx(ctx), existing.Claims); err != nil {
		otel.RecordError(span, err)
		return err
	}

	// Delete the registry (cascades to registry_source junction).
	// Go-level getExistingAPIRegistry check above already verified this is an API registry.
	if _, err := querier.DeleteRegistry(ctx, name); err != nil {
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

// ListRegistryEntries returns all entries across a registry's linked sources.
func (s *dbService) ListRegistryEntries(ctx context.Context, registryName string) ([]service.RegistryEntryInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListRegistryEntries")
	defer span.End()
	start := time.Now()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(registryName))

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

	// Look up the registry by name
	registry, err := querier.GetRegistryByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, registryName)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	// Validate caller's JWT covers the registry's claims (hide existence on failure)
	if err := validateClaimsSubsetBytes(ctx, claimsFromCtx(ctx), registry.Claims); err != nil {
		err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, registryName)
		otel.RecordError(span, err)
		return nil, err
	}

	// Query all entries across sources linked to this registry
	rows, err := querier.ListEntriesByRegistry(ctx, registry.ID)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to list entries by registry: %w", err)
	}

	// Map each row directly to RegistryEntryInfo (flat, no grouping)
	result := make([]service.RegistryEntryInfo, 0, len(rows))
	for _, row := range rows {
		result = append(result, service.RegistryEntryInfo{
			EntryType:  string(row.EntryType),
			Name:       row.Name,
			Version:    row.Version,
			SourceName: row.SourceName,
		})
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(result)))
	slog.DebugContext(ctx, "ListRegistryEntries completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"registry", registryName,
		"entry_count", len(result),
		"request_id", middleware.GetReqID(ctx))
	return result, nil
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

// replaceRegistrySourcesWithGate is like replaceRegistrySources but validates
// the caller's claims cover each source's claims.
func replaceRegistrySourcesWithGate(
	ctx context.Context, querier *sqlc.Queries, registryID uuid.UUID,
	sourceNames []string, callerClaims map[string]any,
) error {
	sourceIDs, err := resolveSourceIDsWithGate(ctx, querier, sourceNames, callerClaims)
	if err != nil {
		return err
	}

	if err := unlinkAllSources(ctx, querier, registryID); err != nil {
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
		Claims:       db.DeserializeClaims(reg.Claims),
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

// resolveSourceIDsWithGate resolves source names to UUIDs and validates that
// the caller's claims cover each source's claims.
func resolveSourceIDsWithGate(
	ctx context.Context, querier *sqlc.Queries, names []string, callerClaims map[string]any,
) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(names))
	for _, name := range names {
		src, err := querier.GetSourceByName(ctx, name)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
			}
			return nil, fmt.Errorf("failed to resolve source %s: %w", name, err)
		}
		if err := validateClaimsSubsetBytes(ctx, callerClaims, src.Claims); err != nil {
			return nil, fmt.Errorf("%w: cannot reference source %s", err, name)
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
	ctx context.Context, querier *sqlc.Queries, registryID uuid.UUID,
) error {
	return querier.UnlinkAllRegistrySources(ctx, registryID)
}

// streamRegistryRows fetches registry rows in batches, filtering by caller claims,
// until the DB is exhausted. This avoids underfilled responses when post-filtering
// drops rows that fail claims checks.
func streamRegistryRows(
	ctx context.Context,
	querier *sqlc.Queries,
	callerClaims map[string]any,
) ([]sqlc.Registry, error) {
	var accumulated []sqlc.Registry
	params := sqlc.ListRegistriesParams{
		Size: int64(service.MaxPageSize),
	}

	for {
		batch, err := querier.ListRegistries(ctx, params)
		if err != nil {
			return nil, err
		}

		for _, reg := range batch {
			if err := validateClaimsSubsetBytes(ctx, callerClaims, reg.Claims); err != nil {
				continue
			}
			accumulated = append(accumulated, reg)
		}

		if int64(len(batch)) < params.Size {
			break
		}

		last := batch[len(batch)-1]
		params.Cursor = &last.Name
	}

	return accumulated, nil
}

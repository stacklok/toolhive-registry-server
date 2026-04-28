package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

// CreateSource creates a new API-managed source
//
//nolint:gocyclo // complexity driven by validation, claim checks, and config serialization
func (s *dbService) CreateSource(
	ctx context.Context,
	name string,
	req *service.SourceCreateRequest,
) (*service.SourceInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.CreateSource")
	defer span.End()
	start := time.Now()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Validate configuration
	if err := service.ValidateSourceConfig(req); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("%w: %v", service.ErrInvalidSourceConfig, err)
	}

	// Validate source claims are a subset of the caller's JWT claims
	if err := s.validateClaimsSubset(ctx, claimsFromCtx(ctx), req.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Add source type attribute after validation
	span.SetAttributes(attribute.String("source.type", string(req.GetSourceType())))

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

	// Check if source already exists
	_, err = querier.GetSourceByName(ctx, name)
	if err == nil {
		err = fmt.Errorf("%w: %s", service.ErrSourceAlreadyExists, name)
		otel.RecordError(span, err)
		return nil, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to check source existence: %w", err)
	}

	// Enforce at most one managed source globally
	if req.GetSourceType() == config.SourceTypeManaged {
		managedSources, err := querier.GetManagedSources(ctx)
		if err != nil {
			otel.RecordError(span, err)
			return nil, fmt.Errorf("failed to check managed source limit: %w", err)
		}
		if len(managedSources) > 0 {
			slog.WarnContext(ctx, "Managed source limit reached",
				"existing_source", managedSources[0].Name)
			otel.RecordError(span, service.ErrManagedSourceLimitReached)
			return nil, service.ErrManagedSourceLimitReached
		}
	}

	// Prepare insert parameters
	now := time.Now()
	sourceType := string(req.GetSourceType())

	sourceConfig, err := serializeSourceConfigFromRequest(req)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	filterConfig, err := serializeFilterConfigFromRequest(req.Filter)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	syncSchedule := parseSyncScheduleFromRequest(req)
	syncable := !req.IsNonSyncedType()

	claimsJSON := db.SerializeClaims(req.Claims)

	params := sqlc.InsertSourceParams{
		Name:         name,
		CreationType: sqlc.CreationTypeAPI,
		SourceType:   sourceType,
		SourceConfig: sourceConfig,
		FilterConfig: filterConfig,
		SyncSchedule: syncSchedule,
		Syncable:     syncable,
		Claims:       claimsJSON,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}

	// Insert the source
	isManagedSource := req.GetSourceType() == config.SourceTypeManaged
	source, err := querier.InsertSource(ctx, params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505": // unique constraint violation
				err = fmt.Errorf("%w: %s", service.ErrSourceAlreadyExists, name)
				otel.RecordError(span, err)
				return nil, err
			case "40001": // serialization failure — concurrent managed source race
				if isManagedSource {
					otel.RecordError(span, service.ErrManagedSourceLimitReached)
					return nil, service.ErrManagedSourceLimitReached
				}
			}
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to insert source: %w", err)
	}

	// Initialize sync status for the new source
	initialSyncStatus, initialErrorMsg := getAPISourceInitialSyncStatus(req, sourceType)
	err = querier.BulkInitializeSourceSyncs(ctx, sqlc.BulkInitializeSourceSyncsParams{
		SourceIds:    []uuid.UUID{source.ID},
		SyncStatuses: []sqlc.SyncStatus{initialSyncStatus},
		ErrorMsgs:    []string{initialErrorMsg},
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to initialize sync status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		// Serialization failure at commit means a concurrent managed source won the race
		var pgErr *pgconn.PgError
		if isManagedSource && errors.As(err, &pgErr) && pgErr.Code == "40001" {
			otel.RecordError(span, service.ErrManagedSourceLimitReached)
			return nil, service.ErrManagedSourceLimitReached
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Source created",
		"duration_ms", time.Since(start).Milliseconds(),
		"name", name,
		"type", source.SourceType,
		"source_type", sourceType,
		"request_id", middleware.GetReqID(ctx))

	// Build and return SourceInfo
	return buildSourceInfoFromDBSource(&source), nil
}

// UpdateSource updates an existing API source
//
//nolint:gocyclo // complexity from creation_type guard is intentional
func (s *dbService) UpdateSource(
	ctx context.Context,
	name string,
	req *service.SourceCreateRequest,
) (*service.SourceInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.UpdateSource")
	defer span.End()
	start := time.Now()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Validate configuration
	if err := service.ValidateSourceConfig(req); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("%w: %v", service.ErrInvalidSourceConfig, err)
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

	// Fetch existing source and validate type constraints
	existing, err := querier.GetSourceByName(ctx, name)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to get existing source: %w", err)
	}

	// Guard: cannot modify CONFIG sources via API
	if existing.CreationType == sqlc.CreationTypeCONFIG {
		err = fmt.Errorf("%w: %s", service.ErrConfigSource, name)
		otel.RecordError(span, err)
		return nil, err
	}

	// Validate caller's JWT covers both the existing and new claims
	callerClaims := claimsFromCtx(ctx)
	if err := s.validateClaimsSubsetBytes(ctx, callerClaims, existing.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	if err := s.validateClaimsSubset(ctx, callerClaims, req.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Check if source type is changing (not allowed)
	if err := validateSourceTypeChange(&existing, req.GetSourceType()); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// For file source types, check if the subtype (path/url/data) is changing
	if req.GetSourceType() == config.SourceTypeFile {
		if err := validateFileSourceTypeChange(&existing, req); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	// Prepare update parameters
	now := time.Now()
	sourceType := string(req.GetSourceType())

	sourceConfig, err := serializeSourceConfigFromRequest(req)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	filterConfig, err := serializeFilterConfigFromRequest(req.Filter)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	syncSchedule := parseSyncScheduleFromRequest(req)
	syncable := !req.IsNonSyncedType()

	claimsJSON := db.SerializeClaims(req.Claims)

	params := sqlc.UpdateSourceParams{
		Name:         name,
		SourceType:   sourceType,
		SourceConfig: sourceConfig,
		FilterConfig: filterConfig,
		SyncSchedule: syncSchedule,
		Syncable:     syncable,
		Claims:       claimsJSON,
		UpdatedAt:    &now,
	}

	// Update the source (creation_type guard is above)
	source, err := querier.UpdateSource(ctx, params)
	if err != nil {
		updateErr := classifyUpdateSourceError(ctx, querier, name, err)
		otel.RecordError(span, updateErr)
		return nil, updateErr
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Source updated",
		"duration_ms", time.Since(start).Milliseconds(),
		"name", name,
		"type", source.SourceType,
		"source_type", sourceType,
		"request_id", middleware.GetReqID(ctx))

	// Build and return SourceInfo
	return buildSourceInfoFromDBSource(&source), nil
}

// DeleteSource deletes an API source
func (s *dbService) DeleteSource(ctx context.Context, name string) error {
	ctx, span := s.startSpan(ctx, "dbService.DeleteSource")
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

	// Look up the source first to validate it exists and is API-created
	existing, err := querier.GetSourceByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
			otel.RecordError(span, err)
			return err
		}
		otel.RecordError(span, err)
		return fmt.Errorf("failed to get source: %w", err)
	}
	if existing.CreationType == sqlc.CreationTypeCONFIG {
		err = fmt.Errorf("%w: %s", service.ErrConfigSource, name)
		otel.RecordError(span, err)
		return err
	}

	// Validate caller's JWT covers the source's claims
	if err := s.validateClaimsSubsetBytes(ctx, claimsFromCtx(ctx), existing.Claims); err != nil {
		otel.RecordError(span, err)
		return err
	}

	// Check if the source is linked to any registries.
	// The RESTRICT FK on registry_source.source_id would block deletion anyway, but we
	// want to return a clear error message.
	linkCount, err := querier.CountRegistriesBySourceID(ctx, existing.ID)
	if err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to check registry links: %w", err)
	}
	if linkCount > 0 {
		err = fmt.Errorf("%w: %s is referenced by %d registry(ies)", service.ErrSourceInUse, name, linkCount)
		otel.RecordError(span, err)
		return err
	}

	// Delete the source — we already verified it exists, is API-created, and is not in use.
	if _, err := querier.DeleteSource(ctx, name); err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to delete source: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Source deleted",
		"duration_ms", time.Since(start).Milliseconds(),
		"name", name,
		"request_id", middleware.GetReqID(ctx))

	return nil
}

// ListSources returns all configured sources
func (s *dbService) ListSources(ctx context.Context) ([]service.SourceInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListSources")
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

	dbSources, err := s.streamSourceRows(ctx, querier, callerClaims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}

	result := make([]service.SourceInfo, 0, len(dbSources))
	for _, src := range dbSources {
		info := buildSourceInfoFromListRow(&src)
		info.SyncStatus = fetchSyncStatus(ctx, querier, src.Name)
		result = append(result, *info)
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(result)))
	slog.DebugContext(ctx, "ListSources completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"count", len(result),
		"request_id", middleware.GetReqID(ctx))
	return result, nil
}

// GetSourceByName returns a single source by name
func (s *dbService) GetSourceByName(ctx context.Context, name string) (*service.SourceInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.GetSourceByName")
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

	// Get the source by name
	source, err := querier.GetSourceByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to get source: %w", err)
	}

	// Build SourceInfo with all config fields
	info := buildSourceInfoFromGetByNameRow(&source)

	// Validate caller's JWT covers the source's claims (hide existence on failure)
	if err := s.validateClaimsSubset(ctx, claimsFromCtx(ctx), info.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
	}

	info.SyncStatus = fetchSyncStatus(ctx, querier, source.Name)

	slog.DebugContext(ctx, "GetSourceByName completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"source", name,
		"request_id", middleware.GetReqID(ctx))
	return info, nil
}

// ProcessInlineSourceData processes inline source data synchronously.
// It parses the data, validates it, and stores the servers in the database.
func (s *dbService) ProcessInlineSourceData(ctx context.Context, name string, data string) error {
	// Capture actual start time for accurate timing
	startTime := time.Now()

	// Parse the inline data using the registry validator
	validator := sources.NewRegistryDataValidator()
	registry, err := validator.ValidateData([]byte(data))
	if err != nil {
		// Update sync status to failed with actual timing
		errMsg := fmt.Sprintf("failed to parse registry data: %v", err)
		if updateErr := s.updateSyncStatusFailed(ctx, name, errMsg, startTime); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update sync status after parse error",
				"registry", name,
				"parse_error", err,
				"update_error", updateErr)
		}
		return fmt.Errorf("failed to parse inline registry data: %w", err)
	}

	// Create a sync writer to store the servers
	syncWriter, err := writer.NewDBSyncWriter(s.pool, s.maxMetaSize)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create sync writer: %v", err)
		if updateErr := s.updateSyncStatusFailed(ctx, name, errMsg, startTime); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update sync status after writer creation error",
				"registry", name,
				"writer_error", err,
				"update_error", updateErr)
		}
		return fmt.Errorf("failed to create sync writer: %w", err)
	}

	// Store the servers in the database
	if err := syncWriter.Store(ctx, name, registry); err != nil {
		errMsg := fmt.Sprintf("failed to store servers: %v", err)
		if updateErr := s.updateSyncStatusFailed(ctx, name, errMsg, startTime); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update sync status after store error",
				"registry", name,
				"store_error", err,
				"update_error", updateErr)
		}
		return fmt.Errorf("failed to store inline registry data: %w", err)
	}

	// Update sync status to completed with actual timing
	if err := s.updateSyncStatusCompleted(ctx, name, len(registry.Data.Servers), startTime); err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status to completed",
			"registry", name,
			"error", err)
		// Don't return error here - the data was successfully stored
	}

	slog.InfoContext(ctx, "Inline registry data processed successfully",
		"duration_ms", time.Since(startTime).Milliseconds(),
		"registry", name,
		"server_count", len(registry.Data.Servers))

	return nil
}

// ListSourceEntries returns all entries for a source with their versions.
func (s *dbService) ListSourceEntries(ctx context.Context, sourceName string) ([]service.SourceEntryInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListSourceEntries")
	defer span.End()
	start := time.Now()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(sourceName))

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

	// Look up the source by name
	source, err := querier.GetSourceByName(ctx, sourceName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: %s", service.ErrSourceNotFound, sourceName)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to get source: %w", err)
	}

	// Validate caller's JWT covers the source's claims (hide existence on failure)
	if err := s.validateClaimsSubsetBytes(ctx, claimsFromCtx(ctx), source.Claims); err != nil {
		err = fmt.Errorf("%w: %s", service.ErrSourceNotFound, sourceName)
		otel.RecordError(span, err)
		return nil, err
	}

	// Query all entries with versions for this source
	rows, err := querier.ListEntriesBySource(ctx, source.ID)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to list entries by source: %w", err)
	}

	// Group flat rows by (entry_type, name) into SourceEntryInfo with nested versions
	type entryKey = string
	entryMap := make(map[entryKey]int) // key -> index in result slice
	result := make([]service.SourceEntryInfo, 0)

	for _, row := range rows {
		key := string(row.EntryType) + "/" + row.Name
		idx, exists := entryMap[key]
		if !exists {
			idx = len(result)
			entryMap[key] = idx
			result = append(result, service.SourceEntryInfo{
				EntryType: string(row.EntryType),
				Name:      row.Name,
				Claims:    db.DeserializeClaims(row.Claims),
			})
		}

		versionInfo := service.EntryVersionInfo{
			Version: row.Version,
		}
		if row.Title != nil {
			versionInfo.Title = *row.Title
		}
		if row.Description != nil {
			versionInfo.Description = *row.Description
		}
		if row.CreatedAt != nil {
			versionInfo.CreatedAt = *row.CreatedAt
		}
		if row.UpdatedAt != nil {
			versionInfo.UpdatedAt = *row.UpdatedAt
		}

		result[idx].Versions = append(result[idx].Versions, versionInfo)
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(result)))
	slog.DebugContext(ctx, "ListSourceEntries completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"source", sourceName,
		"entry_count", len(result),
		"request_id", middleware.GetReqID(ctx))
	return result, nil
}

// =============================================================================
// Helper functions for source CRUD operations
// =============================================================================

// validateSourceTypeChange checks if the source type is changing and returns an error if so.
// Users cannot change a source's type (e.g., git to file) - they must delete and recreate.
// existing may be a zero-value row if the source doesn't exist yet.
func validateSourceTypeChange(existing *sqlc.GetSourceByNameRow, newSourceType config.SourceType) error {
	if existing.SourceType != "" && existing.SourceType != string(newSourceType) {
		return fmt.Errorf("%w: cannot change from '%s' to '%s', delete and recreate the source instead",
			service.ErrSourceTypeChangeNotAllowed, existing.SourceType, newSourceType)
	}
	return nil
}

// validateFileSourceTypeChange checks if the file source subtype (path/url/data) is changing
// and returns an error if so. Users must delete and recreate to change file source subtypes.
// existing may be a zero-value row if the source doesn't exist yet.
func validateFileSourceTypeChange(existing *sqlc.GetSourceByNameRow, newConfig *service.SourceCreateRequest) error {
	// If the existing source is not a file type, no validation needed
	if existing.SourceType != string(config.SourceTypeFile) {
		return nil
	}

	// Deserialize the existing source config
	var existingFileConfig config.FileConfig
	if len(existing.SourceConfig) > 0 {
		if err := json.Unmarshal(existing.SourceConfig, &existingFileConfig); err != nil {
			return fmt.Errorf("failed to parse existing file config: %w", err)
		}
	}

	// Determine the existing and new file subtypes
	existingSubtype := getFileSourceSubtype(&existingFileConfig)
	newSubtype := getFileSourceSubtype(newConfig.File)

	// Check if the subtype is changing
	if existingSubtype != newSubtype {
		return fmt.Errorf("%w: cannot change file source type from '%s' to '%s', delete and recreate the source instead",
			service.ErrInvalidSourceConfig, existingSubtype, newSubtype)
	}

	return nil
}

// classifyUpdateSourceError determines the appropriate error when UpdateSource returns ErrNoRows.
// The update may have failed because the source doesn't exist.
func classifyUpdateSourceError(ctx context.Context, querier *sqlc.Queries, name string, err error) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to update source: %w", err)
	}
	existing, checkErr := querier.GetSourceByName(ctx, name)
	if checkErr != nil {
		if errors.Is(checkErr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
		}
		return fmt.Errorf("failed to check source: %w", checkErr)
	}
	if existing.CreationType == sqlc.CreationTypeCONFIG {
		return fmt.Errorf("%w: %s", service.ErrConfigSource, name)
	}
	return fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
}

// serializeSourceConfigFromRequest serializes the source config from request to JSON bytes
func serializeSourceConfigFromRequest(req *service.SourceCreateRequest) ([]byte, error) {
	if req == nil {
		return nil, nil
	}

	sourceConfig := req.GetSourceConfig()
	if sourceConfig == nil {
		return nil, nil
	}

	data, err := json.Marshal(sourceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize source config: %w", err)
	}
	return data, nil
}

// serializeFilterConfigFromRequest serializes the filter config from request to JSON bytes
func serializeFilterConfigFromRequest(filter *config.FilterConfig) ([]byte, error) {
	if filter == nil {
		return nil, nil
	}

	data, err := json.Marshal(filter)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize filter config: %w", err)
	}
	return data, nil
}

// parseSyncScheduleFromRequest parses the sync schedule from request to pgtypes.Interval
func parseSyncScheduleFromRequest(req *service.SourceCreateRequest) pgtypes.Interval {
	if req == nil || req.SyncPolicy == nil || req.SyncPolicy.Interval == "" {
		return pgtypes.NewNullInterval()
	}

	interval, err := pgtypes.ParseDuration(req.SyncPolicy.Interval)
	if err != nil {
		return pgtypes.NewNullInterval()
	}
	return interval
}

// sourceRow is a common representation of source data from different sqlc row types.
type sourceRow struct {
	Name         string
	SourceType   string
	CreationType sqlc.CreationType
	SourceConfig []byte
	FilterConfig []byte
	SyncSchedule pgtypes.Interval
	Claims       []byte
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

// buildSourceInfo builds a SourceInfo from the common sourceRow representation.
func buildSourceInfo(row *sourceRow) *service.SourceInfo {
	info := &service.SourceInfo{
		Name:         row.Name,
		Type:         row.SourceType,
		CreationType: service.CreationType(row.CreationType),
	}

	if row.SourceType != "" {
		info.SourceType = config.SourceType(row.SourceType)
	}

	if row.SourceType != "" {
		info.SourceConfig = deserializeSourceConfig(row.SourceType, row.SourceConfig)
	}

	info.FilterConfig = deserializeFilterConfig(row.FilterConfig)
	info.Claims = db.DeserializeClaims(row.Claims)

	if row.SyncSchedule.Valid {
		info.SyncSchedule = row.SyncSchedule.Duration.String()
	}

	if row.CreatedAt != nil {
		info.CreatedAt = *row.CreatedAt
	}

	if row.UpdatedAt != nil {
		info.UpdatedAt = *row.UpdatedAt
	}

	return info
}

// buildSourceInfoFromDBSource builds a SourceInfo from a database Source.
func buildSourceInfoFromDBSource(source *sqlc.Source) *service.SourceInfo {
	return buildSourceInfo(&sourceRow{
		Name:         source.Name,
		SourceType:   source.SourceType,
		CreationType: source.CreationType,
		SourceConfig: source.SourceConfig,
		FilterConfig: source.FilterConfig,
		SyncSchedule: source.SyncSchedule,
		Claims:       source.Claims,
		CreatedAt:    source.CreatedAt,
		UpdatedAt:    source.UpdatedAt,
	})
}

// buildSourceInfoFromListRow builds a SourceInfo from a ListSourcesRow.
func buildSourceInfoFromListRow(row *sqlc.ListSourcesRow) *service.SourceInfo {
	return buildSourceInfo(&sourceRow{
		Name:         row.Name,
		SourceType:   row.SourceType,
		CreationType: row.CreationType,
		SourceConfig: row.SourceConfig,
		FilterConfig: row.FilterConfig,
		SyncSchedule: row.SyncSchedule,
		Claims:       row.Claims,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	})
}

// buildSourceInfoFromGetByNameRow builds a SourceInfo from a GetSourceByNameRow.
func buildSourceInfoFromGetByNameRow(row *sqlc.GetSourceByNameRow) *service.SourceInfo {
	return buildSourceInfo(&sourceRow{
		Name:         row.Name,
		SourceType:   row.SourceType,
		CreationType: row.CreationType,
		SourceConfig: row.SourceConfig,
		FilterConfig: row.FilterConfig,
		SyncSchedule: row.SyncSchedule,
		Claims:       row.Claims,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	})
}

// fetchSyncStatus retrieves and converts sync status for a source.
// Returns nil if sync record doesn't exist.
func fetchSyncStatus(ctx context.Context, querier *sqlc.Queries, sourceName string) *service.SourceSyncStatus {
	syncRecord, err := querier.GetSourceSyncByName(ctx, sourceName)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("Failed to get sync status for source",
				"source", sourceName,
				"error", err)
		}
		return nil
	}
	return &service.SourceSyncStatus{
		Phase:        convertSyncPhase(syncRecord.SyncStatus),
		LastSyncTime: syncRecord.EndedAt,
		LastAttempt:  syncRecord.StartedAt,
		AttemptCount: int(syncRecord.AttemptCount),
		ServerCount:  int(syncRecord.ServerCount),
		SkillCount:   int(syncRecord.SkillCount),
		Message:      getStatusMessage(syncRecord.ErrorMsg),
	}
}

// updateSyncStatusFailed updates the sync status to failed with an error message.
// startTime is the time when processing began, endTime is captured when this is called.
func (s *dbService) updateSyncStatusFailed(
	ctx context.Context, name string, errorMsg string, startTime time.Time,
) error {
	querier := sqlc.New(s.pool)
	endTime := time.Now()
	return querier.UpsertSourceSyncByName(ctx, sqlc.UpsertSourceSyncByNameParams{
		Name:         name,
		SyncStatus:   sqlc.SyncStatusFAILED,
		ErrorMsg:     &errorMsg,
		StartedAt:    &startTime,
		EndedAt:      &endTime,
		AttemptCount: 1,
		ServerCount:  0,
	})
}

// updateSyncStatusCompleted updates the sync status to completed.
// startTime is the time when processing began, endTime is captured when this is called.
func (s *dbService) updateSyncStatusCompleted(
	ctx context.Context, name string, serverCount int, startTime time.Time,
) error {
	querier := sqlc.New(s.pool)
	endTime := time.Now()
	return querier.UpsertSourceSyncByName(ctx, sqlc.UpsertSourceSyncByNameParams{
		Name:         name,
		SyncStatus:   sqlc.SyncStatusCOMPLETED,
		ErrorMsg:     nil,
		StartedAt:    &startTime,
		EndedAt:      &endTime,
		AttemptCount: 1,
		ServerCount:  int64(serverCount),
	})
}

// getAPISourceInitialSyncStatus returns the initial sync status and message for an API source.
// Non-synced types (managed, kubernetes) start as COMPLETED; synced types and inline data start as FAILED.
func getAPISourceInitialSyncStatus(req *service.SourceCreateRequest, sourceType string) (sqlc.SyncStatus, string) {
	if req.IsNonSyncedType() && !req.IsInlineData() {
		return sqlc.SyncStatusCOMPLETED, fmt.Sprintf("Non-synced registry (type: %s)", sourceType)
	}
	return sqlc.SyncStatusFAILED, "No previous sync status found"
}

// getFileSourceSubtype returns which file source subtype is being used (path, url, or data)
func getFileSourceSubtype(cfg *config.FileConfig) string {
	if cfg == nil {
		return unknownSubtype
	}
	switch {
	case cfg.Path != "":
		return "path"
	case cfg.URL != "":
		return "url"
	case cfg.Data != "":
		return "data"
	default:
		return unknownSubtype
	}
}

// convertSyncPhase converts database SyncStatus enum to service phase string
func convertSyncPhase(status sqlc.SyncStatus) string {
	switch status {
	case sqlc.SyncStatusINPROGRESS:
		return "syncing"
	case sqlc.SyncStatusCOMPLETED:
		return "complete"
	case sqlc.SyncStatusFAILED:
		return "failed"
	default:
		return "unknown"
	}
}

// getStatusMessage converts error message pointer to string
func getStatusMessage(errorMsg *string) string {
	if errorMsg == nil || *errorMsg == "" {
		return ""
	}
	return *errorMsg
}

// streamSourceRows fetches source rows in batches, filtering by caller claims,
// until the DB is exhausted. This avoids underfilled responses when post-filtering
// drops rows that fail claims checks.
func (s *dbService) streamSourceRows(
	ctx context.Context,
	querier *sqlc.Queries,
	callerClaims map[string]any,
) ([]sqlc.ListSourcesRow, error) {
	var accumulated []sqlc.ListSourcesRow
	params := sqlc.ListSourcesParams{
		Size: int64(service.MaxPageSize),
	}

	for {
		batch, err := querier.ListSources(ctx, params)
		if err != nil {
			return nil, err
		}

		for _, src := range batch {
			if err := s.validateClaimsSubsetBytes(ctx, callerClaims, src.Claims); err != nil {
				continue
			}
			accumulated = append(accumulated, src)
		}

		if int64(len(batch)) < params.Size {
			break
		}

		last := batch[len(batch)-1]
		params.Next = last.CreatedAt
	}

	return accumulated, nil
}

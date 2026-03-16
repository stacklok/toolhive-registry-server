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
	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

// CreateSource creates a new API-managed source
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

	// Prepare insert parameters
	now := time.Now()
	sourceType := string(req.GetSourceType())
	format := req.Format
	if format == "" {
		format = "upstream" // default format
	}

	sourceConfig := serializeSourceConfigFromRequest(req)
	filterConfig := serializeFilterConfigFromRequest(req.Filter)
	syncSchedule := parseSyncScheduleFromRequest(req)
	syncable := !req.IsNonSyncedType()

	params := sqlc.InsertAPISourceParams{
		Name:         name,
		SourceType:   sourceType,
		Format:       &format,
		SourceConfig: sourceConfig,
		FilterConfig: filterConfig,
		SyncSchedule: syncSchedule,
		Syncable:     syncable,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}

	// Insert the source
	registry, err := querier.InsertAPISource(ctx, params)
	if err != nil {
		// Check for unique constraint violation
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			err = fmt.Errorf("%w: %s", service.ErrSourceAlreadyExists, name)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to insert source: %w", err)
	}

	// Create corresponding registry row and link to source
	if err := createRegistryRowAndLink(ctx, querier, name, registry.ID, &now); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Initialize sync status for the new registry
	initialSyncStatus, initialErrorMsg := getAPISourceInitialSyncStatus(req, sourceType)
	err = querier.BulkInitializeSourceSyncs(ctx, sqlc.BulkInitializeSourceSyncsParams{
		SourceIds:    []uuid.UUID{registry.ID},
		SyncStatuses: []sqlc.SyncStatus{initialSyncStatus},
		ErrorMsgs:    []string{initialErrorMsg},
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to initialize sync status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Source created",
		"duration_ms", time.Since(start).Milliseconds(),
		"name", name,
		"type", registry.SourceType,
		"source_type", sourceType,
		"request_id", middleware.GetReqID(ctx))

	// Build and return SourceInfo
	return buildSourceInfoFromDBSource(&registry), nil
}

// UpdateSource updates an existing API source
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

	// Check if source type is changing (not allowed)
	if err := s.validateSourceTypeChange(ctx, name, req.GetSourceType()); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// For file source types, check if the subtype (path/url/data) is changing
	// We don't allow changing between path, url, and data - user must delete and recreate
	if req.GetSourceType() == config.SourceTypeFile {
		if err := s.validateFileSourceTypeChange(ctx, name, req); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
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

	// Prepare update parameters
	now := time.Now()
	sourceType := string(req.GetSourceType())
	format := req.Format
	if format == "" {
		format = "upstream" // default format
	}

	sourceConfig := serializeSourceConfigFromRequest(req)
	filterConfig := serializeFilterConfigFromRequest(req.Filter)
	syncSchedule := parseSyncScheduleFromRequest(req)
	syncable := !req.IsNonSyncedType()

	params := sqlc.UpdateAPISourceParams{
		Name:         name,
		SourceType:   sourceType,
		Format:       &format,
		SourceConfig: sourceConfig,
		FilterConfig: filterConfig,
		SyncSchedule: syncSchedule,
		Syncable:     syncable,
		UpdatedAt:    &now,
	}

	// Update the source (only updates API-created sources)
	registry, err := querier.UpdateAPISource(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Source not found or is CONFIG type, check which case
			existing, checkErr := querier.GetSourceByName(ctx, name)
			if checkErr != nil {
				if errors.Is(checkErr, pgx.ErrNoRows) {
					err = fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
					otel.RecordError(span, err)
					return nil, err
				}
				otel.RecordError(span, checkErr)
				return nil, fmt.Errorf("failed to check source: %w", checkErr)
			}
			// Source exists but is CONFIG type
			if existing.CreationType == sqlc.CreationTypeCONFIG {
				err = fmt.Errorf("%w: %s", service.ErrConfigSource, name)
				otel.RecordError(span, err)
				return nil, err
			}
			// Should not reach here, but return not found just in case
			err = fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to update source: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Source updated",
		"duration_ms", time.Since(start).Milliseconds(),
		"name", name,
		"type", registry.SourceType,
		"source_type", sourceType,
		"request_id", middleware.GetReqID(ctx))

	// Build and return SourceInfo
	return buildSourceInfoFromDBSource(&registry), nil
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

	// Delete the registry row first (cascades to registry_source junction).
	// This removes the RESTRICT FK on source, allowing source deletion.
	// If no registry row exists (e.g., sources created before this fix), (0, nil) is returned.
	if _, err := querier.DeleteRegistry(ctx, name); err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to delete registry row: %w", err)
	}

	// Delete the source (only deletes API-created sources)
	rowsAffected, err := querier.DeleteAPISource(ctx, name)
	if err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to delete source: %w", err)
	}

	if rowsAffected == 0 {
		// Source not found or is CONFIG type, check which case
		existing, checkErr := querier.GetSourceByName(ctx, name)
		if checkErr != nil {
			if errors.Is(checkErr, pgx.ErrNoRows) {
				err = fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
				otel.RecordError(span, err)
				return err
			}
			otel.RecordError(span, checkErr)
			return fmt.Errorf("failed to check source: %w", checkErr)
		}
		// Source exists but is CONFIG type
		if existing.CreationType == sqlc.CreationTypeCONFIG {
			err = fmt.Errorf("%w: %s", service.ErrConfigSource, name)
			otel.RecordError(span, err)
			return err
		}
		// Should not reach here, but return not found just in case
		err = fmt.Errorf("%w: %s", service.ErrSourceNotFound, name)
		otel.RecordError(span, err)
		return err
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

	// List all sources (no pagination for now)
	params := sqlc.ListSourcesParams{
		Size: service.MaxPageSize, // Maximum number of sources to return
	}

	dbSources, err := querier.ListSources(ctx, params)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}

	// Convert to API response format
	result := make([]service.SourceInfo, 0, len(dbSources))
	for _, src := range dbSources {
		// Build SourceInfo with all config fields
		info := buildSourceInfoFromListRow(&src)

		// Fetch sync status from database
		syncRecord, err := querier.GetSourceSyncByName(ctx, src.Name)
		if err != nil {
			// It's okay if sync record doesn't exist yet (source may not have been synced)
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("Failed to get sync status for source",
					"source", src.Name,
					"error", err)
			}
			// Leave SyncStatus as nil if not found or error
			info.SyncStatus = nil
		} else {
			// Convert database sync status to service type
			info.SyncStatus = &service.SourceSyncStatus{
				Phase:        convertSyncPhase(syncRecord.SyncStatus),
				LastSyncTime: syncRecord.EndedAt,   // EndedAt represents successful completion
				LastAttempt:  syncRecord.StartedAt, // StartedAt is the last attempt time
				AttemptCount: int(syncRecord.AttemptCount),
				ServerCount:  int(syncRecord.ServerCount),
				Message:      getStatusMessage(syncRecord.ErrorMsg),
			}
		}

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

	// Fetch sync status from database
	syncRecord, err := querier.GetSourceSyncByName(ctx, source.Name)
	if err != nil {
		// It's okay if sync record doesn't exist yet (source may not have been synced)
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("Failed to get sync status for source",
				"source", source.Name,
				"error", err)
		}
		// Leave SyncStatus as nil if not found or error
		info.SyncStatus = nil
	} else {
		// Convert database sync status to service type
		info.SyncStatus = &service.SourceSyncStatus{
			Phase:        convertSyncPhase(syncRecord.SyncStatus),
			LastSyncTime: syncRecord.EndedAt,   // EndedAt represents successful completion
			LastAttempt:  syncRecord.StartedAt, // StartedAt is the last attempt time
			AttemptCount: int(syncRecord.AttemptCount),
			ServerCount:  int(syncRecord.ServerCount),
			Message:      getStatusMessage(syncRecord.ErrorMsg),
		}
	}

	slog.DebugContext(ctx, "GetSourceByName completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"source", name,
		"request_id", middleware.GetReqID(ctx))
	return info, nil
}

// ProcessInlineSourceData processes inline source data synchronously.
// It parses the data, validates it, and stores the servers in the database.
func (s *dbService) ProcessInlineSourceData(ctx context.Context, name string, data string, format string) error {
	// Capture actual start time for accurate timing
	startTime := time.Now()

	// Parse the inline data using the registry validator
	validator := sources.NewRegistryDataValidator()
	registry, err := validator.ValidateData([]byte(data), format)
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

// =============================================================================
// Helper functions for source CRUD operations
// =============================================================================

// validateSourceTypeChange checks if the source type is changing and returns an error if so.
// Users cannot change a source's type (e.g., git to file) - they must delete and recreate.
func (s *dbService) validateSourceTypeChange(
	ctx context.Context, name string, newSourceType config.SourceType,
) error {
	querier := sqlc.New(s.pool)
	existing, err := querier.GetSourceByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Source doesn't exist yet, no validation needed
			return nil
		}
		return fmt.Errorf("failed to get existing source: %w", err)
	}

	// Check if source type is changing
	if existing.SourceType != "" && existing.SourceType != string(newSourceType) {
		return fmt.Errorf("%w: cannot change from '%s' to '%s', delete and recreate the source instead",
			service.ErrSourceTypeChangeNotAllowed, existing.SourceType, newSourceType)
	}

	return nil
}

// validateFileSourceTypeChange checks if the file source subtype (path/url/data) is changing
// and returns an error if so. Users must delete and recreate to change file source subtypes.
func (s *dbService) validateFileSourceTypeChange(
	ctx context.Context, name string, newConfig *service.SourceCreateRequest,
) error {
	// Get the existing source
	querier := sqlc.New(s.pool)
	existing, err := querier.GetSourceByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Source doesn't exist yet, no validation needed
			return nil
		}
		return fmt.Errorf("failed to get existing source: %w", err)
	}

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

// serializeSourceConfigFromRequest serializes the source config from request to JSON bytes
func serializeSourceConfigFromRequest(req *service.SourceCreateRequest) []byte {
	if req == nil {
		return nil
	}

	sourceConfig := req.GetSourceConfig()
	if sourceConfig == nil {
		return nil
	}

	data, err := json.Marshal(sourceConfig)
	if err != nil {
		return nil
	}
	return data
}

// serializeFilterConfigFromRequest serializes the filter config from request to JSON bytes
func serializeFilterConfigFromRequest(filter *config.FilterConfig) []byte {
	if filter == nil {
		return nil
	}

	data, err := json.Marshal(filter)
	if err != nil {
		return nil
	}
	return data
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

// buildSourceInfoFromDBSource builds a SourceInfo from a database Source
func buildSourceInfoFromDBSource(source *sqlc.Source) *service.SourceInfo {
	info := &service.SourceInfo{
		Name:         source.Name,
		Type:         source.SourceType,
		CreationType: service.CreationType(source.CreationType),
	}

	if source.SourceType != "" {
		info.SourceType = config.SourceType(source.SourceType)
	}

	if source.Format != nil {
		info.Format = *source.Format
	}

	if source.SourceType != "" {
		info.SourceConfig = deserializeSourceConfig(source.SourceType, source.SourceConfig)
	}

	info.FilterConfig = deserializeFilterConfig(source.FilterConfig)

	if source.SyncSchedule.Valid {
		info.SyncSchedule = source.SyncSchedule.Duration.String()
	}

	if source.CreatedAt != nil {
		info.CreatedAt = *source.CreatedAt
	}

	if source.UpdatedAt != nil {
		info.UpdatedAt = *source.UpdatedAt
	}

	return info
}

// buildSourceInfoFromListRow builds a SourceInfo from a ListSourcesRow
func buildSourceInfoFromListRow(row *sqlc.ListSourcesRow) *service.SourceInfo {
	info := &service.SourceInfo{
		Name:         row.Name,
		Type:         row.SourceType,
		CreationType: service.CreationType(row.CreationType),
	}

	if row.SourceType != "" {
		info.SourceType = config.SourceType(row.SourceType)
	}

	if row.Format != nil {
		info.Format = *row.Format
	}

	if row.SourceType != "" {
		info.SourceConfig = deserializeSourceConfig(row.SourceType, row.SourceConfig)
	}

	info.FilterConfig = deserializeFilterConfig(row.FilterConfig)

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

// buildSourceInfoFromGetByNameRow builds a SourceInfo from a GetSourceByNameRow
func buildSourceInfoFromGetByNameRow(row *sqlc.GetSourceByNameRow) *service.SourceInfo {
	info := &service.SourceInfo{
		Name:         row.Name,
		Type:         row.SourceType,
		CreationType: service.CreationType(row.CreationType),
	}

	if row.SourceType != "" {
		info.SourceType = config.SourceType(row.SourceType)
	}

	if row.Format != nil {
		info.Format = *row.Format
	}

	if row.SourceType != "" {
		info.SourceConfig = deserializeSourceConfig(row.SourceType, row.SourceConfig)
	}

	info.FilterConfig = deserializeFilterConfig(row.FilterConfig)

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

// createRegistryRowAndLink inserts an API registry row and links it to a source at position 0.
func createRegistryRowAndLink(
	ctx context.Context,
	querier *sqlc.Queries,
	name string,
	sourceID uuid.UUID,
	now *time.Time,
) error {
	registryRow, err := querier.UpsertAPIRegistry(ctx, sqlc.UpsertAPIRegistryParams{
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("failed to insert registry row: %w", err)
	}

	err = querier.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{
		RegistryID: registryRow.ID,
		SourceID:   sourceID,
		Position:   0,
	})
	if err != nil {
		return fmt.Errorf("failed to link registry to source: %w", err)
	}

	return nil
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

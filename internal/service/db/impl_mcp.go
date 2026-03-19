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
	"github.com/jackc/pgx/v5/pgconn"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	model "github.com/modelcontextprotocol/registry/pkg/model"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/validators"
	"github.com/stacklok/toolhive-registry-server/internal/versions"
)

// ListServers returns all servers in the registry
//
//nolint:gocyclo
func (s *dbService) ListServers(
	ctx context.Context,
	opts ...service.Option,
) (*service.ListServersResult, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListServers")
	defer span.End()

	options := &service.ListServersOptions{
		Limit: service.DefaultPageSize, // default limit
	}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	// Cap the limit at service.MaxPageSize to prevent potential DoS
	if options.Limit > service.MaxPageSize {
		options.Limit = service.MaxPageSize
	}

	// Add tracing attributes after options are parsed
	span.SetAttributes(
		otel.AttrPageSize.Int(options.Limit),
		otel.AttrHasCursor.Bool(options.Cursor != ""),
	)
	if options.RegistryName != nil {
		span.SetAttributes(otel.AttrRegistryName.String(*options.RegistryName))

		if err := checkRegistryExists(ctx, s.pool, *options.RegistryName); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	slog.DebugContext(ctx, "ListServers query",
		"limit", options.Limit,
		"registry", options.RegistryName,
		"search", options.Search,
		"cursor", options.Cursor,
		"updated_since", options.UpdatedSince,
		"version", options.Version,
		"request_id", middleware.GetReqID(ctx))

	// Request one extra record to detect if there are more results
	params := sqlc.ListServersParams{
		Size:         int64(options.Limit + 1),
		RegistryName: options.RegistryName,
	}
	if options.Search != "" {
		params.Search = &options.Search
	}

	if options.Cursor != "" {
		cursorName, cursorVersion, err := service.DecodeCursor(options.Cursor)
		if err != nil {
			otel.RecordError(span, err)
			return nil, fmt.Errorf("invalid cursor format: %w", err)
		}
		params.CursorName = &cursorName
		params.CursorVersion = &cursorVersion
	}

	if !options.UpdatedSince.IsZero() {
		params.UpdatedSince = &options.UpdatedSince
	}

	if options.Version != "" {
		params.Version = &options.Version
	}

	// Fetch servers in a loop to ensure we have enough results after dedup.
	// Dedup can remove entries when multiple sources provide the same name,
	// so a single SQL fetch of limit+1 rows may yield fewer than limit
	// deduplicated results. We loop, advancing the SQL cursor, until we
	// have enough or the database is exhausted.
	//
	// All fetched helpers are accumulated and deduped together to ensure
	// consistent cross-batch dedup (a name's winning source must be the
	// same regardless of batch boundaries).
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		const maxFetchIterations = 10 // safety cap to prevent runaway loops
		target := options.Limit + 1   // +1 to detect hasMore
		var allHelpers []helper
		batchParams := params // copy so we can mutate cursor

		for range maxFetchIterations {
			servers, err := querier.ListServers(ctx, batchParams)
			if err != nil {
				return nil, err
			}

			for _, server := range servers {
				allHelpers = append(allHelpers, listServersRowToHelper(server))
			}

			deduped := deduplicateHelpers(allHelpers)

			// Stop if we have enough deduplicated results or SQL returned
			// fewer rows than requested (no more data).
			if len(deduped) >= target || int64(len(servers)) < batchParams.Size {
				return deduped, nil
			}

			// Advance cursor to the last fetched row's (name, version)
			lastRow := servers[len(servers)-1]
			batchParams.CursorName = &lastRow.Name
			batchParams.CursorVersion = &lastRow.Version
		}

		// Iteration cap reached — return what we have
		return deduplicateHelpers(allHelpers), nil
	}

	results, lastCursor, err := s.sharedListServersWithCursor(ctx, querierFunc, options.Limit)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Calculate NextCursor if there are more results
	var nextCursor string
	if lastCursor != nil {
		nextCursor = service.EncodeCursor(lastCursor.Name, lastCursor.Version)
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(results)))
	slog.DebugContext(ctx, "ListServers completed",
		"count", len(results),
		"has_more", nextCursor != "",
		"request_id", middleware.GetReqID(ctx))

	return &service.ListServersResult{
		Servers:    results,
		NextCursor: nextCursor,
	}, nil
}

// ListServerVersions implements RegistryService.ListServerVersions
func (s *dbService) ListServerVersions(
	ctx context.Context,
	opts ...service.Option,
) ([]*upstreamv0.ServerJSON, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListServerVersions")
	defer span.End()
	start := time.Now()

	options := &service.ListServerVersionsOptions{
		Limit: service.DefaultPageSize, // default limit
	}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	if options.Next != nil && options.Prev != nil {
		err := fmt.Errorf("next and prev cannot be set at the same time")
		otel.RecordError(span, err)
		return nil, err
	}

	// Cap the limit at service.MaxPageSize to prevent potential DoS
	if options.Limit > service.MaxPageSize {
		options.Limit = service.MaxPageSize
	}

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrServerName.String(options.Name),
		otel.AttrPageSize.Int(options.Limit),
	)
	if options.RegistryName != nil {
		span.SetAttributes(otel.AttrRegistryName.String(*options.RegistryName))

		if err := checkRegistryExists(ctx, s.pool, *options.RegistryName); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	params := sqlc.ListServerVersionsParams{
		Name:         options.Name,
		Next:         options.Next,
		Prev:         options.Prev,
		Size:         int64(options.Limit),
		RegistryName: options.RegistryName,
	}

	// Note: this function fetches a list of server versions. In case no records are
	// found, the called function should return an empty slice as it's
	// customary in Go.
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		servers, err := querier.ListServerVersions(ctx, params)
		if err != nil {
			return nil, err
		}

		helpers := make([]helper, 0, len(servers))
		for _, server := range servers {
			helpers = append(helpers, listServerVersionsRowToHelper(server))
		}

		return deduplicateHelpers(helpers), nil
	}

	results, err := s.sharedListServers(ctx, querierFunc)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(results)))
	slog.DebugContext(ctx, "ListServerVersions completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"count", len(results),
		"server_name", options.Name,
		"request_id", middleware.GetReqID(ctx))
	return results, nil
}

// GetServer returns a specific server by name
func (s *dbService) GetServerVersion(
	ctx context.Context,
	opts ...service.Option,
) (*upstreamv0.ServerJSON, error) {
	ctx, span := s.startSpan(ctx, "dbService.GetServerVersion")
	defer span.End()
	start := time.Now()

	options := &service.GetServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrServerName.String(options.Name),
		otel.AttrServerVersion.String(options.Version),
	)
	if options.RegistryName != "" {
		span.SetAttributes(otel.AttrRegistryName.String(options.RegistryName))
	}

	var registryName *string
	if options.RegistryName != "" {
		registryName = &options.RegistryName
		if err := checkRegistryExists(ctx, s.pool, options.RegistryName); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	params := sqlc.GetServerVersionParams{
		Name:         options.Name,
		Version:      options.Version,
		RegistryName: registryName,
	}

	// Note: this function fetches a single record given name and version.
	// In case no record is found, the called function maps the underlying
	// `pgx.ErrNoRows` to `service.ErrNotFound`, and callers should expect
	// to receive `service.ErrNotFound` for a missing record.
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		servers, err := querier.GetServerVersion(ctx, params)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
			}
			return nil, err
		}
		if len(servers) == 0 {
			return nil, pgx.ErrNoRows
		}

		// Return only the first row (highest priority by position)
		return []helper{getServerVersionRowToHelper(servers[0])}, nil
	}

	res, err := s.sharedListServers(ctx, querierFunc)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	if len(res) != 1 {
		err := fmt.Errorf("%w: number of servers returned is not 1", ErrBug)
		otel.RecordError(span, err)
		return nil, err
	}

	slog.DebugContext(ctx, "GetServerVersion completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"server_name", options.Name,
		"version", options.Version,
		"request_id", middleware.GetReqID(ctx))
	return res[0], nil
}

// insertServerVersionData inserts the server version record and returns the entry_version ID.
// It validates unique constraints on (entry_id, version) and returns ErrVersionAlreadyExists if violated.
func insertServerVersionData(
	ctx context.Context,
	querier *sqlc.Queries,
	serverData *upstreamv0.ServerJSON,
	registryID uuid.UUID,
	maxMetaSize int,
) (uuid.UUID, error) {
	// Prepare repository fields
	var repoURL, repoID, repoSubfolder, repoType *string
	if serverData.Repository != nil {
		repoURL = &serverData.Repository.URL
		repoID = &serverData.Repository.ID
		repoSubfolder = &serverData.Repository.Subfolder
		repoType = &serverData.Repository.Source
	}

	// Serialize publisher-provided metadata
	serverMeta, err := serializePublisherProvidedMeta(serverData.Meta, maxMetaSize)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to serialize metadata: %w", err)
	}

	now := time.Now()

	// Get or create the registry entry (one per unique name)
	entryID, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  registryID,
		EntryType: sqlc.EntryTypeMCP,
		Name:      serverData.Name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		entryID, err = querier.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
			SourceID:  registryID,
			EntryType: sqlc.EntryTypeMCP,
			Name:      serverData.Name,
			CreatedAt: &now,
			UpdatedAt: &now,
		})
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get or create registry entry: %w", err)
	}

	// Insert the entry version (one per name+version)
	versionID, err := querier.InsertEntryVersion(ctx, sqlc.InsertEntryVersionParams{
		EntryID:     entryID,
		Version:     serverData.Version,
		Title:       &serverData.Title,
		Description: &serverData.Description,
		CreatedAt:   &now,
		UpdatedAt:   &now,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, fmt.Errorf("%w: %s@%s",
				service.ErrVersionAlreadyExists, serverData.Name, serverData.Version)
		}
		return uuid.Nil, fmt.Errorf("failed to insert entry version: %w", err)
	}

	serverVersionID, err := querier.InsertServerVersion(ctx, sqlc.InsertServerVersionParams{
		VersionID:           versionID,
		Website:             &serverData.WebsiteURL,
		UpstreamMeta:        nil,
		ServerMeta:          serverMeta,
		RepositoryUrl:       repoURL,
		RepositoryID:        repoID,
		RepositorySubfolder: repoSubfolder,
		RepositoryType:      repoType,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to insert server version: %w", err)
	}

	return serverVersionID, nil
}

// insertServerPackages inserts all packages for a server version.
// Each package includes transport configuration, runtime/package arguments, and environment variables.
func insertServerPackages(
	ctx context.Context,
	querier *sqlc.Queries,
	entryID uuid.UUID,
	packages []model.Package,
) error {
	for _, pkg := range packages {
		envVarsJSON, err := serializeKeyValueInputs(pkg.EnvironmentVariables)
		if err != nil {
			return fmt.Errorf("failed to serialize env vars: %w", err)
		}

		transportHeadersJSON, err := serializeKeyValueInputs(pkg.Transport.Headers)
		if err != nil {
			return fmt.Errorf("failed to serialize transport headers: %w", err)
		}

		err = querier.InsertServerPackage(ctx, sqlc.InsertServerPackageParams{
			ServerID:         entryID,
			RegistryType:     pkg.RegistryType,
			PkgRegistryUrl:   pkg.RegistryBaseURL,
			PkgIdentifier:    pkg.Identifier,
			PkgVersion:       pkg.Version,
			RuntimeHint:      &pkg.RunTimeHint,
			RuntimeArguments: extractArgumentValues(pkg.RuntimeArguments),
			PackageArguments: extractArgumentValues(pkg.PackageArguments),
			EnvVars:          envVarsJSON,
			Sha256Hash:       &pkg.FileSHA256,
			Transport:        pkg.Transport.Type,
			TransportUrl:     &pkg.Transport.URL,
			TransportHeaders: transportHeadersJSON,
		})
		if err != nil {
			return fmt.Errorf("failed to insert server package: %w", err)
		}
	}
	return nil
}

// insertServerRemotes inserts all remotes for a server version.
// Remote transports (SSE, streamable-http) require a transport URL.
func insertServerRemotes(
	ctx context.Context,
	querier *sqlc.Queries,
	entryID uuid.UUID,
	remotes []model.Transport,
) error {
	for _, remote := range remotes {
		headersJSON, err := serializeKeyValueInputs(remote.Headers)
		if err != nil {
			return fmt.Errorf("failed to serialize transport headers: %w", err)
		}

		err = querier.InsertServerRemote(ctx, sqlc.InsertServerRemoteParams{
			ServerID:         entryID,
			Transport:        remote.Type,
			TransportUrl:     remote.URL,
			TransportHeaders: headersJSON,
		})
		if err != nil {
			return fmt.Errorf("failed to insert server remote: %w", err)
		}
	}
	return nil
}

// insertServerIcons inserts all icons for a server version.
// Icons include source URI, MIME type, and theme (light/dark) attributes.
func insertServerIcons(
	ctx context.Context,
	querier *sqlc.Queries,
	entryID uuid.UUID,
	icons []model.Icon,
) error {
	for _, icon := range icons {
		// Convert theme string pointer to IconTheme enum
		var theme sqlc.IconTheme
		if icon.Theme != nil {
			switch *icon.Theme {
			case "light":
				theme = sqlc.IconThemeLIGHT
			case "dark":
				theme = sqlc.IconThemeDARK
			default:
				theme = sqlc.IconThemeLIGHT // Default to light if unknown
			}
		} else {
			theme = sqlc.IconThemeLIGHT // Default to light if not specified
		}

		// Get MIME type, default to empty string if not provided
		mimeType := ""
		if icon.MimeType != nil {
			mimeType = *icon.MimeType
		}

		err := querier.InsertServerIcon(ctx, sqlc.InsertServerIconParams{
			ServerID:  entryID,
			SourceUri: icon.Src,
			MimeType:  mimeType,
			Theme:     theme,
		})
		if err != nil {
			return fmt.Errorf("failed to insert server icon: %w", err)
		}
	}
	return nil
}

// getManagedSource finds the managed source from the database.
// Returns ErrNoManagedSource if no managed source exists.
// Returns an error if more than one managed source is found.
func getManagedSource(ctx context.Context, querier *sqlc.Queries) (*sqlc.Source, error) {
	rows, err := querier.GetManagedSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get managed source: %w", err)
	}
	if len(rows) == 0 {
		return nil, service.ErrNoManagedSource
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("expected exactly one managed source, found %d", len(rows))
	}
	row := rows[0]
	return &sqlc.Source{
		ID:           row.ID,
		Name:         row.Name,
		SourceType:   row.SourceType,
		CreationType: row.CreationType,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// PublishServerVersion publishes a server version to a managed registry
func (s *dbService) PublishServerVersion(
	ctx context.Context,
	opts ...service.Option,
) (*upstreamv0.ServerJSON, error) {
	ctx, span := s.startSpan(ctx, "dbService.PublishServerVersion")
	defer span.End()
	start := time.Now()

	// Parse options
	options := &service.PublishServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, fmt.Errorf("invalid option: %w", err)
		}
	}

	if options.ServerData == nil {
		err := fmt.Errorf("server data is required")
		otel.RecordError(span, err)
		return nil, err
	}

	serverData := options.ServerData

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrServerName.String(serverData.Name),
		otel.AttrServerVersion.String(serverData.Version),
	)

	// Defensive check: validate server name format (should never fail if API layer is correct)
	if !validators.IsValidServerName(serverData.Name) {
		err := fmt.Errorf("invalid server name format: %s", serverData.Name)
		otel.RecordError(span, err)
		return nil, err
	}

	// Execute the publish operation in a transaction
	if err := s.executePublishTransaction(ctx, serverData); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	slog.InfoContext(ctx, "Server version published",
		"duration_ms", time.Since(start).Milliseconds(),
		"server", serverData.Name,
		"version", serverData.Version,
		"request_id", middleware.GetReqID(ctx))

	// Fetch the inserted server to return it
	result, err := s.GetServerVersion(ctx,
		service.WithName(serverData.Name),
		service.WithVersion(serverData.Version),
	)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to fetch published server: %w", err)
	}

	return result, nil
}

// executePublishTransaction executes the publish operation within a transaction
func (s *dbService) executePublishTransaction(
	ctx context.Context,
	serverData *upstreamv0.ServerJSON,
) error {
	// Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	// Find the managed source automatically
	registry, err := getManagedSource(ctx, querier)
	if err != nil {
		return err
	}

	// Insert server and related data
	if err := s.insertServerData(ctx, querier, serverData, registry.ID); err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// insertServerData inserts the server version and all related data
func (s *dbService) insertServerData(
	ctx context.Context,
	querier *sqlc.Queries,
	serverData *upstreamv0.ServerJSON,
	registryID uuid.UUID,
) error {
	// Insert the server version
	serverVersionID, err := insertServerVersionData(ctx, querier, serverData, registryID, s.maxMetaSize)
	if err != nil {
		return err
	}

	// Insert packages
	if err := insertServerPackages(ctx, querier, serverVersionID, serverData.Packages); err != nil {
		return err
	}

	// Insert remotes
	if err := insertServerRemotes(ctx, querier, serverVersionID, serverData.Remotes); err != nil {
		return err
	}

	// Insert icons
	if err := insertServerIcons(ctx, querier, serverVersionID, serverData.Icons); err != nil {
		return err
	}

	// Compare with current latest before upserting — avoid regressing the pointer
	shouldUpdateLatest := true
	currentLatest, err := querier.GetLatestVersionForServer(ctx, sqlc.GetLatestVersionForServerParams{
		Name:     serverData.Name,
		SourceID: registryID,
	})
	if err == nil {
		shouldUpdateLatest = versions.IsNewerVersion(serverData.Version, currentLatest)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to get current latest version: %w", err)
	}

	if shouldUpdateLatest {
		_, err = querier.UpsertLatestServerVersion(ctx, sqlc.UpsertLatestServerVersionParams{
			SourceID:  registryID,
			Name:      serverData.Name,
			Version:   serverData.Version,
			VersionID: serverVersionID,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert latest server version: %w", err)
		}
	}

	return nil
}

// DeleteServerVersion removes a server version from a managed registry
func (s *dbService) DeleteServerVersion(
	ctx context.Context,
	opts ...service.Option,
) error {
	ctx, span := s.startSpan(ctx, "dbService.DeleteServerVersion")
	defer span.End()
	start := time.Now()

	options := &service.DeleteServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return fmt.Errorf("invalid option: %w", err)
		}
	}

	span.SetAttributes(
		otel.AttrServerName.String(options.ServerName),
		otel.AttrServerVersion.String(options.Version),
	)

	if err := s.executeDeleteTransaction(ctx, options); err != nil {
		otel.RecordError(span, err)
		return err
	}

	slog.InfoContext(ctx, "Server version deleted",
		"duration_ms", time.Since(start).Milliseconds(),
		"server", options.ServerName,
		"version", options.Version,
		"request_id", middleware.GetReqID(ctx))

	return nil
}

// executeDeleteTransaction runs the version deletion within a serializable transaction.
func (s *dbService) executeDeleteTransaction(
	ctx context.Context,
	options *service.DeleteServerVersionOptions,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	registry, err := getManagedSource(ctx, querier)
	if err != nil {
		return err
	}

	entryID, err := lookupAndDeleteEntryVersion(ctx, querier, registry.ID, sqlc.EntryTypeMCP, options.ServerName, options.Version)
	if err != nil {
		return err
	}

	if err := rePointLatestServerVersionIfNeeded(ctx, querier, registry.ID, options.ServerName, entryID); err != nil {
		return err
	}

	if err := cleanupOrphanedEntry(ctx, querier, entryID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// rePointLatestServerVersionIfNeeded checks whether the latest_entry_version pointer was cascade-deleted
// (because the deleted version was the current latest) and, if so, re-points it to the
// next-highest remaining version according to versions.IsNewerVersion (semantic when possible,
// with lexicographic ordering as a fallback).
func rePointLatestServerVersionIfNeeded(
	ctx context.Context,
	querier *sqlc.Queries,
	registryID uuid.UUID,
	serverName string,
	entryID uuid.UUID,
) error {
	_, err := querier.GetLatestVersionForServer(ctx, sqlc.GetLatestVersionForServerParams{
		Name:  serverName,
		RegID: registryID,
	})
	if err == nil {
		// Pointer still exists — deleted version was not the latest, nothing to do.
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to check latest server version: %w", err)
	}

	// Pointer was cascade-deleted. Find the next-highest remaining version using the same
	// ordering rules as versions.IsNewerVersion (semantic when possible, lexicographic otherwise).
	remaining, err := querier.ListEntryVersions(ctx, entryID)
	if err != nil {
		return fmt.Errorf("failed to list remaining server versions: %w", err)
	}

	newVersionID, newVersion := findHighestVersion(remaining)
	if newVersionID == uuid.Nil {
		// No remaining versions — the cascade on registry_entry will handle full cleanup.
		return nil
	}

	if _, err := querier.UpsertLatestServerVersion(ctx, sqlc.UpsertLatestServerVersionParams{
		RegID:     registryID,
		Name:      serverName,
		Version:   newVersion,
		VersionID: newVersionID,
	}); err != nil {
		return fmt.Errorf("failed to upsert latest server version: %w", err)
	}
	return nil
}

// lookupAndDeleteEntryVersion finds the registry entry by name and entry type,
// then deletes the specified version. Returns the entry ID for potential
// cleanup, or an error if the entry or version is not found.
func lookupAndDeleteEntryVersion(
	ctx context.Context,
	querier *sqlc.Queries,
	registryID uuid.UUID,
	entryType sqlc.EntryType,
	name string,
	version string,
) (uuid.UUID, error) {
	entryID, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  registryID,
		EntryType: entryType,
		Name:      name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: %s@%s", service.ErrNotFound, name, version)
		}
		return uuid.Nil, fmt.Errorf("failed to look up registry entry: %w", err)
	}

	rowsAffected, err := querier.DeleteEntryVersion(ctx, sqlc.DeleteEntryVersionParams{
		EntryID: entryID,
		Version: version,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to delete entry version: %w", err)
	}

	if rowsAffected == 0 {
		return uuid.Nil, fmt.Errorf("%w: %s@%s", service.ErrNotFound, name, version)
	}

	return entryID, nil
}

// cleanupOrphanedEntry removes the parent registry entry if no versions remain.
func cleanupOrphanedEntry(
	ctx context.Context,
	querier *sqlc.Queries,
	entryID uuid.UUID,
) error {
	remaining, err := querier.CountEntryVersions(ctx, entryID)
	if err != nil {
		return fmt.Errorf("failed to count remaining versions: %w", err)
	}
	if remaining == 0 {
		if _, err := querier.DeleteRegistryEntryByID(ctx, entryID); err != nil {
			return fmt.Errorf("failed to delete empty registry entry: %w", err)
		}
	}
	return nil
}

// querierFunction is a function that uses the given querier object to run the
// main extraction. As of the time of this writing, its main use is accessing
// the `mcp_server` table in a type-agnostic way. This is to overcome a
// limitation of sqlc that prevents us from having the exact same go type
// despite the fact that the underlying columns returned are the same.
//
// Note that the underlying table does not have to be the `mcp_server` table
// as it is used now, as long as the result is a slice of helpers.
type querierFunction func(ctx context.Context, querier sqlc.Querier) ([]helper, error)

// sharedListServers is a helper function to list servers and mapping them to
// the API schema.
//
// Its responsibilities are:
// * Begin a transaction
// * Execute the querier function
// * List packages and remotes using the server IDs
// * Map the results to the API schema
// * Return the results
//
// The argument `querierFunc` is a function that uses the given querier object
// to run the main extraction. Note that the underlying table does not have
// to be the `mcp_server` table as it is used now, as long as the result is a
// slice of helpers.
func (s *dbService) sharedListServers(
	ctx context.Context,
	querierFunc querierFunction,
) ([]*upstreamv0.ServerJSON, error) {
	// Delegate to sharedListServersWithCursor with a high limit and discard the cursor.
	// This avoids duplicating the transaction and fetch logic.
	result, _, err := s.sharedListServersWithCursor(ctx, querierFunc, service.MaxPageSize)
	return result, err
}

// sharedListServersWithCursor is similar to sharedListServers but supports cursor-based pagination.
// It takes a limit parameter and returns:
// - The list of servers (up to limit items)
// - The serverCursor of the last server if there are more results (for cursor calculation)
// - An error if the operation fails
//
// The querierFunc should request limit+1 records to allow detecting if there are more results.
func (s *dbService) sharedListServersWithCursor(
	ctx context.Context,
	querierFunc querierFunction,
	limit int,
) ([]*upstreamv0.ServerJSON, *serverCursor, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	servers, err := querierFunc(ctx, querier)
	if err != nil {
		return nil, nil, err
	}

	// Check if there are more results
	var lastCursor *serverCursor
	hasMore := len(servers) > limit
	if hasMore {
		// Trim to the requested limit
		servers = servers[:limit]
		// Get the Name and Version of the last server for the next cursor
		if len(servers) > 0 {
			lastServer := servers[len(servers)-1]
			lastCursor = &serverCursor{
				Name:    lastServer.Name,
				Version: lastServer.Version,
			}
		}
	}

	// Fetch packages and remotes for all servers
	ids := make([]uuid.UUID, len(servers))
	for i, server := range servers {
		ids[i] = server.ID
	}

	packages, err := querier.ListServerPackages(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	packagesMap := make(map[uuid.UUID][]sqlc.ListServerPackagesRow)
	for _, pkg := range packages {
		packagesMap[pkg.ServerID] = append(packagesMap[pkg.ServerID], pkg)
	}

	remotes, err := querier.ListServerRemotes(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	remotesMap := make(map[uuid.UUID][]sqlc.McpServerRemote)
	for _, remote := range remotes {
		remotesMap[remote.ServerID] = append(remotesMap[remote.ServerID], remote)
	}

	result := make([]*upstreamv0.ServerJSON, 0, len(servers))
	for _, dbServer := range servers {
		server, err := helperToServer(
			dbServer,
			packagesMap[dbServer.ID],
			remotesMap[dbServer.ID],
		)
		if err != nil {
			return nil, nil, err
		}
		result = append(result, &server)
	}

	return result, lastCursor, nil
}

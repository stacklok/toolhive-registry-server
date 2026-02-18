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

	"github.com/stacklok/toolhive-registry-server/internal/config"
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

		querier := sqlc.New(s.pool)
		if err := validateRegistryExists(ctx, querier, *options.RegistryName); err != nil {
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
		Size: int64(options.Limit + 1),
	}
	if options.RegistryName != nil {
		params.RegistryName = options.RegistryName
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

	// Note: this function fetches a list of servers. In case no records are
	// found, the called function should return an empty slice as it's
	// customary in Go.
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		servers, err := querier.ListServers(ctx, params)
		if err != nil {
			return nil, err
		}

		helpers := make([]helper, len(servers))
		for i, server := range servers {
			helpers[i] = listServersRowToHelper(server)
		}

		return helpers, nil
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
	}

	if options.RegistryName != nil {
		querier := sqlc.New(s.pool)
		if err := validateRegistryExists(ctx, querier, *options.RegistryName); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	params := sqlc.ListServerVersionsParams{
		Name: options.Name,
		Next: options.Next,
		Prev: options.Prev,
		Size: int64(options.Limit),
	}
	if options.RegistryName != nil {
		params.RegistryName = options.RegistryName
	}

	// Note: this function fetches a list of server versions. In case no records are
	// found, the called function should return an empty slice as it's
	// customary in Go.
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		servers, err := querier.ListServerVersions(ctx, params)
		if err != nil {
			return nil, err
		}

		helpers := make([]helper, len(servers))
		for i, server := range servers {
			helpers[i] = listServerVersionsRowToHelper(server)
		}

		return helpers, nil
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

	if options.RegistryName != "" {
		querier := sqlc.New(s.pool)
		if err := validateRegistryExists(ctx, querier, options.RegistryName); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	params := sqlc.GetServerVersionParams{
		Name:    options.Name,
		Version: options.Version,
	}
	if options.RegistryName != "" {
		params.RegistryName = &options.RegistryName
	}

	// Note: this function fetches a single record given name and version.
	// In case no record is found, the called function should return an
	// `sql.ErrNoRows` error as it's customary in Go.
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		server, err := querier.GetServerVersion(ctx, params)
		if err != nil {
			return nil, err
		}

		return []helper{getServerVersionRowToHelper(server)}, nil
	}

	res, err := s.sharedListServers(ctx, querierFunc)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Note: the `queryFunc` function is expected to return an error
	// sooner if no records are found, so getting this far with
	// a length result slice other than 1 means there's a bug.
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

// insertServerVersionData inserts the server version record and returns the server ID.
// It validates unique constraints on (registry_id, name, version) and returns ErrVersionAlreadyExists if violated.
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

	// Insert the server version
	now := time.Now()
	entryID, err := querier.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
		Name:        serverData.Name,
		Version:     serverData.Version,
		RegID:       registryID,
		EntryType:   sqlc.EntryTypeMCP,
		CreatedAt:   &now,
		UpdatedAt:   &now,
		Description: &serverData.Description,
		Title:       &serverData.Title,
	})
	if err != nil {
		// Check if this is a unique constraint violation
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, fmt.Errorf("%w: %s@%s",
				service.ErrVersionAlreadyExists, serverData.Name, serverData.Version)
		}
		return uuid.Nil, fmt.Errorf("failed to insert server version: %w", err)
	}

	_, err = querier.InsertServerVersion(ctx, sqlc.InsertServerVersionParams{
		EntryID:             entryID,
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

	return entryID, nil
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
			EntryID:          entryID,
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
			EntryID:          entryID,
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
			EntryID:   entryID,
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

// validateRegistryExists validates that the registry exists.
// Returns ErrRegistryNotFound if the registry doesn't exist.
func validateRegistryExists(ctx context.Context, querier *sqlc.Queries, registryName string) error {
	_, err := querier.GetRegistryByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", service.ErrRegistryNotFound, registryName)
		}
		return fmt.Errorf("failed to get registry: %w", err)
	}
	return nil
}

// validateManagedRegistry validates that the registry exists and is a managed (LOCAL) registry.
// Returns ErrRegistryNotFound if the registry doesn't exist, or ErrNotManagedRegistry if it's not a LOCAL type.
func validateManagedRegistry(
	ctx context.Context,
	querier *sqlc.Queries,
	registryName string,
) (*sqlc.Registry, error) {
	registryRow, err := querier.GetRegistryByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", service.ErrRegistryNotFound, registryName)
		}
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	if registryRow.RegType != sqlc.RegistryTypeMANAGED {
		return nil, fmt.Errorf("%w: registry %s has type %s",
			service.ErrNotManagedRegistry, registryName, registryRow.RegType)
	}

	// Convert row to Registry struct
	registry := &sqlc.Registry{
		ID:           registryRow.ID,
		Name:         registryRow.Name,
		RegType:      registryRow.RegType,
		CreationType: registryRow.CreationType,
		CreatedAt:    registryRow.CreatedAt,
		UpdatedAt:    registryRow.UpdatedAt,
	}

	return registry, nil
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
		otel.AttrRegistryName.String(options.RegistryName),
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
	if err := s.executePublishTransaction(ctx, options.RegistryName, serverData); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	slog.InfoContext(ctx, "Server version published",
		"duration_ms", time.Since(start).Milliseconds(),
		"registry", options.RegistryName,
		"server", serverData.Name,
		"version", serverData.Version,
		"request_id", middleware.GetReqID(ctx))

	// Fetch the inserted server to return it
	result, err := s.GetServerVersion(ctx,
		service.WithRegistryName(options.RegistryName),
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
	registryName string,
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

	// Validate registry exists and is managed
	registry, err := validateManagedRegistry(ctx, querier, registryName)
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
	entryID, err := insertServerVersionData(ctx, querier, serverData, registryID, s.maxMetaSize)
	if err != nil {
		return err
	}

	// Insert packages
	if err := insertServerPackages(ctx, querier, entryID, serverData.Packages); err != nil {
		return err
	}

	// Insert remotes
	if err := insertServerRemotes(ctx, querier, entryID, serverData.Remotes); err != nil {
		return err
	}

	// Insert icons
	if err := insertServerIcons(ctx, querier, entryID, serverData.Icons); err != nil {
		return err
	}

	// Compare with current latest before upserting — avoid regressing the pointer
	shouldUpdateLatest := true
	currentLatest, err := querier.GetLatestVersionForServer(ctx, sqlc.GetLatestVersionForServerParams{
		Name:  serverData.Name,
		RegID: registryID,
	})
	if err == nil {
		shouldUpdateLatest = versions.IsNewerVersion(serverData.Version, currentLatest)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to get current latest version: %w", err)
	}

	if shouldUpdateLatest {
		_, err = querier.UpsertLatestServerVersion(ctx, sqlc.UpsertLatestServerVersionParams{
			RegID:   registryID,
			Name:    serverData.Name,
			Version: serverData.Version,
			EntryID: entryID,
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

	// 1. Parse options
	options := &service.DeleteServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return fmt.Errorf("invalid option: %w", err)
		}
	}

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrRegistryName.String(options.RegistryName),
		otel.AttrServerName.String(options.ServerName),
		otel.AttrServerVersion.String(options.Version),
	)

	// 2. Begin transaction
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

	// 3. Validate registry exists and get registry info
	registry, err := querier.GetRegistryByName(ctx, options.RegistryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, options.RegistryName)
			otel.RecordError(span, err)
			return err
		}
		otel.RecordError(span, err)
		return fmt.Errorf("failed to get registry: %w", err)
	}

	// 4. Validate registry is MANAGED type
	if registry.RegType != sqlc.RegistryTypeMANAGED {
		err = fmt.Errorf("%w: registry %s has type %s",
			service.ErrNotManagedRegistry, options.RegistryName, registry.RegType)
		otel.RecordError(span, err)
		return err
	}

	// 5. Delete the server version
	rowsAffected, err := querier.DeleteRegistryEntry(ctx, sqlc.DeleteRegistryEntryParams{
		RegID:   registry.ID,
		Name:    options.ServerName,
		Version: options.Version,
	})
	if err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to delete server version: %w", err)
	}

	// 5.1. Check if the server version was found and deleted
	if rowsAffected == 0 {
		err = fmt.Errorf("%w: %s@%s",
			service.ErrNotFound, options.ServerName, options.Version)
		otel.RecordError(span, err)
		return err
	}

	// 6. Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Server version deleted",
		"duration_ms", time.Since(start).Milliseconds(),
		"registry", options.RegistryName,
		"server", options.ServerName,
		"version", options.Version,
		"request_id", middleware.GetReqID(ctx))

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
		packagesMap[pkg.EntryID] = append(packagesMap[pkg.EntryID], pkg)
	}

	remotes, err := querier.ListServerRemotes(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	remotesMap := make(map[uuid.UUID][]sqlc.McpServerRemote)
	for _, remote := range remotes {
		remotesMap[remote.EntryID] = append(remotesMap[remote.EntryID], remote)
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

	// List all registries (no pagination for now)
	params := sqlc.ListRegistriesParams{
		Size: service.MaxPageSize, // Maximum number of registries to return
	}

	registries, err := querier.ListRegistries(ctx, params)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}

	// Convert to API response format
	result := make([]service.RegistryInfo, 0, len(registries))
	for _, reg := range registries {
		// Build RegistryInfo with all config fields
		info := buildRegistryInfoFromListRow(&reg)

		// Fetch sync status from database
		syncRecord, err := querier.GetRegistrySyncByName(ctx, reg.Name)
		if err != nil {
			// It's okay if sync record doesn't exist yet (registry may not have been synced)
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("Failed to get sync status for registry",
					"registry", reg.Name,
					"error", err)
			}
			// Leave SyncStatus as nil if not found or error
			info.SyncStatus = nil
		} else {
			// Convert database sync status to service type
			info.SyncStatus = &service.RegistrySyncStatus{
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
	slog.DebugContext(ctx, "ListRegistries completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"count", len(result),
		"request_id", middleware.GetReqID(ctx))
	return result, nil
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

	// Get the registry by name
	registry, err := querier.GetRegistryByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	// Build RegistryInfo with all config fields
	info := buildRegistryInfoFromGetByNameRow(&registry)

	// Fetch sync status from database
	syncRecord, err := querier.GetRegistrySyncByName(ctx, registry.Name)
	if err != nil {
		// It's okay if sync record doesn't exist yet (registry may not have been synced)
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("Failed to get sync status for registry",
				"registry", registry.Name,
				"error", err)
		}
		// Leave SyncStatus as nil if not found or error
		info.SyncStatus = nil
	} else {
		// Convert database sync status to service type
		info.SyncStatus = &service.RegistrySyncStatus{
			Phase:        convertSyncPhase(syncRecord.SyncStatus),
			LastSyncTime: syncRecord.EndedAt,   // EndedAt represents successful completion
			LastAttempt:  syncRecord.StartedAt, // StartedAt is the last attempt time
			AttemptCount: int(syncRecord.AttemptCount),
			ServerCount:  int(syncRecord.ServerCount),
			Message:      getStatusMessage(syncRecord.ErrorMsg),
		}
	}

	slog.DebugContext(ctx, "GetRegistryByName completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"registry", name,
		"request_id", middleware.GetReqID(ctx))
	return info, nil
}

// buildRegistryInfoFromListRow builds a RegistryInfo from a ListRegistriesRow
func buildRegistryInfoFromListRow(row *sqlc.ListRegistriesRow) *service.RegistryInfo {
	info := &service.RegistryInfo{
		Name:         row.Name,
		Type:         string(row.RegType),
		CreationType: service.CreationType(row.CreationType),
	}

	if row.SourceType != nil {
		info.SourceType = config.SourceType(*row.SourceType)
	}

	if row.Format != nil {
		info.Format = *row.Format
	}

	if row.SourceType != nil {
		info.SourceConfig = deserializeSourceConfig(*row.SourceType, row.SourceConfig)
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

// buildRegistryInfoFromGetByNameRow builds a RegistryInfo from a GetRegistryByNameRow
func buildRegistryInfoFromGetByNameRow(row *sqlc.GetRegistryByNameRow) *service.RegistryInfo {
	info := &service.RegistryInfo{
		Name:         row.Name,
		Type:         string(row.RegType),
		CreationType: service.CreationType(row.CreationType),
	}

	if row.SourceType != nil {
		info.SourceType = config.SourceType(*row.SourceType)
	}

	if row.Format != nil {
		info.Format = *row.Format
	}

	if row.SourceType != nil {
		info.SourceConfig = deserializeSourceConfig(*row.SourceType, row.SourceConfig)
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

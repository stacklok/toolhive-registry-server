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
	"github.com/jackc/pgx/v5/pgtype"
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
	if options.RegistryName == "" {
		return nil, fmt.Errorf("registry name is required")
	}
	span.SetAttributes(otel.AttrRegistryName.String(options.RegistryName))

	registryID, err := lookupRegistryIDWithGate(ctx, s.pool, options.RegistryName, options.Claims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Request one extra record to detect if there are more results
	params := sqlc.ListServersParams{
		Size:       int64(options.Limit + 1),
		RegistryID: registryID,
	}

	slog.DebugContext(ctx, "ListServers query",
		"limit", options.Limit,
		"registry", options.RegistryName,
		"search", options.Search,
		"cursor", options.Cursor,
		"updated_since", options.UpdatedSince,
		"version", options.Version,
		"request_id", middleware.GetReqID(ctx))
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

	querierFunc := func(ctx context.Context, querier sqlc.Querier, cursor *serverCursor) ([]helper, error) {
		p := params // copy so we can mutate cursor
		if cursor != nil {
			p.CursorName = &cursor.Name
			p.CursorVersion = &cursor.Version
		}
		rows, err := querier.ListServers(ctx, p)
		if err != nil {
			return nil, err
		}
		helpers := make([]helper, 0, len(rows))
		for _, row := range rows {
			helpers = append(helpers, listServersRowToHelper(row))
		}
		return helpers, nil
	}

	claimsFilter := newClaimsFilterWith(
		options.Claims,
		func(record any) ([]byte, bool) {
			h, ok := record.(helper)
			return h.Claims, ok
		},
	)
	results, lastCursor, err := s.sharedListServersWithCursor(ctx, querierFunc, options.Limit, claimsFilter)
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

	// Cap the limit at service.MaxPageSize to prevent potential DoS
	if options.Limit > service.MaxPageSize {
		options.Limit = service.MaxPageSize
	}

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrServerName.String(options.Name),
		otel.AttrPageSize.Int(options.Limit),
	)
	if options.RegistryName == "" {
		return nil, fmt.Errorf("registry name is required")
	}
	span.SetAttributes(otel.AttrRegistryName.String(options.RegistryName))

	registryIDForVersions, err := lookupRegistryIDWithGate(ctx, s.pool, options.RegistryName, options.Claims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	params := sqlc.ListServersParams{
		Name:       &options.Name,
		Size:       int64(options.Limit),
		RegistryID: registryIDForVersions,
	}

	// Note: this function fetches a list of server versions. In case no records are
	// found, the called function should return an empty slice as it's
	// customary in Go.
	querierFunc := func(ctx context.Context, querier sqlc.Querier, _ *serverCursor) ([]helper, error) {
		servers, err := querier.ListServers(ctx, params)
		if err != nil {
			return nil, err
		}

		helpers := make([]helper, 0, len(servers))
		for _, server := range servers {
			helpers = append(helpers, listServersRowToHelper(server))
		}

		return helpers, nil
	}

	claimsFilter := newClaimsFilterWith(
		options.Claims,
		func(record any) ([]byte, bool) {
			h, ok := record.(helper)
			return h.Claims, ok
		},
	)
	results, err := s.sharedListServers(ctx, querierFunc, claimsFilter)
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

	if options.RegistryName == "" {
		return nil, fmt.Errorf("registry name is required")
	}

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrServerName.String(options.Name),
		otel.AttrServerVersion.String(options.Version),
		otel.AttrRegistryName.String(options.RegistryName),
	)

	registryID, err := lookupRegistryIDWithGate(ctx, s.pool, options.RegistryName, options.Claims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Note: this function fetches a single record given name and version.
	// In case no record is found, the called function maps the underlying
	// `pgx.ErrNoRows` to `service.ErrNotFound`, and callers should expect
	// to receive `service.ErrNotFound` for a missing record.
	querierFunc := func(ctx context.Context, querier sqlc.Querier, cursor *serverCursor) ([]helper, error) {
		p := sqlc.GetServerVersionParams{
			Name:       options.Name,
			Version:    options.Version,
			RegistryID: registryID,
			Size:       int64(service.MaxPageSize) + 1,
		}
		if options.SourceName != "" {
			p.SourceName = &options.SourceName
		}
		if cursor != nil {
			p.CursorPosition = pgtype.Int4{Int32: cursor.Position, Valid: true}
			p.CursorSourceID = &cursor.SourceID
		}
		servers, err := querier.GetServerVersion(ctx, p)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
			}
			return nil, err
		}
		if len(servers) == 0 {
			return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
		}

		// Return all rows ordered by position so the claims filter can promote
		// lower-priority sources when higher-priority ones fail the claims check.
		helpers := make([]helper, len(servers))
		for i, sv := range servers {
			helpers[i] = getServerVersionRowToHelper(sv)
		}
		return helpers, nil
	}

	claimsFilter := newClaimsFilterWith(
		options.Claims,
		func(record any) ([]byte, bool) {
			h, ok := record.(helper)
			return h.Claims, ok
		},
	)
	res, err := s.sharedListServers(ctx, querierFunc, claimsFilter)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	if len(res) == 0 {
		err := fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
		otel.RecordError(span, err)
		return nil, err
	}
	if len(res) > 1 {
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
// When claimsJSON is non-nil it is stored on new entries and verified against existing entries.
func insertServerVersionData(
	ctx context.Context,
	querier *sqlc.Queries,
	serverData *upstreamv0.ServerJSON,
	registryID uuid.UUID,
	maxMetaSize int,
	claimsJSON []byte,
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
	var entryID uuid.UUID
	existing, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  registryID,
		EntryType: sqlc.EntryTypeMCP,
		Name:      serverData.Name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		entryID, err = querier.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
			SourceID:  registryID,
			EntryType: sqlc.EntryTypeMCP,
			Name:      serverData.Name,
			Claims:    claimsJSON,
			CreatedAt: &now,
			UpdatedAt: &now,
		})
	} else if err == nil {
		entryID = existing.ID
		// Verify claim consistency: if both the existing entry and the new
		// publish request carry claims, they must match.
		if claimsJSON != nil && existing.Claims != nil {
			var existingClaims, incoming map[string]any
			_ = json.Unmarshal(existing.Claims, &existingClaims)
			_ = json.Unmarshal(claimsJSON, &incoming)
			if !claimsContain(incoming, existingClaims) {
				return uuid.Nil, fmt.Errorf("%w: claims do not match existing entry", service.ErrClaimsMismatch)
			}
		}
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get or create registry entry: %w", err)
	}

	// Insert the entry version (one per name+version)
	versionID, err := querier.InsertEntryVersion(ctx, sqlc.InsertEntryVersionParams{
		EntryID:     entryID,
		Name:        serverData.Name,
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

	// Validate published claims are a subset of the publisher's JWT claims
	if err := validateClaimsSubset(ctx, options.JWTClaims, options.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Serialize claims to JSON for storage
	var claimsJSON []byte
	if options.Claims != nil {
		var err error
		claimsJSON, err = json.Marshal(options.Claims)
		if err != nil {
			otel.RecordError(span, err)
			return nil, fmt.Errorf("failed to serialize claims: %w", err)
		}
	}

	// Execute the publish operation in a transaction
	sourceName, err := s.executePublishTransaction(ctx, serverData, claimsJSON)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	slog.InfoContext(ctx, "Server version published",
		"duration_ms", time.Since(start).Milliseconds(),
		"server", serverData.Name,
		"version", serverData.Version,
		"request_id", middleware.GetReqID(ctx))

	// Fetch the inserted server to return it
	result, err := s.fetchServerVersionBySource(ctx, serverData.Name, serverData.Version, sourceName)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to fetch published server: %w", err)
	}

	return result, nil
}

// fetchServerVersionBySource retrieves a server version using the source name directly,
// bypassing registry filtering. Used by the publish fetch-back path.
func (s *dbService) fetchServerVersionBySource(
	ctx context.Context,
	name, version, sourceName string,
) (*upstreamv0.ServerJSON, error) {
	querier := sqlc.New(s.pool)
	row, err := querier.GetServerVersionBySourceName(ctx, sqlc.GetServerVersionBySourceNameParams{
		Name:       name,
		Version:    version,
		SourceName: sourceName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, name, version)
		}
		return nil, err
	}

	h := getServerVersionBySourceNameRowToHelper(row)

	versionIDs := []uuid.UUID{row.ID}
	packages, err := querier.ListServerPackages(ctx, versionIDs)
	if err != nil {
		return nil, err
	}
	remotes, err := querier.ListServerRemotes(ctx, versionIDs)
	if err != nil {
		return nil, err
	}

	server, err := helperToServer(h, packages, remotes)
	if err != nil {
		return nil, err
	}
	return &server, nil
}

// executePublishTransaction executes the publish operation within a transaction
func (s *dbService) executePublishTransaction(
	ctx context.Context,
	serverData *upstreamv0.ServerJSON,
	claimsJSON []byte,
) (string, error) {
	// Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.WarnContext(ctx, "Failed to rollback transaction", "error", err)
		}
	}()

	querier := sqlc.New(tx)

	// Find the managed source automatically
	source, err := getManagedSource(ctx, querier)
	if err != nil {
		return "", err
	}

	// Insert server and related data
	if err := s.insertServerData(ctx, querier, serverData, source.ID, claimsJSON); err != nil {
		return "", err
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return source.Name, nil
}

// insertServerData inserts the server version and all related data
func (s *dbService) insertServerData(
	ctx context.Context,
	querier *sqlc.Queries,
	serverData *upstreamv0.ServerJSON,
	registryID uuid.UUID,
	claimsJSON []byte,
) error {
	// Insert the server version
	serverVersionID, err := insertServerVersionData(ctx, querier, serverData, registryID, s.maxMetaSize, claimsJSON)
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
	currentLatest, err := querier.GetLatestEntryVersion(ctx, sqlc.GetLatestEntryVersionParams{
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

	source, err := getManagedSource(ctx, querier)
	if err != nil {
		return err
	}

	// Verify the caller's JWT claims cover the entry's claims before deleting
	if options.JWTClaims != nil {
		existing, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
			SourceID:  source.ID,
			EntryType: sqlc.EntryTypeMCP,
			Name:      options.ServerName,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s@%s", service.ErrNotFound, options.ServerName, options.Version)
			}
			return fmt.Errorf("failed to look up registry entry: %w", err)
		}
		if err := validateClaimsSubsetBytes(ctx, options.JWTClaims, existing.Claims); err != nil {
			return err
		}
	}

	entryID, err := lookupAndDeleteEntryVersion(ctx, querier, source.ID, sqlc.EntryTypeMCP, options.ServerName, options.Version)
	if err != nil {
		return err
	}

	if err := rePointLatestVersionIfNeeded(ctx, querier, source.ID, options.ServerName, entryID,
		func(ctx context.Context, querier *sqlc.Queries, sourceID uuid.UUID, name, version string, versionID uuid.UUID) error {
			_, err := querier.UpsertLatestServerVersion(ctx, sqlc.UpsertLatestServerVersionParams{
				SourceID:  sourceID,
				Name:      name,
				Version:   version,
				VersionID: versionID,
			})
			return err
		}); err != nil {
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

// lookupAndDeleteEntryVersion finds the registry entry by name and entry type,
// then deletes the specified version. Returns the entry ID for potential
// cleanup, or an error if the entry or version is not found.
func lookupAndDeleteEntryVersion(
	ctx context.Context,
	querier *sqlc.Queries,
	sourceID uuid.UUID,
	entryType sqlc.EntryType,
	name string,
	version string,
) (uuid.UUID, error) {
	existing, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  sourceID,
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
		EntryID: existing.ID,
		Version: version,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to delete entry version: %w", err)
	}

	if rowsAffected == 0 {
		return uuid.Nil, fmt.Errorf("%w: %s@%s", service.ErrNotFound, name, version)
	}

	return existing.ID, nil
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

// claimsContain reports whether callerClaims satisfies every claim in recordClaims.
// For each key K in recordClaims the caller must have K, and every value required
// by the record must appear in the caller's value(s) for K.
// Both plain strings and []string values are supported.
func claimsContain(caller, record map[string]any) bool {
	for k, rv := range record {
		cv, ok := caller[k]
		if !ok {
			return false
		}
		required := toStringSet(rv)
		have := toStringSet(cv)
		for v := range required {
			if _, found := have[v]; !found {
				return false
			}
		}
	}
	return true
}

// toStringSet normalises a claim value (string or []any of strings) to a set.
func toStringSet(v any) map[string]struct{} {
	switch val := v.(type) {
	case string:
		return map[string]struct{}{val: {}}
	case []any:
		s := make(map[string]struct{}, len(val))
		for _, elem := range val {
			if str, ok := elem.(string); ok {
				s[str] = struct{}{}
			}
		}
		return s
	default:
		return map[string]struct{}{}
	}
}

// querierFunction is a function that uses the given querier object to run the
// main extraction. As of the time of this writing, its main use is accessing
// the `mcp_server` table in a type-agnostic way. This is to overcome a
// limitation of sqlc that prevents us from having the exact same go type
// despite the fact that the underlying columns returned are the same.
//
// Note that the underlying table does not have to be the `mcp_server` table
// as it is used now, as long as the result is a slice of helpers.
type querierFunction func(ctx context.Context, querier sqlc.Querier, cursor *serverCursor) ([]helper, error)

// newDeduplicatingFilterWith returns a stateful RecordFilter that deduplicates records
// by entry name, keeping only records from the highest-priority source (lowest position).
// SQL must return records in position-ascending order per name.
// extract retrieves the name and source position from a record; returning ok=false causes
// the filter to reject the record with a type error.
func newDeduplicatingFilterWith(
	extract func(record any) (name string, pos int32, ok bool),
) service.RecordFilter {
	seen := make(map[string]int32) // name → winning source position
	return func(_ context.Context, record any) (bool, error) {
		name, pos, ok := extract(record)
		if !ok {
			return false, fmt.Errorf("unexpected record type: %T", record)
		}
		winPos, exists := seen[name]
		if !exists {
			seen[name] = pos
			return true, nil
		}
		return pos == winPos, nil
	}
}

// streamHelpers fetches helpers in batches, applying the auth filter then the dedup
// filter to each record, until limit+1 records are accumulated or the DB is exhausted.
// It returns the trimmed slice (≤ limit) and the cursor for the next page, if any.
func streamHelpers(
	ctx context.Context,
	querier *sqlc.Queries,
	querierFunc querierFunction,
	filter service.RecordFilter,
	limit int,
) ([]helper, *serverCursor, error) {
	dedupFilter := newDeduplicatingFilter()
	var accumulated []helper
	var cursor *serverCursor

	for {
		batch, err := querierFunc(ctx, querier, cursor)
		if err != nil {
			return nil, nil, err
		}

		for _, h := range batch {
			keep := true
			var ferr error
			if filter != nil {
				keep, ferr = filter(ctx, h)
				if ferr != nil {
					return nil, nil, ferr
				}
			}
			if keep {
				keep, ferr = dedupFilter(ctx, h)
				if ferr != nil {
					return nil, nil, ferr
				}
			}
			if keep {
				accumulated = append(accumulated, h)
			}
		}

		if len(accumulated) >= limit+1 || len(batch) < limit+1 {
			break
		}

		// Advance cursor to continue fetching
		if len(batch) > 0 {
			lastRow := batch[len(batch)-1]
			cursor = &serverCursor{
				Name:     lastRow.Name,
				Version:  lastRow.Version,
				Position: lastRow.Position,
				SourceID: lastRow.SourceID,
			}
		}
	}

	// Trim to limit and compute next-page cursor
	var lastCursor *serverCursor
	if len(accumulated) > limit {
		last := accumulated[limit-1]
		lastCursor = &serverCursor{
			Name:     last.Name,
			Version:  last.Version,
			Position: last.Position,
			SourceID: last.SourceID,
		}
		accumulated = accumulated[:limit]
	}

	return accumulated, lastCursor, nil
}

// newDeduplicatingFilter returns a stateful RecordFilter that deduplicates helpers
// by entry name, keeping only records from the highest-priority source (lowest position).
func newDeduplicatingFilter() service.RecordFilter {
	return newDeduplicatingFilterWith(
		func(record any) (string, int32, bool) {
			h, ok := record.(helper)
			return h.Name, h.Position, ok
		},
	)
}

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
	filter service.RecordFilter,
) ([]*upstreamv0.ServerJSON, error) {
	// Delegate to sharedListServersWithCursor with a high limit and discard the cursor.
	// This avoids duplicating the transaction and fetch logic.
	result, _, err := s.sharedListServersWithCursor(ctx, querierFunc, service.MaxPageSize, filter)
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
	filter service.RecordFilter,
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

	accumulated, lastCursor, err := streamHelpers(ctx, querier, querierFunc, filter, limit)
	if err != nil {
		return nil, nil, err
	}

	result, err := fetchAndMapServers(ctx, querier, accumulated)
	if err != nil {
		return nil, nil, err
	}

	return result, lastCursor, nil
}

// fetchAndMapServers fetches packages and remotes for the given server helpers and
// maps them to the API schema.
func fetchAndMapServers(
	ctx context.Context,
	querier *sqlc.Queries,
	servers []helper,
) ([]*upstreamv0.ServerJSON, error) {
	ids := make([]uuid.UUID, len(servers))
	for i, server := range servers {
		ids[i] = server.ID
	}

	packages, err := querier.ListServerPackages(ctx, ids)
	if err != nil {
		return nil, err
	}
	packagesMap := make(map[uuid.UUID][]sqlc.ListServerPackagesRow)
	for _, pkg := range packages {
		packagesMap[pkg.ServerID] = append(packagesMap[pkg.ServerID], pkg)
	}

	remotes, err := querier.ListServerRemotes(ctx, ids)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		result = append(result, &server)
	}

	return result, nil
}

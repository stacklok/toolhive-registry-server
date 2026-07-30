// Package database provides the database-backed implementation of the plugin
// catalog operations on the RegistryService interface.
package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/versions"
)

// ListPlugins returns plugins in the registry with cursor-based pagination.
//
//nolint:gocyclo
func (s *dbService) ListPlugins(
	ctx context.Context,
	opts ...service.Option,
) (*service.ListPluginsResult, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListPlugins")
	defer span.End()

	options := &service.ListPluginsOptions{
		Limit: service.DefaultPageSize,
	}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	if options.RegistryName == "" {
		return nil, fmt.Errorf("registry name is required")
	}

	span.SetAttributes(otel.AttrRegistryName.String(options.RegistryName))

	if options.Limit > service.MaxPageSize {
		options.Limit = service.MaxPageSize
	}

	gateClaims := options.Claims
	if s.skipAuthz {
		gateClaims = nil
	}
	registryID, err := lookupRegistryIDWithGate(ctx, s.pool, options.RegistryName, gateClaims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	querier := sqlc.New(s.pool)

	params := sqlc.ListPluginsParams{
		RegistryID: registryID,
		Size:       int64(options.Limit + 1),
	}
	if options.Namespace != "" {
		params.Namespace = &options.Namespace
	}
	if options.Name != nil {
		params.Name = options.Name
	}
	if options.Search != nil {
		params.Search = options.Search
	}
	if options.Cursor != nil {
		cursorName, cursorVersion, err := service.DecodeCursor(*options.Cursor)
		if err != nil {
			otel.RecordError(span, err)
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		params.CursorName = &cursorName
		params.CursorVersion = &cursorVersion
	}

	claimsFilter := newClaimsFilterWith(
		ctx, options.Claims,
		func(record any) ([]byte, bool) {
			r, ok := record.(sqlc.ListPluginsRow)
			return r.Claims, ok
		},
	)
	if s.skipAuthz {
		claimsFilter = nil
	}
	listRows, nextCursor, err := streamPluginRows(ctx, querier, params, claimsFilter, options.Limit)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	packages, err := fetchPluginPackages(ctx, querier, listRows)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	plugins := make([]*service.Plugin, len(listRows))
	for i, row := range listRows {
		plugin := service.ListPluginsRowToPlugin(row)
		plugin.Packages = packages[row.VersionID]
		plugins[i] = plugin
	}

	return &service.ListPluginsResult{
		Plugins:    plugins,
		NextCursor: nextCursor,
	}, nil
}

// fetchPluginPackages fetches OCI and Git packages for the given plugin rows and
// returns them keyed by plugin version ID.
func fetchPluginPackages(
	ctx context.Context,
	querier *sqlc.Queries,
	rows []sqlc.ListPluginsRow,
) (map[uuid.UUID][]service.PluginPackage, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.VersionID
	}

	ociPackages, err := querier.ListPluginOciPackages(ctx, ids)
	if err != nil {
		return nil, err
	}
	gitPackages, err := querier.ListPluginGitPackages(ctx, ids)
	if err != nil {
		return nil, err
	}

	packages := make(map[uuid.UUID][]service.PluginPackage)
	for _, pkg := range ociPackages {
		packages[pkg.PluginID] = append(packages[pkg.PluginID], toServicePluginOciPackage(pkg))
	}
	for _, pkg := range gitPackages {
		packages[pkg.PluginID] = append(packages[pkg.PluginID], toServicePluginGitPackage(pkg))
	}

	return packages, nil
}

// GetPluginVersion returns a specific plugin version by name and version.
//
//nolint:gocyclo
func (s *dbService) GetPluginVersion(
	ctx context.Context,
	opts ...service.Option,
) (*service.Plugin, error) {
	ctx, span := s.startSpan(ctx, "dbService.GetPluginVersion")
	defer span.End()

	options := &service.GetPluginVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	if options.RegistryName == "" {
		return nil, fmt.Errorf("registry name is required")
	}
	if options.Name == "" || options.Version == "" {
		return nil, fmt.Errorf("name and version are required")
	}
	if options.Namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	span.SetAttributes(otel.AttrRegistryName.String(options.RegistryName))

	gateClaims := options.Claims
	if s.skipAuthz {
		gateClaims = nil
	}
	registryID, err := lookupRegistryIDWithGate(ctx, s.pool, options.RegistryName, gateClaims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	querier := sqlc.New(s.pool)

	params := sqlc.GetPluginVersionParams{
		Name:       options.Name,
		Version:    options.Version,
		Namespace:  &options.Namespace,
		RegistryID: registryID,
		Size:       int64(service.MaxPageSize) + 1,
	}
	if options.SourceName != "" {
		params.SourceName = &options.SourceName
	}

	rows, err := querier.GetPluginVersion(ctx, params)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
	}

	// Iterate rows in priority order (position ascending) and pick the first
	// one that passes the claims check, promoting lower-priority sources when
	// higher-priority ones fail. The filter is nil for skipAuthz, anonymous,
	// and super-admin callers (uniform bypass — see newClaimsFilterWith), in
	// which case the highest-priority row wins outright.
	claimsFilter := newClaimsFilterWith(
		ctx, options.Claims,
		func(record any) ([]byte, bool) {
			r, ok := record.(sqlc.GetPluginVersionRow)
			return r.Claims, ok
		},
	)
	if s.skipAuthz {
		claimsFilter = nil
	}
	var row sqlc.GetPluginVersionRow
	found := false
	for _, r := range rows {
		if claimsFilter == nil {
			row = r
			found = true
			break
		}
		ok, err := claimsFilter(ctx, r)
		if err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
		if ok {
			row = r
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
	}

	ociPackages, err := querier.ListPluginOciPackages(ctx, []uuid.UUID{row.PluginVersionID})
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	gitPackages, err := querier.ListPluginGitPackages(ctx, []uuid.UUID{row.PluginVersionID})
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	packages := make([]service.PluginPackage, 0)
	for _, pkg := range ociPackages {
		packages = append(packages, toServicePluginOciPackage(pkg))
	}
	for _, pkg := range gitPackages {
		packages = append(packages, toServicePluginGitPackage(pkg))
	}

	res := service.GetPluginVersionRowToPlugin(row)
	res.Packages = packages
	return res, nil
}

func toServicePluginOciPackage(pkg sqlc.PluginOciPackage) service.PluginPackage {
	digest := ""
	mediaType := ""
	if pkg.Digest != nil {
		digest = *pkg.Digest
	}
	if pkg.MediaType != nil {
		mediaType = *pkg.MediaType
	}
	return service.PluginPackage{
		RegistryType: service.PluginPackageTypeOCI,
		Identifier:   pkg.Identifier,
		Digest:       digest,
		MediaType:    mediaType,
	}
}

func toServicePluginGitPackage(pkg sqlc.PluginGitPackage) service.PluginPackage {
	ref := ""
	commit := ""
	subfolder := ""
	if pkg.Ref != nil {
		ref = *pkg.Ref
	}
	if pkg.CommitSha != nil {
		commit = *pkg.CommitSha
	}
	if pkg.Subfolder != nil {
		subfolder = *pkg.Subfolder
	}
	return service.PluginPackage{
		RegistryType: service.PluginPackageTypeGit,
		URL:          pkg.Url,
		Ref:          ref,
		Commit:       commit,
		Subfolder:    subfolder,
	}
}

// PublishPlugin inserts a new plugin version into a managed registry.
func (s *dbService) PublishPlugin(
	ctx context.Context,
	plugin *service.Plugin,
	opts ...service.Option,
) (*service.Plugin, error) {
	ctx, span := s.startSpan(ctx, "dbService.PublishPlugin")
	defer span.End()

	options := &service.PublishPluginOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}
	if plugin.Namespace == "" || plugin.Name == "" || plugin.Version == "" {
		return nil, fmt.Errorf("namespace, name, and version are required")
	}

	// Validate published claims are a subset of the publisher's JWT claims
	gateClaims := options.JWTClaims
	if s.skipAuthz {
		gateClaims = nil
	}
	if err := validateClaimsSubset(ctx, gateClaims, options.Claims); err != nil {
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

	sourceName, err := s.executePublishPluginTransaction(ctx, plugin, claimsJSON, gateClaims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	result, err := s.fetchPluginVersionBySource(ctx, plugin.Name, plugin.Version, sourceName)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to fetch published plugin: %w", err)
	}
	return result, nil
}

// fetchPluginVersionBySource retrieves a plugin version using the source name directly,
// bypassing registry filtering. Used by the publish fetch-back path.
func (s *dbService) fetchPluginVersionBySource(
	ctx context.Context,
	name, version, sourceName string,
) (*service.Plugin, error) {
	querier := sqlc.New(s.pool)
	row, err := querier.GetPluginVersionBySourceName(ctx, sqlc.GetPluginVersionBySourceNameParams{
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

	ociPackages, err := querier.ListPluginOciPackages(ctx, []uuid.UUID{row.PluginVersionID})
	if err != nil {
		return nil, err
	}
	gitPackages, err := querier.ListPluginGitPackages(ctx, []uuid.UUID{row.PluginVersionID})
	if err != nil {
		return nil, err
	}

	packages := make([]service.PluginPackage, 0, len(ociPackages)+len(gitPackages))
	for _, pkg := range ociPackages {
		packages = append(packages, toServicePluginOciPackage(pkg))
	}
	for _, pkg := range gitPackages {
		packages = append(packages, toServicePluginGitPackage(pkg))
	}

	result := service.GetPluginVersionRowToPlugin(sqlc.GetPluginVersionRow{
		RegistryType:    row.RegistryType,
		ID:              row.ID,
		Name:            row.Name,
		Version:         row.Version,
		IsLatest:        row.IsLatest,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Description:     row.Description,
		Title:           row.Title,
		PluginVersionID: row.PluginVersionID,
		Namespace:       row.Namespace,
		Status:          row.Status,
		License:         row.License,
		Repository:      row.Repository,
		Icons:           row.Icons,
		Metadata:        row.Metadata,
		ExtensionMeta:   row.ExtensionMeta,
		Claims:          row.Claims,
		Position:        row.Position,
	})
	result.Packages = packages
	return result, nil
}

// executePublishPluginTransaction executes the plugin publish operation within a transaction.
// Returns the managed source name for fetch-back, or an error.
//
//nolint:gocyclo
func (s *dbService) executePublishPluginTransaction(
	ctx context.Context,
	plugin *service.Plugin,
	claimsJSON []byte,
	gateClaims map[string]any,
) (string, error) {
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

	managedSource, err := getManagedSource(ctx, querier)
	if err != nil {
		return "", err
	}

	// Verify the caller may publish into this source: their JWT must cover the
	// source's claims (visibility / OR — auth.md §3/§5). An untagged managed
	// source is publishable only by super-admin (default-deny, #845).
	if err := validateClaimsVisibleBytes(ctx, gateClaims, managedSource.Claims); err != nil {
		return "", err
	}
	sourceName := managedSource.Name

	now := time.Now().UTC()

	// Get or create the registry entry (one per unique name)
	var entryID uuid.UUID
	existing, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  managedSource.ID,
		EntryType: sqlc.EntryTypePLUGIN,
		Name:      plugin.Name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		entryID, err = querier.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
			SourceID:  managedSource.ID,
			EntryType: sqlc.EntryTypePLUGIN,
			Name:      plugin.Name,
			Claims:    claimsJSON,
			CreatedAt: &now,
			UpdatedAt: &now,
		})
	} else if err == nil {
		entryID = existing.ID
		if err := checkClaimConsistency(claimsJSON, existing.Claims); err != nil {
			return "", err
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to get or create registry entry: %w", err)
	}

	// Insert the entry version (one per name+version)
	versionParams := sqlc.InsertEntryVersionParams{
		EntryID:   entryID,
		Name:      plugin.Name,
		Version:   plugin.Version,
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	if plugin.Title != "" {
		versionParams.Title = &plugin.Title
	}
	if plugin.Description != "" {
		versionParams.Description = &plugin.Description
	}

	versionID, err := querier.InsertEntryVersion(ctx, versionParams)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", fmt.Errorf("%w: %s %s", service.ErrVersionAlreadyExists, plugin.Name, plugin.Version)
		}
		return "", err
	}

	pluginParams, err := makeInsertPluginVersionParams(versionID, plugin)
	if err != nil {
		return "", err
	}

	_, err = querier.InsertPluginVersion(ctx, *pluginParams)
	if err != nil {
		return "", err
	}

	for _, pkg := range plugin.Packages {
		var err error
		switch pkg.RegistryType {
		case service.PluginPackageTypeOCI:
			err = querier.InsertPluginOciPackage(ctx, sqlc.InsertPluginOciPackageParams{
				PluginID:   versionID,
				Identifier: pkg.Identifier,
				Digest:     &pkg.Digest,
				MediaType:  &pkg.MediaType,
			})
		case service.PluginPackageTypeGit:
			err = querier.InsertPluginGitPackage(ctx, sqlc.InsertPluginGitPackageParams{
				PluginID:  versionID,
				Url:       pkg.URL,
				Ref:       &pkg.Ref,
				CommitSha: &pkg.Commit,
				Subfolder: &pkg.Subfolder,
			})
		}
		if err != nil {
			return "", err
		}
	}

	// Compare with current latest before upserting — avoid regressing the pointer
	shouldUpdateLatest := true
	currentLatest, err := querier.GetLatestEntryVersion(ctx, sqlc.GetLatestEntryVersionParams{
		Name:     plugin.Name,
		SourceID: managedSource.ID,
	})
	if err == nil {
		shouldUpdateLatest = versions.IsNewerVersion(plugin.Version, currentLatest)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("failed to get current latest version: %w", err)
	}

	if shouldUpdateLatest {
		_, err = querier.UpsertLatestPluginVersion(ctx, sqlc.UpsertLatestPluginVersionParams{
			SourceID:  managedSource.ID,
			Name:      plugin.Name,
			Version:   plugin.Version,
			VersionID: versionID,
		})
		if err != nil {
			return "", fmt.Errorf("failed to upsert latest plugin version: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return sourceName, nil
}

func makeInsertPluginVersionParams(
	versionID uuid.UUID,
	plugin *service.Plugin,
) (*sqlc.InsertPluginVersionParams, error) {

	status := sqlc.NullPluginStatus{}
	if plugin.Status != "" {
		status = sqlc.NullPluginStatus{
			PluginStatus: sqlc.PluginStatus(strings.ToUpper(plugin.Status)),
			Valid:        true,
		}
	}

	repository, err := json.Marshal(plugin.Repository)
	if err != nil {
		return nil, err
	}
	icons, err := json.Marshal(plugin.Icons)
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(plugin.Metadata)
	if err != nil {
		return nil, err
	}
	extensionMeta, err := json.Marshal(plugin.Meta)
	if err != nil {
		return nil, err
	}

	pluginParams := sqlc.InsertPluginVersionParams{
		VersionID:     versionID,
		Namespace:     plugin.Namespace,
		Status:        status,
		Repository:    repository,
		Icons:         icons,
		Metadata:      metadata,
		ExtensionMeta: extensionMeta,
	}
	if plugin.License != "" {
		pluginParams.License = &plugin.License
	}

	return &pluginParams, nil
}

// DeletePluginVersion removes a plugin version from a managed registry.
func (s *dbService) DeletePluginVersion(
	ctx context.Context,
	opts ...service.Option,
) error {
	ctx, span := s.startSpan(ctx, "dbService.DeletePluginVersion")
	defer span.End()

	options := &service.DeletePluginVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return err
		}
	}

	if err := s.executeDeletePluginTransaction(ctx, options); err != nil {
		otel.RecordError(span, err)
		return err
	}

	return nil
}

// executeDeletePluginTransaction runs the plugin version deletion within a serializable transaction.
func (s *dbService) executeDeletePluginTransaction(
	ctx context.Context,
	options *service.DeletePluginVersionOptions,
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

	// Verify the caller's JWT claims cover the entry's claims before deleting
	if options.JWTClaims != nil {
		existing, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
			SourceID:  registry.ID,
			EntryType: sqlc.EntryTypePLUGIN,
			Name:      options.Name,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s@%s", service.ErrNotFound, options.Name, options.Version)
			}
			return fmt.Errorf("failed to look up registry entry: %w", err)
		}
		gateClaims := options.JWTClaims
		if s.skipAuthz {
			gateClaims = nil
		}
		if err := validateClaimsVisibleBytes(ctx, gateClaims, existing.Claims); err != nil {
			return err
		}
	}

	entryID, err := lookupAndDeleteEntryVersion(
		ctx,
		querier,
		registry.ID,
		sqlc.EntryTypePLUGIN,
		options.Name,
		options.Version,
	)
	if err != nil {
		return err
	}

	if err := rePointLatestVersionIfNeeded(ctx, querier, registry.ID, options.Name, entryID,
		func(
			ctx context.Context,
			querier *sqlc.Queries,
			sourceID uuid.UUID,
			name string,
			version string,
			versionID uuid.UUID,
		) error {
			_, err := querier.UpsertLatestPluginVersion(ctx, sqlc.UpsertLatestPluginVersionParams{
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

// streamPluginRows fetches plugin rows in batches, applying the auth filter then the
// dedup filter to each record, until limit+1 rows are accumulated or the DB is
// exhausted. It returns the trimmed slice (≤ limit) and the encoded cursor for the
// next page, if any.
func streamPluginRows(
	ctx context.Context,
	querier *sqlc.Queries,
	params sqlc.ListPluginsParams,
	filter service.RecordFilter,
	limit int,
) ([]sqlc.ListPluginsRow, string, error) {
	dedupFilter := newDeduplicatingPluginFilter()
	var accumulated []sqlc.ListPluginsRow
	batchParams := params

	for {
		batch, err := querier.ListPlugins(ctx, batchParams)
		if err != nil {
			return nil, "", err
		}

		for _, row := range batch {
			keep := true
			var ferr error
			if filter != nil {
				keep, ferr = filter(ctx, row)
				if ferr != nil {
					return nil, "", ferr
				}
			}
			if keep {
				keep, ferr = dedupFilter(ctx, row)
				if ferr != nil {
					return nil, "", ferr
				}
			}
			if keep {
				accumulated = append(accumulated, row)
			}
		}

		if len(accumulated) >= limit+1 || int64(len(batch)) < batchParams.Size {
			break
		}

		lastRow := batch[len(batch)-1]
		batchParams.CursorName = &lastRow.Name
		batchParams.CursorVersion = &lastRow.Version
	}

	nextCursor := ""
	if len(accumulated) > limit {
		last := accumulated[limit-1]
		nextCursor = service.EncodeCursor(last.Name, last.Version)
		accumulated = accumulated[:limit]
	}

	return accumulated, nextCursor, nil
}

// newDeduplicatingPluginFilter returns a stateful RecordFilter that deduplicates
// plugin rows by entry name, keeping only records from the highest-priority source
// (lowest position). SQL must return records in position-ascending order per name.
func newDeduplicatingPluginFilter() service.RecordFilter {
	return newDeduplicatingFilterWith(
		func(record any) (string, int32, bool) {
			r, ok := record.(sqlc.ListPluginsRow)
			return r.Name, r.Position, ok
		},
	)
}

// Package database provides the database-backed implementation of the SkillService interface.
package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/versions"
)

// ListSkills returns skills in the registry with cursor-based pagination.
//
//nolint:gocyclo
func (s *dbService) ListSkills(
	ctx context.Context,
	opts ...service.Option,
) (*service.ListSkillsResult, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListSkills")
	defer span.End()

	options := &service.ListSkillsOptions{
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

	registryID, err := lookupRegistryIDWithGate(ctx, s.pool, options.RegistryName, options.Claims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	querier := sqlc.New(s.pool)

	params := sqlc.ListSkillsParams{
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
			r, ok := record.(sqlc.ListSkillsRow)
			return r.Claims, ok
		},
	)
	listRows, nextCursor, err := streamSkillRows(ctx, querier, params, claimsFilter, options.Limit)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	packages, err := fetchSkillPackages(ctx, querier, listRows)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	skills := make([]*service.Skill, len(listRows))
	for i, row := range listRows {
		skill := service.ListSkillsRowToSkill(row)
		skill.Packages = packages[row.VersionID]
		skills[i] = skill
	}

	return &service.ListSkillsResult{
		Skills:     skills,
		NextCursor: nextCursor,
	}, nil
}

// fetchSkillPackages fetches OCI and Git packages for the given skill rows and
// returns them keyed by skill version ID.
func fetchSkillPackages(
	ctx context.Context,
	querier *sqlc.Queries,
	rows []sqlc.ListSkillsRow,
) (map[uuid.UUID][]service.SkillPackage, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.VersionID
	}

	ociPackages, err := querier.ListSkillOciPackages(ctx, ids)
	if err != nil {
		return nil, err
	}
	gitPackages, err := querier.ListSkillGitPackages(ctx, ids)
	if err != nil {
		return nil, err
	}

	packages := make(map[uuid.UUID][]service.SkillPackage)
	for _, pkg := range ociPackages {
		packages[pkg.SkillID] = append(packages[pkg.SkillID], toServiceSkillOciPackage(pkg))
	}
	for _, pkg := range gitPackages {
		packages[pkg.SkillID] = append(packages[pkg.SkillID], toServiceSkillGitPackage(pkg))
	}

	return packages, nil
}

// GetSkillVersion returns a specific skill version by name and version.
//
//nolint:gocyclo
func (s *dbService) GetSkillVersion(
	ctx context.Context,
	opts ...service.Option,
) (*service.Skill, error) {
	ctx, span := s.startSpan(ctx, "dbService.GetSkillVersion")
	defer span.End()

	options := &service.GetSkillVersionOptions{}
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

	registryID, err := lookupRegistryIDWithGate(ctx, s.pool, options.RegistryName, options.Claims)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	querier := sqlc.New(s.pool)

	params := sqlc.GetSkillVersionParams{
		Name:       options.Name,
		Version:    options.Version,
		Namespace:  &options.Namespace,
		RegistryID: registryID,
		Size:       int64(service.MaxPageSize) + 1,
	}
	if options.SourceName != "" {
		params.SourceName = &options.SourceName
	}

	rows, err := querier.GetSkillVersion(ctx, params)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
	}

	// Iterate rows in priority order (position ascending) and pick the first
	// one that passes the claims check, promoting lower-priority sources when
	// higher-priority ones fail.
	callerJSON := marshalClaims(options.Claims)
	var row sqlc.GetSkillVersionRow
	found := false
	for _, r := range rows {
		if callerJSON == nil || checkClaims(callerJSON, r.Claims) {
			row = r
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
	}

	ociPackages, err := querier.ListSkillOciPackages(ctx, []uuid.UUID{row.SkillVersionID})
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	gitPackages, err := querier.ListSkillGitPackages(ctx, []uuid.UUID{row.SkillVersionID})
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	packages := make([]service.SkillPackage, 0)
	for _, pkg := range ociPackages {
		packages = append(packages, toServiceSkillOciPackage(pkg))
	}
	for _, pkg := range gitPackages {
		packages = append(packages, toServiceSkillGitPackage(pkg))
	}

	res := service.GetSkillVersionRowToSkill(row)
	res.Packages = packages
	return res, nil
}

func toServiceSkillOciPackage(pkg sqlc.SkillOciPackage) service.SkillPackage {
	digest := ""
	mediaType := ""
	if pkg.Digest != nil {
		digest = *pkg.Digest
	}
	if pkg.MediaType != nil {
		mediaType = *pkg.MediaType
	}
	return service.SkillPackage{
		RegistryType: service.SkillPackageTypeOCI,
		Identifier:   pkg.Identifier,
		Digest:       digest,
		MediaType:    mediaType,
	}
}

func toServiceSkillGitPackage(pkg sqlc.SkillGitPackage) service.SkillPackage {
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
	return service.SkillPackage{
		RegistryType: service.SkillPackageTypeGit,
		URL:          pkg.Url,
		Ref:          ref,
		Commit:       commit,
		Subfolder:    subfolder,
	}
}

// PublishSkill inserts a new skill version into a managed registry.
func (s *dbService) PublishSkill(
	ctx context.Context,
	skill *service.Skill,
	opts ...service.Option,
) (*service.Skill, error) {
	ctx, span := s.startSpan(ctx, "dbService.PublishSkill")
	defer span.End()

	options := &service.PublishSkillOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}
	if skill.Namespace == "" || skill.Name == "" || skill.Version == "" {
		return nil, fmt.Errorf("namespace, name, and version are required")
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

	sourceName, err := s.executePublishSkillTransaction(ctx, skill, claimsJSON)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	result, err := s.fetchSkillVersionBySource(ctx, skill.Name, skill.Version, sourceName)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to fetch published skill: %w", err)
	}
	return result, nil
}

// fetchSkillVersionBySource retrieves a skill version using the source name directly,
// bypassing registry filtering. Used by the publish fetch-back path.
func (s *dbService) fetchSkillVersionBySource(
	ctx context.Context,
	name, version, sourceName string,
) (*service.Skill, error) {
	querier := sqlc.New(s.pool)
	row, err := querier.GetSkillVersionBySourceName(ctx, sqlc.GetSkillVersionBySourceNameParams{
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

	ociPackages, err := querier.ListSkillOciPackages(ctx, []uuid.UUID{row.SkillVersionID})
	if err != nil {
		return nil, err
	}
	gitPackages, err := querier.ListSkillGitPackages(ctx, []uuid.UUID{row.SkillVersionID})
	if err != nil {
		return nil, err
	}

	packages := make([]service.SkillPackage, 0, len(ociPackages)+len(gitPackages))
	for _, pkg := range ociPackages {
		packages = append(packages, toServiceSkillOciPackage(pkg))
	}
	for _, pkg := range gitPackages {
		packages = append(packages, toServiceSkillGitPackage(pkg))
	}

	result := service.GetSkillVersionRowToSkill(sqlc.GetSkillVersionRow{
		RegistryType:   row.RegistryType,
		ID:             row.ID,
		Name:           row.Name,
		Version:        row.Version,
		IsLatest:       row.IsLatest,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		Description:    row.Description,
		Title:          row.Title,
		SkillVersionID: row.SkillVersionID,
		Namespace:      row.Namespace,
		Status:         row.Status,
		License:        row.License,
		Compatibility:  row.Compatibility,
		AllowedTools:   row.AllowedTools,
		Repository:     row.Repository,
		Icons:          row.Icons,
		Metadata:       row.Metadata,
		ExtensionMeta:  row.ExtensionMeta,
		Claims:         row.Claims,
		Position:       row.Position,
	})
	result.Packages = packages
	return result, nil
}

// executePublishSkillTransaction executes the skill publish operation within a transaction.
// Returns the managed source name for fetch-back, or an error.
//
//nolint:gocyclo
func (s *dbService) executePublishSkillTransaction(
	ctx context.Context,
	skill *service.Skill,
	claimsJSON []byte,
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
	sourceName := managedSource.Name

	now := time.Now().UTC()

	// Get or create the registry entry (one per unique name)
	var entryID uuid.UUID
	existing, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  managedSource.ID,
		EntryType: sqlc.EntryTypeSKILL,
		Name:      skill.Name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		entryID, err = querier.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
			SourceID:  managedSource.ID,
			EntryType: sqlc.EntryTypeSKILL,
			Name:      skill.Name,
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
				return "", fmt.Errorf("%w: claims do not match existing entry", service.ErrClaimsMismatch)
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to get or create registry entry: %w", err)
	}

	// Insert the entry version (one per name+version)
	versionParams := sqlc.InsertEntryVersionParams{
		EntryID:   entryID,
		Name:      skill.Name,
		Version:   skill.Version,
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	if skill.Title != "" {
		versionParams.Title = &skill.Title
	}
	if skill.Description != "" {
		versionParams.Description = &skill.Description
	}

	versionID, err := querier.InsertEntryVersion(ctx, versionParams)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", fmt.Errorf("%w: %s %s", service.ErrVersionAlreadyExists, skill.Name, skill.Version)
		}
		return "", err
	}

	skillParams, err := makeInsertSkillVersionParams(versionID, skill)
	if err != nil {
		return "", err
	}

	_, err = querier.InsertSkillVersion(ctx, *skillParams)
	if err != nil {
		return "", err
	}

	for _, pkg := range skill.Packages {
		var err error
		switch pkg.RegistryType {
		case service.SkillPackageTypeOCI:
			err = querier.InsertSkillOciPackage(ctx, sqlc.InsertSkillOciPackageParams{
				SkillID:    versionID,
				Identifier: pkg.Identifier,
				Digest:     &pkg.Digest,
				MediaType:  &pkg.MediaType,
			})
		case service.SkillPackageTypeGit:
			err = querier.InsertSkillGitPackage(ctx, sqlc.InsertSkillGitPackageParams{
				SkillID:   versionID,
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
		Name:     skill.Name,
		SourceID: managedSource.ID,
	})
	if err == nil {
		shouldUpdateLatest = versions.IsNewerVersion(skill.Version, currentLatest)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("failed to get current latest version: %w", err)
	}

	if shouldUpdateLatest {
		_, err = querier.UpsertLatestSkillVersion(ctx, sqlc.UpsertLatestSkillVersionParams{
			SourceID:  managedSource.ID,
			Name:      skill.Name,
			Version:   skill.Version,
			VersionID: versionID,
		})
		if err != nil {
			return "", fmt.Errorf("failed to upsert latest skill version: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return sourceName, nil
}

func makeInsertSkillVersionParams(
	versionID uuid.UUID,
	skill *service.Skill,
) (*sqlc.InsertSkillVersionParams, error) {

	status := sqlc.NullSkillStatus{}
	if skill.Status != "" {
		status = sqlc.NullSkillStatus{
			SkillStatus: sqlc.SkillStatus(skill.Status),
			Valid:       true,
		}
	}

	repository, err := json.Marshal(skill.Repository)
	if err != nil {
		return nil, err
	}
	icons, err := json.Marshal(skill.Icons)
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(skill.Metadata)
	if err != nil {
		return nil, err
	}
	extensionMeta, err := json.Marshal(skill.Meta)
	if err != nil {
		return nil, err
	}

	skillParams := sqlc.InsertSkillVersionParams{
		VersionID:     versionID,
		Namespace:     skill.Namespace,
		Status:        status,
		AllowedTools:  skill.AllowedTools,
		Repository:    repository,
		Icons:         icons,
		Metadata:      metadata,
		ExtensionMeta: extensionMeta,
	}
	if skill.License != "" {
		skillParams.License = &skill.License
	}
	if skill.Compatibility != "" {
		skillParams.Compatibility = &skill.Compatibility
	}

	return &skillParams, nil
}

// DeleteSkillVersion removes a skill version from a managed registry.
func (s *dbService) DeleteSkillVersion(
	ctx context.Context,
	opts ...service.Option,
) error {
	ctx, span := s.startSpan(ctx, "dbService.DeleteSkillVersion")
	defer span.End()

	options := &service.DeleteSkillVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return err
		}
	}

	if err := s.executeDeleteSkillTransaction(ctx, options); err != nil {
		otel.RecordError(span, err)
		return err
	}

	return nil
}

// executeDeleteSkillTransaction runs the skill version deletion within a serializable transaction.
func (s *dbService) executeDeleteSkillTransaction(
	ctx context.Context,
	options *service.DeleteSkillVersionOptions,
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
			EntryType: sqlc.EntryTypeSKILL,
			Name:      options.Name,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s@%s", service.ErrNotFound, options.Name, options.Version)
			}
			return fmt.Errorf("failed to look up registry entry: %w", err)
		}
		if err := validateClaimsSubsetBytes(ctx, options.JWTClaims, existing.Claims); err != nil {
			return err
		}
	}

	entryID, err := lookupAndDeleteEntryVersion(
		ctx,
		querier,
		registry.ID,
		sqlc.EntryTypeSKILL,
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
			_, err := querier.UpsertLatestSkillVersion(ctx, sqlc.UpsertLatestSkillVersionParams{
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

// streamSkillRows fetches skill rows in batches, applying the auth filter then the
// dedup filter to each record, until limit+1 rows are accumulated or the DB is
// exhausted. It returns the trimmed slice (≤ limit) and the encoded cursor for the
// next page, if any.
func streamSkillRows(
	ctx context.Context,
	querier *sqlc.Queries,
	params sqlc.ListSkillsParams,
	filter service.RecordFilter,
	limit int,
) ([]sqlc.ListSkillsRow, string, error) {
	dedupFilter := newDeduplicatingSkillFilter()
	var accumulated []sqlc.ListSkillsRow
	batchParams := params

	for {
		batch, err := querier.ListSkills(ctx, batchParams)
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

// newDeduplicatingSkillFilter returns a stateful RecordFilter that deduplicates
// skill rows by entry name, keeping only records from the highest-priority source
// (lowest position). SQL must return records in position-ascending order per name.
func newDeduplicatingSkillFilter() service.RecordFilter {
	return newDeduplicatingFilterWith(
		func(record any) (string, int32, bool) {
			r, ok := record.(sqlc.ListSkillsRow)
			return r.Name, r.Position, ok
		},
	)
}

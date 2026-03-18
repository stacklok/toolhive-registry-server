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

	if err := checkRegistryExists(ctx, s.pool, options.RegistryName); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	querier := sqlc.New(s.pool)

	params := sqlc.ListSkillsParams{
		RegistryName: &options.RegistryName,
		Size:         int64(options.Limit + 1),
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

	allRows, err := querier.ListSkills(ctx, params)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// Deduplicate by (name, version), keeping the first occurrence
	// (highest-priority source, ordered by position ASC).
	listRows := deduplicateSkillRows(allRows)

	ids := make([]uuid.UUID, 0)
	for _, row := range listRows {
		ids = append(ids, row.VersionID)
	}

	ociPackages, err := querier.ListSkillOciPackages(ctx, ids)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	gitPackages, err := querier.ListSkillGitPackages(ctx, ids)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	packages := make(map[uuid.UUID][]service.SkillPackage)
	for _, pkg := range ociPackages {
		packages[pkg.SkillID] = append(packages[pkg.SkillID], toServiceSkillOciPackage(pkg))
	}
	for _, pkg := range gitPackages {
		packages[pkg.SkillID] = append(packages[pkg.SkillID], toServiceSkillGitPackage(pkg))
	}

	nextCursor := ""
	if len(listRows) > options.Limit {
		last := listRows[options.Limit-1]
		nextCursor = service.EncodeCursor(last.Name, last.Version)
		listRows = listRows[:options.Limit]
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

// GetSkillVersion returns a specific skill version by name and version.
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

	if err := checkRegistryExists(ctx, s.pool, options.RegistryName); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	querier := sqlc.New(s.pool)

	params := sqlc.GetSkillVersionParams{
		Name:         options.Name,
		Version:      options.Version,
		RegistryName: &options.RegistryName,
		Namespace:    &options.Namespace,
	}

	rows, err := querier.GetSkillVersion(ctx, params)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
	}

	// Pick the first row (ordered by position ascending = highest priority source)
	row := rows[0]

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

	sourceName, err := s.executePublishSkillTransaction(ctx, skill)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	return s.GetSkillVersion(ctx,
		service.WithRegistryName(sourceName),
		service.WithName(skill.Name),
		service.WithVersion(skill.Version),
		service.WithNamespace(skill.Namespace),
	)
}

// executePublishSkillTransaction executes the skill publish operation within a transaction.
// Returns the managed source name for fetch-back, or an error.
//
//nolint:gocyclo
func (s *dbService) executePublishSkillTransaction(
	ctx context.Context,
	skill *service.Skill,
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
	entryID, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  managedSource.ID,
		EntryType: sqlc.EntryTypeSKILL,
		Name:      skill.Name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		entryID, err = querier.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
			SourceID:  managedSource.ID,
			EntryType: sqlc.EntryTypeSKILL,
			Name:      skill.Name,
			CreatedAt: &now,
			UpdatedAt: &now,
		})
	}
	if err != nil {
		return "", fmt.Errorf("failed to get or create registry entry: %w", err)
	}

	// Insert the entry version (one per name+version)
	versionParams := sqlc.InsertEntryVersionParams{
		EntryID:   entryID,
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
	latestSkillRows, err := querier.GetSkillVersion(ctx, sqlc.GetSkillVersionParams{
		Name:         skill.Name,
		RegistryName: &sourceName,
		Namespace:    &skill.Namespace,
		Version:      "latest",
	})
	if err == nil && len(latestSkillRows) > 0 {
		shouldUpdateLatest = versions.IsNewerVersion(skill.Version, latestSkillRows[0].Version)
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
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

	if err := cleanupOrphanedEntry(ctx, querier, entryID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// deduplicateSkillRows removes duplicate entries at the entry name level, keeping
// all versions from the highest-priority source (lowest position value).
func deduplicateSkillRows(rows []sqlc.ListSkillsRow) []sqlc.ListSkillsRow {
	// Pass 1: determine winning position per entry name (lowest position wins)
	winningPosition := make(map[string]int32, len(rows))
	for _, r := range rows {
		if pos, ok := winningPosition[r.Name]; !ok || r.Position < pos {
			winningPosition[r.Name] = r.Position
		}
	}
	// Pass 2: keep only versions from the winning source (by position)
	result := make([]sqlc.ListSkillsRow, 0, len(rows))
	for _, r := range rows {
		if r.Position == winningPosition[r.Name] {
			result = append(result, r)
		}
	}
	return result
}

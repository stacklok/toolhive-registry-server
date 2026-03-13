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

	if options.Limit > service.MaxPageSize {
		options.Limit = service.MaxPageSize
	}

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

	querier := sqlc.New(s.pool)

	if err := validateRegistryExists(ctx, querier, options.RegistryName); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	listRows, err := querier.ListSkills(ctx, params)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

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

	params := sqlc.GetSkillVersionParams{
		Name:         options.Name,
		Version:      options.Version,
		RegistryName: &options.RegistryName,
		Namespace:    &options.Namespace,
	}

	querier := sqlc.New(s.pool)
	row, err := querier.GetSkillVersion(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s %s", service.ErrNotFound, options.Name, options.Version)
		}
		otel.RecordError(span, err)
		return nil, err
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

	if err := s.executePublishSkillTransaction(ctx, options.RegistryName, skill); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	return s.GetSkillVersion(ctx,
		service.WithRegistryName(options.RegistryName),
		service.WithName(skill.Name),
		service.WithVersion(skill.Version),
		service.WithNamespace(skill.Namespace),
	)
}

// executePublishSkillTransaction executes the skill publish operation within a transaction.
//
//nolint:gocyclo
func (s *dbService) executePublishSkillTransaction(
	ctx context.Context,
	registryName string,
	skill *service.Skill,
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

	registry, err := validateManagedRegistry(ctx, querier, registryName)
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	// Get or create the registry entry (one per unique name)
	entryID, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  registry.ID,
		EntryType: sqlc.EntryTypeSKILL,
		Name:      skill.Name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		entryID, err = querier.InsertRegistryEntry(ctx, sqlc.InsertRegistryEntryParams{
			SourceID:  registry.ID,
			EntryType: sqlc.EntryTypeSKILL,
			Name:      skill.Name,
			CreatedAt: &now,
			UpdatedAt: &now,
		})
	}
	if err != nil {
		return fmt.Errorf("failed to get or create registry entry: %w", err)
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
			return fmt.Errorf("%w: %s %s", service.ErrVersionAlreadyExists, skill.Name, skill.Version)
		}
		return err
	}

	skillParams, err := makeInsertSkillVersionParams(versionID, skill)
	if err != nil {
		return err
	}

	_, err = querier.InsertSkillVersion(ctx, *skillParams)
	if err != nil {
		return err
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
			return err
		}
	}

	// Compare with current latest before upserting — avoid regressing the pointer
	shouldUpdateLatest := true
	latestSkill, err := querier.GetSkillVersion(ctx, sqlc.GetSkillVersionParams{
		Name:         skill.Name,
		RegistryName: &registryName,
		Namespace:    &skill.Namespace,
		Version:      "latest",
	})
	if err == nil {
		shouldUpdateLatest = versions.IsNewerVersion(skill.Version, latestSkill.Version)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to get current latest version: %w", err)
	}

	if shouldUpdateLatest {
		_, err = querier.UpsertLatestSkillVersion(ctx, sqlc.UpsertLatestSkillVersionParams{
			SourceID:  registry.ID,
			Name:      skill.Name,
			Version:   skill.Version,
			VersionID: versionID,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert latest skill version: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
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

	registry, err := validateManagedRegistry(ctx, querier, options.RegistryName)
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

// Package database provides the database-backed implementation of the SkillService interface.
package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		Namespace:    &options.Namespace,
		Size:         int64(options.Limit + 1),
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
	listRows, err := querier.ListSkills(ctx, params)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	ids := make([]uuid.UUID, 0)
	for _, row := range listRows {
		ids = append(ids, row.EntryID)
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
		packages[pkg.SkillEntryID] = append(packages[pkg.SkillEntryID], toServiceSkillOciPackage(pkg))
	}
	for _, pkg := range gitPackages {
		packages[pkg.SkillEntryID] = append(packages[pkg.SkillEntryID], toServiceSkillGitPackage(pkg))
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
		skill.Packages = packages[row.EntryID]
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

	params := sqlc.GetSkillVersionParams{
		Name:         options.Name,
		Version:      options.Version,
		RegistryName: &options.RegistryName,
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

	ociPackages, err := querier.ListSkillOciPackages(ctx, []uuid.UUID{row.SkillEntryID})
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}
	gitPackages, err := querier.ListSkillGitPackages(ctx, []uuid.UUID{row.SkillEntryID})
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
	return service.SkillPackage{
		RegistryType: service.SkillPackageTypeGit,
		URL:          pkg.Url,
		Ref:          *pkg.Ref,
		Commit:       *pkg.CommitSha,
	}
}

// PublishSkill inserts a new skill version into a managed registry.
//
//nolint:gocyclo
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

	registry, err := validateManagedRegistry(ctx, sqlc.New(s.pool), options.RegistryName)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	now := time.Now().UTC()
	entryParams := sqlc.InsertRegistryEntryParams{
		RegID:     registry.ID,
		EntryType: sqlc.EntryTypeSKILL,
		Name:      skill.Name,
		Version:   skill.Version,
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	if skill.Title != "" {
		entryParams.Title = &skill.Title
	}
	if skill.Description != "" {
		entryParams.Description = &skill.Description
	}

	querier := sqlc.New(s.pool)
	entryID, err := querier.InsertRegistryEntry(ctx, entryParams)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, fmt.Errorf("%w: %s %s", service.ErrVersionAlreadyExists, skill.Name, skill.Version)
		}
		otel.RecordError(span, err)
		return nil, err
	}

	skillParams, err := makeInsertSkillVersionParams(entryID, skill)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	_, err = querier.InsertSkillVersion(ctx, *skillParams)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	for _, pkg := range skill.Packages {
		var err error
		switch pkg.RegistryType {
		case service.SkillPackageTypeOCI:
			err = querier.InsertSkillOciPackage(ctx, sqlc.InsertSkillOciPackageParams{
				SkillEntryID: entryID,
				Identifier:   pkg.Identifier,
				Digest:       &pkg.Digest,
				MediaType:    &pkg.MediaType,
			})
		case service.SkillPackageTypeGit:
			err = querier.InsertSkillGitPackage(ctx, sqlc.InsertSkillGitPackageParams{
				SkillEntryID: entryID,
				Url:          pkg.URL,
				Ref:          &pkg.Ref,
				CommitSha:    &pkg.Commit,
				Subfolder:    &pkg.Subfolder,
			})
		}
		if err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	latestSkill, err := querier.GetSkillVersion(ctx, sqlc.GetSkillVersionParams{
		Name:         skill.Name,
		RegistryName: &options.RegistryName,
		Version:      "latest",
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		otel.RecordError(span, err)
		return nil, err
	}

	if versions.IsNewerVersion(skill.Version, latestSkill.Version) {
		_, err = querier.UpsertLatestSkillVersion(ctx, sqlc.UpsertLatestSkillVersionParams{
			RegID:   registry.ID,
			Name:    skill.Name,
			Version: skill.Version,
			EntryID: entryID,
		})
		if err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	return s.GetSkillVersion(ctx,
		service.WithRegistryName(options.RegistryName),
		service.WithName(skill.Name),
		service.WithVersion(skill.Version),
	)
}

func makeInsertSkillVersionParams(
	entryID uuid.UUID,
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
		EntryID:       entryID,
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

	options := &service.DeleteServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return err
		}
	}

	registry, err := validateManagedRegistry(ctx, sqlc.New(s.pool), options.RegistryName)
	if err != nil {
		otel.RecordError(span, err)
		return err
	}

	querier := sqlc.New(s.pool)
	affected, err := querier.DeleteRegistryEntry(ctx, sqlc.DeleteRegistryEntryParams{
		RegID:   registry.ID,
		Name:    options.ServerName,
		Version: options.Version,
	})
	if err != nil {
		otel.RecordError(span, err)
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s %s", service.ErrNotFound, options.ServerName, options.Version)
	}

	return nil
}

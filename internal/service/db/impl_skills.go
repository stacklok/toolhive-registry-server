// Package database provides the database-backed implementation of the SkillService interface.
package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

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

	librarySkill, err := libraryModel[model.Skill](skill)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to convert skill: %w", err)
	}
	publishableSkill(librarySkill)
	sourceName, err := s.publishThroughLibrary(ctx, persistence.Entry{
		Kind: persistence.SkillKind, Name: skill.Name, Version: skill.Version, Skill: librarySkill,
	}, gateClaims, claimsJSON)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	result, err := s.fetchSkillVersionBySource(ctx, skill.Name, skill.Version, sourceName)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to fetch published skill: %w", err)
	}
	// v1.5.4 stored an absent repository as JSON null and returned it as {};
	// the library stores none, so keep the published response's shape.
	if result.Repository == nil {
		result.Repository = &service.SkillRepository{}
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

	if err := s.deleteThroughLibrary(ctx, persistence.SkillKind, options.Name, options.Version,
		s.deleteGate(options.JWTClaims)); err != nil {
		otel.RecordError(span, err)
		return err
	}

	return nil
}

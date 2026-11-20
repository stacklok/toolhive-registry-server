// Package database provides the database-backed implementation of the plugin
// catalog operations on the RegistryService interface.
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

	libraryPlugin, err := libraryModel[model.Plugin](plugin)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to convert plugin: %w", err)
	}
	publishablePlugin(libraryPlugin)
	sourceName, err := s.publishThroughLibrary(ctx, persistence.Entry{
		Kind: persistence.PluginKind, Name: plugin.Name, Version: plugin.Version, Plugin: libraryPlugin,
	}, gateClaims, claimsJSON)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	result, err := s.fetchPluginVersionBySource(ctx, plugin.Name, plugin.Version, sourceName)
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to fetch published plugin: %w", err)
	}
	// v1.5.4 stored an absent repository as JSON null and returned it as {};
	// the library stores none, so keep the published response's shape.
	if result.Repository == nil {
		result.Repository = &service.PluginRepository{}
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

	if err := s.deleteThroughLibrary(ctx, persistence.PluginKind, options.Name, options.Version,
		s.deleteGate(options.JWTClaims)); err != nil {
		otel.RecordError(span, err)
		return err
	}

	return nil
}

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
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/validators"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

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
		Claims:       row.Claims,
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
		err := fmt.Errorf("%w: %s", service.ErrInvalidServerName, serverData.Name)
		otel.RecordError(span, err)
		return nil, err
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

	sourceName, err := s.publishThroughLibrary(ctx, persistence.Entry{
		Kind: persistence.ServerKind, Name: serverData.Name, Version: serverData.Version, Server: serverData,
	}, gateClaims, claimsJSON)
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

	if err := s.deleteThroughLibrary(ctx, persistence.ServerKind, options.ServerName, options.Version,
		s.deleteGate(options.JWTClaims)); err != nil {
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

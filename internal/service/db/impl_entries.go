package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"

	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// UpdateEntryClaims updates the claims on a published registry entry within the managed source.
func (s *dbService) UpdateEntryClaims(ctx context.Context, opts ...service.Option) error {
	ctx, span := s.startSpan(ctx, "dbService.UpdateEntryClaims")
	defer span.End()
	start := time.Now()

	options := &service.UpdateEntryClaimsOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return fmt.Errorf("invalid option: %w", err)
		}
	}

	span.SetAttributes(
		attribute.String("entry.type", options.EntryType),
		attribute.String("entry.name", options.Name),
	)

	entryType, err := mapEntryType(options.EntryType)
	if err != nil {
		otel.RecordError(span, err)
		return err
	}

	if options.Claims != nil {
		if err := db.ValidateClaimValues(options.Claims); err != nil {
			otel.RecordError(span, err)
			return fmt.Errorf("invalid claim values: %w", err)
		}
	}

	if err := s.validateClaimsSubset(ctx, options.JWTClaims, options.Claims); err != nil {
		otel.RecordError(span, err)
		return err
	}

	if err := s.executeUpdateClaimsTransaction(ctx, options, entryType); err != nil {
		otel.RecordError(span, err)
		return err
	}

	slog.InfoContext(ctx, "Entry claims updated",
		"duration_ms", time.Since(start).Milliseconds(),
		"entry_type", options.EntryType,
		"name", options.Name,
		"request_id", middleware.GetReqID(ctx))

	return nil
}

// executeUpdateClaimsTransaction runs the claims update within a serializable transaction.
func (s *dbService) executeUpdateClaimsTransaction(
	ctx context.Context,
	options *service.UpdateEntryClaimsOptions,
	entryType sqlc.EntryType,
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

	existing, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  source.ID,
		EntryType: entryType,
		Name:      options.Name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", service.ErrNotFound, options.Name)
		}
		return fmt.Errorf("failed to look up registry entry: %w", err)
	}

	if err := s.validateClaimsSubsetBytes(ctx, options.JWTClaims, existing.Claims); err != nil {
		return err
	}

	claimsJSON := db.SerializeClaims(options.Claims)

	rowsAffected, err := querier.UpdateRegistryEntryClaims(ctx, sqlc.UpdateRegistryEntryClaimsParams{
		Claims:    claimsJSON,
		SourceID:  source.ID,
		EntryType: entryType,
		Name:      options.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to update entry claims: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: %s", service.ErrNotFound, options.Name)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// mapEntryType converts a validated entry type string to the corresponding sqlc.EntryType.
// service.WithEntryType is the single source of truth for accepted values, so
// reaching the default branch indicates a caller bypassed the option setter.
func mapEntryType(entryType string) (sqlc.EntryType, error) {
	switch entryType {
	case service.EntryTypeServer:
		return sqlc.EntryTypeMCP, nil
	case service.EntryTypeSkill:
		return sqlc.EntryTypeSKILL, nil
	default:
		return "", fmt.Errorf("%w: %s", service.ErrInvalidEntryType, entryType)
	}
}

// GetEntryClaims returns the claims map for a published entry within the managed source.
// The returned map is non-nil even when the entry has no claims set, so callers can
// rely on a stable JSON shape. Access is gated by the manageEntries role plus a
// JWT-subset check against the entry's claims, mirroring the matching PUT and the
// default-deny visibility rule (auth.md §4).
func (s *dbService) GetEntryClaims(ctx context.Context, opts ...service.Option) (map[string]any, error) {
	ctx, span := s.startSpan(ctx, "dbService.GetEntryClaims")
	defer span.End()

	options := &service.GetEntryClaimsOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, fmt.Errorf("invalid option: %w", err)
		}
	}

	span.SetAttributes(
		attribute.String("entry.type", options.EntryType),
		attribute.String("entry.name", options.Name),
	)

	entryType, err := mapEntryType(options.EntryType)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	querier := sqlc.New(s.pool)

	source, err := getManagedSource(ctx, querier)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	row, err := querier.GetRegistryEntryByName(ctx, sqlc.GetRegistryEntryByNameParams{
		SourceID:  source.ID,
		EntryType: entryType,
		Name:      options.Name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", service.ErrNotFound, options.Name)
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to look up registry entry: %w", err)
	}

	if err := s.validateClaimsSubsetBytes(ctx, options.JWTClaims, row.Claims); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	claims := db.DeserializeClaims(row.Claims)
	if claims == nil {
		claims = map[string]any{}
	}
	return claims, nil
}

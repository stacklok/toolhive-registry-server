package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// publishThroughLibrary adds one managed version with the library, which owns
// the entry rows and latest pointers. The ClaimsHook installed on the
// library adapter applies the claims gates and stores claimsJSON inside the
// same transaction. It returns the managed source's name for the fetch-back.
//
// Before handing the entry to the library it keeps v1.5.4's order of checks:
// the managed source must exist and the caller must cover its claims (the
// hook repeats this gate inside the write transaction), and only then is the
// payload examined. A caller who may not publish gets 403 for any body.
func (s *dbService) publishThroughLibrary(
	ctx context.Context, entry persistence.Entry, gate map[string]any, claimsJSON []byte,
) (string, error) {
	if s.publisher == nil {
		return "", errors.New("entry publisher is not configured")
	}
	source, err := getManagedSource(ctx, sqlc.New(s.pool))
	if err != nil {
		return "", err
	}
	if err := validateClaimsVisibleBytes(ctx, gate, source.Claims); err != nil {
		return "", err
	}
	if err := s.checkPublishedMetaSize(ctx, entry); err != nil {
		return "", err
	}
	ctx = withEntryWriteRequest(ctx, entryWriteRequest{gate: gate, stored: claimsJSON, prior: new([]byte)})
	if _, err := s.publisher.Publish(ctx, entry); err != nil {
		return "", libraryWriteError(err, entry.Kind, entry.Name, entry.Version)
	}
	return source.Name, nil
}

// deleteThroughLibrary removes one managed version with the library. The
// ClaimsHook checks the caller can see the entry before the row goes.
func (s *dbService) deleteThroughLibrary(
	ctx context.Context, kind persistence.EntryKind, name, version string, gate map[string]any,
) error {
	if s.publisher == nil {
		return errors.New("entry publisher is not configured")
	}
	ctx = withEntryWriteRequest(ctx, entryWriteRequest{gate: gate})
	if err := s.publisher.DeleteManaged(ctx, kind, name, version); err != nil {
		return libraryWriteError(err, kind, name, version)
	}
	return nil
}

// deleteGate returns the claims the delete gate checks: none when
// authorization is off, otherwise the caller's JWT claims (nil when anonymous).
func (s *dbService) deleteGate(jwtClaims map[string]any) map[string]any {
	if s.skipAuthz {
		return nil
	}
	return jwtClaims
}

// libraryWriteError maps library errors to the service errors the v1
// handlers already translate into v1.5.4 status codes and messages. Errors
// from the ClaimsHook are service errors already and pass through.
//
// A write that lost to concurrent writers on every retry is not a duplicate
// version: it stays an internal error (500 "failed to publish entry", as in
// v1.5.4), so ErrContention is checked before the ErrConflict it refines.
func libraryWriteError(err error, kind persistence.EntryKind, name, version string) error {
	switch {
	case errors.Is(err, persistence.ErrNoManagedSource):
		return service.ErrNoManagedSource
	case errors.Is(err, persistence.ErrNotFound):
		return fmt.Errorf("%w: %s@%s", service.ErrNotFound, name, version)
	case errors.Is(err, persistence.ErrContention):
		return fmt.Errorf("managed write of %s@%s abandoned: %w", name, version, err)
	case errors.Is(err, persistence.ErrConflict):
		return versionExistsError(kind, name, version)
	case errors.Is(err, persistence.ErrInvalid):
		return fmt.Errorf("%w: %s", service.ErrInvalidEntryPayload,
			strings.TrimPrefix(err.Error(), persistence.ErrInvalid.Error()+": "))
	default:
		return err
	}
}

// versionExistsError keeps v1.5.4's duplicate-version text per kind: a server
// is named "name@version", a skill or plugin "name version".
func versionExistsError(kind persistence.EntryKind, name, version string) error {
	if kind == persistence.ServerKind {
		return fmt.Errorf("%w: %s@%s", service.ErrVersionAlreadyExists, name, version)
	}
	return fmt.Errorf("%w: %s %s", service.ErrVersionAlreadyExists, name, version)
}

// libraryModel converts a service-layer skill or plugin to the library model
// of the same JSON shape.
func libraryModel[T any](v any) (*T, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := new(T)
	if err := json.Unmarshal(data, out); err != nil {
		return nil, err
	}
	return out, nil
}

// publishableSkill makes a skill publish body acceptable to the library the
// way v1.5.4 accepted it. The storage-assigned id, isLatest, createdAt and
// updatedAt are ignored rather than rejected, and status is accepted in any
// case: v1.5.4 upper-cased it before storing, while the library's schema takes
// it lower-cased (and still stores it upper-cased, so responses read "ACTIVE").
func publishableSkill(skill *model.Skill) {
	skill.ID, skill.IsLatest = "", false
	skill.CreatedAt, skill.UpdatedAt = time.Time{}, time.Time{}
	skill.Status = strings.ToLower(skill.Status)
}

// publishablePlugin is publishableSkill for plugins.
func publishablePlugin(plugin *model.Plugin) {
	plugin.ID, plugin.IsLatest = "", false
	plugin.CreatedAt, plugin.UpdatedAt = time.Time{}, time.Time{}
	plugin.Status = strings.ToLower(plugin.Status)
}

// checkPublishedMetaSize rejects a server whose publisher-provided _meta is
// larger than the configured limit, with a message and a log line that name
// both sizes. The library applies the same limit but reports no sizes.
func (s *dbService) checkPublishedMetaSize(ctx context.Context, entry persistence.Entry) error {
	if entry.Server == nil || entry.Server.Meta == nil || len(entry.Server.Meta.PublisherProvided) == 0 ||
		s.maxMetaSize <= 0 {
		return nil
	}
	data, err := json.Marshal(entry.Server.Meta.PublisherProvided)
	if err != nil {
		return fmt.Errorf("%w: publisher-provided metadata cannot be serialized", service.ErrInvalidEntryPayload)
	}
	if len(data) <= s.maxMetaSize {
		return nil
	}
	slog.WarnContext(ctx, "Publisher-provided metadata exceeds maximum size",
		"server", entry.Name,
		"version", entry.Version,
		"size_bytes", len(data),
		"max_bytes", s.maxMetaSize,
		"request_id", middleware.GetReqID(ctx))
	return fmt.Errorf("%w: publisher-provided metadata size %d bytes exceeds maximum allowed size of %d bytes",
		service.ErrInvalidEntryPayload, len(data), s.maxMetaSize)
}

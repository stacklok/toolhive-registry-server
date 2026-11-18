package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// publishThroughLibrary adds one managed version with the library, which owns
// the entry rows and latest pointers. The ClaimsHook installed on the
// library adapter applies the claims gates and stores claimsJSON inside the
// same transaction. It returns the managed source's name for the fetch-back.
func (s *dbService) publishThroughLibrary(
	ctx context.Context, entry persistence.Entry, gate map[string]any, claimsJSON []byte,
) (string, error) {
	if s.publisher == nil {
		return "", errors.New("entry publisher is not configured")
	}
	ctx = withEntryWriteRequest(ctx, entryWriteRequest{gate: gate, stored: claimsJSON})
	if _, err := s.publisher.Publish(ctx, entry); err != nil {
		return "", libraryWriteError(err, entry.Name, entry.Version)
	}
	source, err := getManagedSource(ctx, sqlc.New(s.pool))
	if err != nil {
		return "", err
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
		return libraryWriteError(err, name, version)
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
func libraryWriteError(err error, name, version string) error {
	switch {
	case errors.Is(err, persistence.ErrNoManagedSource):
		return service.ErrNoManagedSource
	case errors.Is(err, persistence.ErrNotFound):
		return fmt.Errorf("%w: %s@%s", service.ErrNotFound, name, version)
	case errors.Is(err, persistence.ErrConflict):
		return fmt.Errorf("%w: %s@%s", service.ErrVersionAlreadyExists, name, version)
	case errors.Is(err, persistence.ErrInvalid):
		return fmt.Errorf("%w: %s", service.ErrInvalidEntryPayload,
			strings.TrimPrefix(err.Error(), persistence.ErrInvalid.Error()+": "))
	default:
		return err
	}
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

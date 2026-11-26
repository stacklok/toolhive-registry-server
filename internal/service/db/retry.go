package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// maxWriteAttempts bounds how often an admin write transaction is retried
// after a PostgreSQL serialization failure or deadlock.
const maxWriteAttempts = 5

// CreateSource creates an API-managed source.
func (s *dbService) CreateSource(
	ctx context.Context, name string, req *service.SourceCreateRequest,
) (*service.SourceInfo, error) {
	return retrySerializable(ctx, func() (*service.SourceInfo, error) { return s.createSource(ctx, name, req) })
}

// UpdateSource updates an API-managed source.
func (s *dbService) UpdateSource(
	ctx context.Context, name string, req *service.SourceCreateRequest,
) (*service.SourceInfo, error) {
	return retrySerializable(ctx, func() (*service.SourceInfo, error) { return s.updateSource(ctx, name, req) })
}

// DeleteSource deletes an API-managed source.
func (s *dbService) DeleteSource(ctx context.Context, name string) error {
	_, err := retrySerializable(ctx, func() (struct{}, error) { return struct{}{}, s.deleteSource(ctx, name) })
	return err
}

// CreateRegistry creates an API-managed registry.
func (s *dbService) CreateRegistry(
	ctx context.Context, name string, req *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	return retrySerializable(ctx, func() (*service.RegistryInfo, error) { return s.createRegistry(ctx, name, req) })
}

// UpdateRegistry updates an API-managed registry.
func (s *dbService) UpdateRegistry(
	ctx context.Context, name string, req *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	return retrySerializable(ctx, func() (*service.RegistryInfo, error) { return s.updateRegistry(ctx, name, req) })
}

// DeleteRegistry deletes an API-managed registry.
func (s *dbService) DeleteRegistry(ctx context.Context, name string) error {
	_, err := retrySerializable(ctx, func() (struct{}, error) { return struct{}{}, s.deleteRegistry(ctx, name) })
	return err
}

// UpdateEntryClaims replaces the claims of a published entry.
func (s *dbService) UpdateEntryClaims(ctx context.Context, opts ...service.Option) error {
	_, err := retrySerializable(ctx, func() (struct{}, error) { return struct{}{}, s.updateEntryClaims(ctx, opts...) })
	return err
}

// retrySerializable runs fn, a whole serializable write transaction, again
// when PostgreSQL aborts it with a serialization failure or deadlock. Such an
// abort means a concurrent writer — for example a sync job of the same
// source — touched the same rows; retrying is the documented remedy, and the
// library's own writes do the same.
func retrySerializable[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	for attempt := 1; ; attempt++ {
		v, err := fn()
		if err == nil || attempt == maxWriteAttempts || !isSerializationFailure(err) {
			return v, err
		}
		select {
		case <-ctx.Done():
			return v, err
		case <-time.After(time.Duration(attempt) * 10 * time.Millisecond):
		}
	}
}

func isSerializationFailure(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01")
}

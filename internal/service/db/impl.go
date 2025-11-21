// Package database provides a database-backed implementation of the RegistryService interface
package database

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// options holds configuration options for the database service
type options struct {
	pool *pgxpool.Pool
}

// Option is a functional option for configuring the database service
type Option func(*options) error

// WithConnectionPool creates a new database-backed registry service with the
// given pgx pool.
func WithConnectionPool(pool *pgxpool.Pool) Option {
	return func(o *options) error {
		if pool == nil {
			return fmt.Errorf("pgx pool is required")
		}
		o.pool = pool
		return nil
	}
}

// WithConnectionString creates a new database-backed registry service with
// the given connection string.
func WithConnectionString(connString string) Option {
	return func(o *options) error {
		if connString == "" {
			return fmt.Errorf("connection string is required")
		}

		pool, err := pgxpool.New(context.Background(), connString)
		if err != nil {
			return fmt.Errorf("failed to create pgx pool: %w", err)
		}

		o.pool = pool
		return nil
	}
}

// dbService implements the RegistryService interface using a database backend
type dbService struct {
	pool *pgxpool.Pool
}

var _ service.RegistryService = (*dbService)(nil)

// New creates a new database-backed registry service with the given options
func New(opts ...Option) (service.RegistryService, error) {
	o := &options{}

	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}

	return &dbService{
		pool: o.pool,
	}, nil
}

// CheckReadiness checks if the service is ready to serve requests
func (s *dbService) CheckReadiness(ctx context.Context) error {
	err := s.pool.Ping(ctx)
	if err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	return nil
}

// GetRegistry returns the registry data with metadata
func (*dbService) GetRegistry(
	_ context.Context,
) (*toolhivetypes.UpstreamRegistry, string, error) {
	return nil, "", service.ErrNotImplemented
}

// ListServers returns all servers in the registry
func (s *dbService) ListServers(
	ctx context.Context,
	opts ...service.Option[service.ListServersOptions],
) ([]*upstreamv0.ServerJSON, error) {
	// TODO: implement
	options := &service.ListServersOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	decoded, err := base64.StdEncoding.DecodeString(options.Cursor)
	if err != nil {
		return nil, err
	}
	nextTime, err := time.Parse(time.RFC3339, string(decoded))
	if err != nil {
		return nil, err
	}

	querierFunc := func(querier sqlc.Querier) ([]helper, error) {
		servers, err := querier.ListServers(
			ctx,
			sqlc.ListServersParams{
				Next: &nextTime,
				Size: int64(options.Limit),
			},
		)

		if err != nil {
			return nil, err
		}

		helpers := make([]helper, len(servers))
		for i, server := range servers {
			helpers[i] = listServersRowToHelper(server)
		}

		return helpers, nil
	}

	return s.sharedListServers(ctx, querierFunc)
}

// ListServerVersions implements RegistryService.ListServerVersions
func (s *dbService) ListServerVersions(
	ctx context.Context,
	opts ...service.Option[service.ListServerVersionsOptions],
) ([]*upstreamv0.ServerJSON, error) {
	options := &service.ListServerVersionsOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	querierFunc := func(querier sqlc.Querier) ([]helper, error) {
		servers, err := querier.ListServerVersions(
			ctx,
			sqlc.ListServerVersionsParams{
				Name: options.Name,
				Next: options.Next,
				Prev: options.Prev,
				Size: int64(options.Limit),
			},
		)
		if err != nil {
			return nil, err
		}

		helpers := make([]helper, len(servers))
		for i, server := range servers {
			helpers[i] = listServerVersionsRowToHelper(server)
		}

		return helpers, nil
	}

	return s.sharedListServers(ctx, querierFunc)
}

// GetServer returns a specific server by name
func (s *dbService) GetServerVersion(
	ctx context.Context,
	opts ...service.Option[service.GetServerVersionOptions],
) (*upstreamv0.ServerJSON, error) {
	options := &service.GetServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	querierFunc := func(querier sqlc.Querier) ([]helper, error) {
		server, err := querier.GetServerVersion(
			ctx,
			sqlc.GetServerVersionParams{
				Name:    options.Name,
				Version: options.Version,
			},
		)
		if err != nil {
			return nil, err
		}

		return []helper{getServerVersionRowToHelper(server)}, nil
	}

	res, err := s.sharedListServers(ctx, querierFunc)
	if err != nil {
		return nil, err
	}

	return res[0], nil
}

func (s *dbService) sharedListServers(
	ctx context.Context,
	querierFunc func(querier sqlc.Querier) ([]helper, error),
) ([]*upstreamv0.ServerJSON, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	querier := sqlc.New(tx)

	servers, err := querierFunc(querier)
	if err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, len(servers))
	for i, server := range servers {
		ids[i] = server.ID
	}

	packages, err := querier.ListServerPackages(ctx, ids)
	if err != nil {
		return nil, err
	}
	packagesMap := make(map[uuid.UUID][]sqlc.McpServerPackage)
	for _, pkg := range packages {
		packagesMap[pkg.ServerID] = append(packagesMap[pkg.ServerID], pkg)
	}

	remotes, err := querier.ListServerRemotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	remotesMap := make(map[uuid.UUID][]sqlc.McpServerRemote)
	for _, remote := range remotes {
		remotesMap[remote.ServerID] = append(remotesMap[remote.ServerID], remote)
	}

	result := make([]*upstreamv0.ServerJSON, 0, len(servers))
	for _, dbServer := range servers {
		server := helperToServer(
			dbServer,
			packagesMap[dbServer.ID],
			remotesMap[dbServer.ID],
		)
		result = append(result, &server)
	}

	return result, nil
}

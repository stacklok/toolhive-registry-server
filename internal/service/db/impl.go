// Package database provides a database-backed implementation of the RegistryService interface
package database

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/validators"
)

const (
	// DefaultPageSize is the default number of items per page
	DefaultPageSize = 50
	// MaxPageSize is the maximum allowed items per page
	MaxPageSize = 1000
)

var (
	// ErrBug is returned when a server is not found
	ErrBug = errors.New("bug")
)

// options holds configuration options for the database service
type options struct {
	pool *pgxpool.Pool
}

// Option is a functional option for configuring the database service
type Option func(*options) error

// WithConnectionPool creates a new database-backed registry service with the
// given pgx pool. The caller is responsible for closing the pool when it is
// done.
func WithConnectionPool(pool *pgxpool.Pool) Option {
	return func(o *options) error {
		if pool == nil {
			return fmt.Errorf("pgx pool is required")
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
	options := &service.ListServersOptions{
		Limit: DefaultPageSize, // default limit
	}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	// Cap the limit at MaxPageSize to prevent potential DoS
	if options.Limit > MaxPageSize {
		options.Limit = MaxPageSize
	}

	slog.DebugContext(ctx, "ListServers query",
		"limit", options.Limit,
		"registry", options.RegistryName,
		"search", options.Search,
		"request_id", middleware.GetReqID(ctx))

	params := sqlc.ListServersParams{
		Size: int64(options.Limit),
	}
	if options.RegistryName != nil {
		params.RegistryName = options.RegistryName
	}
	if options.Search != "" {
		params.Search = &options.Search
	}

	if options.Cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(options.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor format: %w", err)
		}
		nextTime, err := time.Parse(time.RFC3339, string(decoded))
		if err != nil {
			return nil, fmt.Errorf("invalid cursor format: %w", err)
		}
		params.Next = &nextTime
	}

	// Note: this function fetches a list of servers. In case no records are
	// found, the called function should return an empty slice as it's
	// customary in Go.
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		servers, err := querier.ListServers(ctx, params)
		if err != nil {
			return nil, err
		}

		helpers := make([]helper, len(servers))
		for i, server := range servers {
			helpers[i] = listServersRowToHelper(server)
		}

		return helpers, nil
	}

	results, err := s.sharedListServers(ctx, querierFunc)
	if err == nil {
		slog.DebugContext(ctx, "ListServers completed",
			"count", len(results),
			"request_id", middleware.GetReqID(ctx))
	}
	return results, err
}

// ListServerVersions implements RegistryService.ListServerVersions
func (s *dbService) ListServerVersions(
	ctx context.Context,
	opts ...service.Option[service.ListServerVersionsOptions],
) ([]*upstreamv0.ServerJSON, error) {
	options := &service.ListServerVersionsOptions{
		Limit: DefaultPageSize, // default limit
	}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	if options.Next != nil && options.Prev != nil {
		return nil, fmt.Errorf("next and prev cannot be set at the same time")
	}

	// Cap the limit at MaxPageSize to prevent potential DoS
	if options.Limit > MaxPageSize {
		options.Limit = MaxPageSize
	}

	params := sqlc.ListServerVersionsParams{
		Name: options.Name,
		Next: options.Next,
		Prev: options.Prev,
		Size: int64(options.Limit),
	}
	if options.RegistryName != nil {
		params.RegistryName = options.RegistryName
	}

	// Note: this function fetches a list of server versions. In case no records are
	// found, the called function should return an empty slice as it's
	// customary in Go.
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		servers, err := querier.ListServerVersions(ctx, params)
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

	params := sqlc.GetServerVersionParams{
		Name:    options.Name,
		Version: options.Version,
	}
	if options.RegistryName != nil {
		params.RegistryName = options.RegistryName
	}

	// Note: this function fetches a single record given name and version.
	// In case no record is found, the called function should return an
	// `sql.ErrNoRows` error as it's customary in Go.
	querierFunc := func(ctx context.Context, querier sqlc.Querier) ([]helper, error) {
		server, err := querier.GetServerVersion(ctx, params)
		if err != nil {
			return nil, err
		}

		return []helper{getServerVersionRowToHelper(server)}, nil
	}

	res, err := s.sharedListServers(ctx, querierFunc)
	if err != nil {
		return nil, err
	}

	// Note: the `queryFunc` function is expected to return an error
	// sooner if no records are found, so getting this far with
	// a length result slice other than 1 means there's a bug.
	if len(res) != 1 {
		return nil, fmt.Errorf("%w: number of servers returned is not 1", ErrBug)
	}

	return res[0], nil
}

// insertServerVersionData inserts the server version record and returns the server ID.
// It validates unique constraints on (registry_id, name, version) and returns ErrVersionAlreadyExists if violated.
func insertServerVersionData(
	ctx context.Context,
	querier *sqlc.Queries,
	serverData *upstreamv0.ServerJSON,
	registryID uuid.UUID,
) (uuid.UUID, error) {
	// Prepare repository fields
	var repoURL, repoID, repoSubfolder, repoType *string
	if serverData.Repository != nil {
		repoURL = &serverData.Repository.URL
		repoID = &serverData.Repository.ID
		repoSubfolder = &serverData.Repository.Subfolder
		repoType = &serverData.Repository.Source
	}

	// Serialize publisher-provided metadata
	serverMeta, err := serializePublisherProvidedMeta(serverData.Meta)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to serialize metadata: %w", err)
	}

	// Insert the server version
	now := time.Now()
	serverID, err := querier.InsertServerVersion(ctx, sqlc.InsertServerVersionParams{
		Name:                serverData.Name,
		Version:             serverData.Version,
		RegID:               registryID,
		CreatedAt:           &now,
		UpdatedAt:           &now,
		Description:         &serverData.Description,
		Title:               &serverData.Title,
		Website:             &serverData.WebsiteURL,
		UpstreamMeta:        nil,
		ServerMeta:          serverMeta,
		RepositoryUrl:       repoURL,
		RepositoryID:        repoID,
		RepositorySubfolder: repoSubfolder,
		RepositoryType:      repoType,
	})
	if err != nil {
		// Check if this is a unique constraint violation
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, fmt.Errorf("%w: %s@%s",
				service.ErrVersionAlreadyExists, serverData.Name, serverData.Version)
		}
		return uuid.Nil, fmt.Errorf("failed to insert server version: %w", err)
	}

	return serverID, nil
}

// insertServerPackages inserts all packages for a server version.
// Each package includes transport configuration, runtime/package arguments, and environment variables.
func insertServerPackages(
	ctx context.Context,
	querier *sqlc.Queries,
	serverID uuid.UUID,
	packages []model.Package,
) error {
	for _, pkg := range packages {
		err := querier.InsertServerPackage(ctx, sqlc.InsertServerPackageParams{
			ServerID:         serverID,
			RegistryType:     pkg.RegistryType,
			PkgRegistryUrl:   pkg.RegistryBaseURL,
			PkgIdentifier:    pkg.Identifier,
			PkgVersion:       pkg.Version,
			RuntimeHint:      &pkg.RunTimeHint,
			RuntimeArguments: extractArgumentValues(pkg.RuntimeArguments),
			PackageArguments: extractArgumentValues(pkg.PackageArguments),
			EnvVars:          extractKeyValueNames(pkg.EnvironmentVariables),
			Sha256Hash:       &pkg.FileSHA256,
			Transport:        pkg.Transport.Type,
			TransportUrl:     &pkg.Transport.URL,
			TransportHeaders: extractKeyValueNames(pkg.Transport.Headers),
		})
		if err != nil {
			return fmt.Errorf("failed to insert server package: %w", err)
		}
	}
	return nil
}

// insertServerRemotes inserts all remotes for a server version.
// Remote transports (SSE, streamable-http) require a transport URL.
func insertServerRemotes(
	ctx context.Context,
	querier *sqlc.Queries,
	serverID uuid.UUID,
	remotes []model.Transport,
) error {
	for _, remote := range remotes {
		err := querier.InsertServerRemote(ctx, sqlc.InsertServerRemoteParams{
			ServerID:         serverID,
			Transport:        remote.Type,
			TransportUrl:     remote.URL,
			TransportHeaders: extractKeyValueNames(remote.Headers),
		})
		if err != nil {
			return fmt.Errorf("failed to insert server remote: %w", err)
		}
	}
	return nil
}

// insertServerIcons inserts all icons for a server version.
// Icons include source URI, MIME type, and theme (light/dark) attributes.
func insertServerIcons(
	ctx context.Context,
	querier *sqlc.Queries,
	serverID uuid.UUID,
	icons []model.Icon,
) error {
	for _, icon := range icons {
		// Convert theme string pointer to IconTheme enum
		var theme sqlc.IconTheme
		if icon.Theme != nil {
			switch *icon.Theme {
			case "light":
				theme = sqlc.IconThemeLIGHT
			case "dark":
				theme = sqlc.IconThemeDARK
			default:
				theme = sqlc.IconThemeLIGHT // Default to light if unknown
			}
		} else {
			theme = sqlc.IconThemeLIGHT // Default to light if not specified
		}

		// Get MIME type, default to empty string if not provided
		mimeType := ""
		if icon.MimeType != nil {
			mimeType = *icon.MimeType
		}

		err := querier.InsertServerIcon(ctx, sqlc.InsertServerIconParams{
			ServerID:  serverID,
			SourceUri: icon.Src,
			MimeType:  mimeType,
			Theme:     theme,
		})
		if err != nil {
			return fmt.Errorf("failed to insert server icon: %w", err)
		}
	}
	return nil
}

// validateManagedRegistry validates that the registry exists and is a managed (LOCAL) registry.
// Returns ErrRegistryNotFound if the registry doesn't exist, or ErrNotManagedRegistry if it's not a LOCAL type.
func validateManagedRegistry(
	ctx context.Context,
	querier *sqlc.Queries,
	registryName string,
) (*sqlc.Registry, error) {
	registryRow, err := querier.GetRegistryByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", service.ErrRegistryNotFound, registryName)
		}
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	if registryRow.RegType != sqlc.RegistryTypeMANAGED {
		return nil, fmt.Errorf("%w: registry %s has type %s",
			service.ErrNotManagedRegistry, registryName, registryRow.RegType)
	}

	// Convert row to Registry struct
	registry := &sqlc.Registry{
		ID:           registryRow.ID,
		Name:         registryRow.Name,
		RegType:      registryRow.RegType,
		CreationType: registryRow.CreationType,
		CreatedAt:    registryRow.CreatedAt,
		UpdatedAt:    registryRow.UpdatedAt,
	}

	return registry, nil
}

// PublishServerVersion publishes a server version to a managed registry
func (s *dbService) PublishServerVersion(
	ctx context.Context,
	opts ...service.Option[service.PublishServerVersionOptions],
) (*upstreamv0.ServerJSON, error) {
	// Parse options
	options := &service.PublishServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, fmt.Errorf("invalid option: %w", err)
		}
	}

	if options.ServerData == nil {
		return nil, fmt.Errorf("server data is required")
	}

	serverData := options.ServerData

	// Defensive check: validate server name format (should never fail if API layer is correct)
	if !validators.IsValidServerName(serverData.Name) {
		return nil, fmt.Errorf("invalid server name format: %s", serverData.Name)
	}

	// Execute the publish operation in a transaction
	if err := s.executePublishTransaction(ctx, options.RegistryName, serverData); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Server version published",
		"registry", options.RegistryName,
		"server", serverData.Name,
		"version", serverData.Version,
		"request_id", middleware.GetReqID(ctx))

	// Fetch the inserted server to return it
	result, err := s.GetServerVersion(ctx,
		service.WithRegistryName[service.GetServerVersionOptions](options.RegistryName),
		service.WithName[service.GetServerVersionOptions](serverData.Name),
		service.WithVersion[service.GetServerVersionOptions](serverData.Version),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch published server: %w", err)
	}

	return result, nil
}

// executePublishTransaction executes the publish operation within a transaction
func (s *dbService) executePublishTransaction(
	ctx context.Context,
	registryName string,
	serverData *upstreamv0.ServerJSON,
) error {
	// Begin transaction
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
			_ = err
		}
	}()

	querier := sqlc.New(tx)

	// Validate registry exists and is managed
	registry, err := validateManagedRegistry(ctx, querier, registryName)
	if err != nil {
		return err
	}

	// Insert server and related data
	if err := s.insertServerData(ctx, querier, serverData, registry.ID); err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// insertServerData inserts the server version and all related data
func (*dbService) insertServerData(
	ctx context.Context,
	querier *sqlc.Queries,
	serverData *upstreamv0.ServerJSON,
	registryID uuid.UUID,
) error {
	// Insert the server version
	serverID, err := insertServerVersionData(ctx, querier, serverData, registryID)
	if err != nil {
		return err
	}

	// Insert packages
	if err := insertServerPackages(ctx, querier, serverID, serverData.Packages); err != nil {
		return err
	}

	// Insert remotes
	if err := insertServerRemotes(ctx, querier, serverID, serverData.Remotes); err != nil {
		return err
	}

	// Insert icons
	if err := insertServerIcons(ctx, querier, serverID, serverData.Icons); err != nil {
		return err
	}

	// Upsert latest server version pointer
	_, err = querier.UpsertLatestServerVersion(ctx, sqlc.UpsertLatestServerVersionParams{
		RegID:    registryID,
		Name:     serverData.Name,
		Version:  serverData.Version,
		ServerID: serverID,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert latest server version: %w", err)
	}

	return nil
}

// DeleteServerVersion removes a server version from a managed registry
func (s *dbService) DeleteServerVersion(
	ctx context.Context,
	opts ...service.Option[service.DeleteServerVersionOptions],
) error {
	// 1. Parse options
	options := &service.DeleteServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return fmt.Errorf("invalid option: %w", err)
		}
	}

	// 2. Begin transaction
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
			// TODO: log the rollback error (add proper logging)
			_ = err
		}
	}()

	querier := sqlc.New(tx)

	// 3. Validate registry exists and get registry info
	registry, err := querier.GetRegistryByName(ctx, options.RegistryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", service.ErrRegistryNotFound, options.RegistryName)
		}
		return fmt.Errorf("failed to get registry: %w", err)
	}

	// 4. Validate registry is MANAGED type
	if registry.RegType != sqlc.RegistryTypeMANAGED {
		return fmt.Errorf("%w: registry %s has type %s",
			service.ErrNotManagedRegistry, options.RegistryName, registry.RegType)
	}

	// 5. Delete the server version
	rowsAffected, err := querier.DeleteServerVersion(ctx, sqlc.DeleteServerVersionParams{
		RegID:   registry.ID,
		Name:    options.ServerName,
		Version: options.Version,
	})
	if err != nil {
		return fmt.Errorf("failed to delete server version: %w", err)
	}

	// 5.1. Check if the server version was found and deleted
	if rowsAffected == 0 {
		return fmt.Errorf("%w: %s@%s",
			service.ErrServerNotFound, options.ServerName, options.Version)
	}

	// 6. Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Server version deleted",
		"registry", options.RegistryName,
		"server", options.ServerName,
		"version", options.Version,
		"request_id", middleware.GetReqID(ctx))

	return nil
}

// querierFunction is a function that uses the given querier object to run the
// main extraction. As of the time of this writing, its main use is accessing
// the `mcp_server` table in a type-agnostic way. This is to overcome a
// limitation of sqlc that prevents us from having the exact same go type
// despite the fact that the underlying columns returned are the same.
//
// Note that the underlying table does not have to be the `mcp_server` table
// as it is used now, as long as the result is a slice of helpers.
type querierFunction func(ctx context.Context, querier sqlc.Querier) ([]helper, error)

// sharedListServers is a helper function to list servers and mapping them to
// the API schema.
//
// Its responsibilities are:
// * Begin a transaction
// * Execute the querier function
// * List packages and remotes using the server IDs
// * Map the results to the API schema
// * Return the results
//
// The argument `querierFunc` is a function that uses the given querier object
// to run the main extraction. Note that the underlying table does not have
// to be the `mcp_server` table as it is used now, as long as the result is a
// slice of helpers.
func (s *dbService) sharedListServers(
	ctx context.Context,
	querierFunc querierFunction,
) ([]*upstreamv0.ServerJSON, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			// TODO: log the rollback error (add proper logging)
			_ = err
		}
	}()

	querier := sqlc.New(tx)

	servers, err := querierFunc(ctx, querier)
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

// ListRegistries returns all configured registries
func (s *dbService) ListRegistries(ctx context.Context) ([]service.RegistryInfo, error) {
	// Begin a read-only transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			// TODO: log the rollback error (add proper logging)
			_ = err
		}
	}()

	querier := sqlc.New(tx)

	// List all registries (no pagination for now)
	params := sqlc.ListRegistriesParams{
		Size: MaxPageSize, // Maximum number of registries to return
	}

	registries, err := querier.ListRegistries(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}

	// Convert to API response format
	result := make([]service.RegistryInfo, 0, len(registries))
	for _, reg := range registries {
		info := service.RegistryInfo{
			Name:      reg.Name,
			Type:      string(reg.RegType), // MANAGED, FILE, REMOTE
			CreatedAt: *reg.CreatedAt,
			UpdatedAt: *reg.UpdatedAt,
		}

		// Fetch sync status from database
		syncRecord, err := querier.GetRegistrySyncByName(ctx, reg.Name)
		if err != nil {
			// It's okay if sync record doesn't exist yet (registry may not have been synced)
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("Failed to get sync status for registry",
					"registry", reg.Name,
					"error", err)
			}
			// Leave SyncStatus as nil if not found or error
			info.SyncStatus = nil
		} else {
			// Convert database sync status to service type
			info.SyncStatus = &service.RegistrySyncStatus{
				Phase:        convertSyncPhase(syncRecord.SyncStatus),
				LastSyncTime: syncRecord.EndedAt,   // EndedAt represents successful completion
				LastAttempt:  syncRecord.StartedAt, // StartedAt is the last attempt time
				AttemptCount: int(syncRecord.AttemptCount),
				ServerCount:  int(syncRecord.ServerCount),
				Message:      getStatusMessage(syncRecord.ErrorMsg),
			}
		}

		result = append(result, info)
	}

	return result, nil
}

// convertSyncPhase converts database SyncStatus enum to service phase string
func convertSyncPhase(status sqlc.SyncStatus) string {
	switch status {
	case sqlc.SyncStatusINPROGRESS:
		return "syncing"
	case sqlc.SyncStatusCOMPLETED:
		return "complete"
	case sqlc.SyncStatusFAILED:
		return "failed"
	default:
		return "unknown"
	}
}

// getStatusMessage converts error message pointer to string
func getStatusMessage(errorMsg *string) string {
	if errorMsg == nil || *errorMsg == "" {
		return ""
	}
	return *errorMsg
}

// GetRegistryByName returns a single registry by name
func (s *dbService) GetRegistryByName(ctx context.Context, name string) (*service.RegistryInfo, error) {
	// Begin a read-only transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			// TODO: log the rollback error (add proper logging)
			_ = err
		}
	}()

	querier := sqlc.New(tx)

	// Get the registry by name
	registry, err := querier.GetRegistryByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
		}
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	// Convert to service type
	info := service.RegistryInfo{
		Name:      registry.Name,
		Type:      string(registry.RegType), // MANAGED, FILE, REMOTE
		CreatedAt: *registry.CreatedAt,
		UpdatedAt: *registry.UpdatedAt,
	}

	// Fetch sync status from database
	syncRecord, err := querier.GetRegistrySyncByName(ctx, registry.Name)
	if err != nil {
		// It's okay if sync record doesn't exist yet (registry may not have been synced)
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("Failed to get sync status for registry",
				"registry", registry.Name,
				"error", err)
		}
		// Leave SyncStatus as nil if not found or error
		info.SyncStatus = nil
	} else {
		// Convert database sync status to service type
		info.SyncStatus = &service.RegistrySyncStatus{
			Phase:        convertSyncPhase(syncRecord.SyncStatus),
			LastSyncTime: syncRecord.EndedAt,   // EndedAt represents successful completion
			LastAttempt:  syncRecord.StartedAt, // StartedAt is the last attempt time
			AttemptCount: int(syncRecord.AttemptCount),
			ServerCount:  int(syncRecord.ServerCount),
			Message:      getStatusMessage(syncRecord.ErrorMsg),
		}
	}

	return &info, nil
}

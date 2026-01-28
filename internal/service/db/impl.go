// Package database provides a database-backed implementation of the RegistryService interface
package database

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
	"github.com/stacklok/toolhive-registry-server/internal/validators"
)

const (
	// DefaultPageSize is the default number of items per page
	DefaultPageSize = 50
	// MaxPageSize is the maximum allowed items per page
	MaxPageSize = 1000
	// unknownSubtype is used when a file source subtype cannot be determined
	unknownSubtype = "unknown"
)

var (
	// ErrBug is returned when a server is not found
	ErrBug = errors.New("bug")
)

// options holds configuration options for the database service
type options struct {
	pool   *pgxpool.Pool
	tracer trace.Tracer
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

// WithTracer sets the OpenTelemetry tracer for the database service.
// If not set, tracing will be disabled (no-op).
func WithTracer(tracer trace.Tracer) Option {
	return func(o *options) error {
		o.tracer = tracer
		return nil
	}
}

// dbService implements the RegistryService interface using a database backend
type dbService struct {
	pool   *pgxpool.Pool
	tracer trace.Tracer
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
		pool:   o.pool,
		tracer: o.tracer,
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
	ctx, span := s.startSpan(ctx, "dbService.ListServers")
	defer span.End()

	options := &service.ListServersOptions{
		Limit: DefaultPageSize, // default limit
	}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	// Cap the limit at MaxPageSize to prevent potential DoS
	if options.Limit > MaxPageSize {
		options.Limit = MaxPageSize
	}

	// Add tracing attributes after options are parsed
	span.SetAttributes(
		otel.AttrPageSize.Int(options.Limit),
		otel.AttrHasCursor.Bool(options.Cursor != ""),
	)
	if options.RegistryName != nil {
		span.SetAttributes(otel.AttrRegistryName.String(*options.RegistryName))
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
			otel.RecordError(span, err)
			return nil, fmt.Errorf("invalid cursor format: %w", err)
		}
		nextTime, err := time.Parse(time.RFC3339, string(decoded))
		if err != nil {
			otel.RecordError(span, err)
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
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(results)))
	slog.DebugContext(ctx, "ListServers completed",
		"count", len(results),
		"request_id", middleware.GetReqID(ctx))
	return results, nil
}

// ListServerVersions implements RegistryService.ListServerVersions
func (s *dbService) ListServerVersions(
	ctx context.Context,
	opts ...service.Option[service.ListServerVersionsOptions],
) ([]*upstreamv0.ServerJSON, error) {
	ctx, span := s.startSpan(ctx, "dbService.ListServerVersions")
	defer span.End()

	options := &service.ListServerVersionsOptions{
		Limit: DefaultPageSize, // default limit
	}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	if options.Next != nil && options.Prev != nil {
		err := fmt.Errorf("next and prev cannot be set at the same time")
		otel.RecordError(span, err)
		return nil, err
	}

	// Cap the limit at MaxPageSize to prevent potential DoS
	if options.Limit > MaxPageSize {
		options.Limit = MaxPageSize
	}

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrServerName.String(options.Name),
		otel.AttrPageSize.Int(options.Limit),
	)
	if options.RegistryName != nil {
		span.SetAttributes(otel.AttrRegistryName.String(*options.RegistryName))
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

	results, err := s.sharedListServers(ctx, querierFunc)
	if err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(results)))
	return results, nil
}

// GetServer returns a specific server by name
func (s *dbService) GetServerVersion(
	ctx context.Context,
	opts ...service.Option[service.GetServerVersionOptions],
) (*upstreamv0.ServerJSON, error) {
	ctx, span := s.startSpan(ctx, "dbService.GetServerVersion")
	defer span.End()

	options := &service.GetServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrServerName.String(options.Name),
		otel.AttrServerVersion.String(options.Version),
	)
	if options.RegistryName != nil {
		span.SetAttributes(otel.AttrRegistryName.String(*options.RegistryName))
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
		otel.RecordError(span, err)
		return nil, err
	}

	// Note: the `queryFunc` function is expected to return an error
	// sooner if no records are found, so getting this far with
	// a length result slice other than 1 means there's a bug.
	if len(res) != 1 {
		err := fmt.Errorf("%w: number of servers returned is not 1", ErrBug)
		otel.RecordError(span, err)
		return nil, err
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
		envVarsJSON, err := serializeKeyValueInputs(pkg.EnvironmentVariables)
		if err != nil {
			return fmt.Errorf("failed to serialize env vars: %w", err)
		}

		transportHeadersJSON, err := serializeKeyValueInputs(pkg.Transport.Headers)
		if err != nil {
			return fmt.Errorf("failed to serialize transport headers: %w", err)
		}

		err = querier.InsertServerPackage(ctx, sqlc.InsertServerPackageParams{
			ServerID:         serverID,
			RegistryType:     pkg.RegistryType,
			PkgRegistryUrl:   pkg.RegistryBaseURL,
			PkgIdentifier:    pkg.Identifier,
			PkgVersion:       pkg.Version,
			RuntimeHint:      &pkg.RunTimeHint,
			RuntimeArguments: extractArgumentValues(pkg.RuntimeArguments),
			PackageArguments: extractArgumentValues(pkg.PackageArguments),
			EnvVars:          envVarsJSON,
			Sha256Hash:       &pkg.FileSHA256,
			Transport:        pkg.Transport.Type,
			TransportUrl:     &pkg.Transport.URL,
			TransportHeaders: transportHeadersJSON,
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
		headersJSON, err := serializeKeyValueInputs(remote.Headers)
		if err != nil {
			return fmt.Errorf("failed to serialize transport headers: %w", err)
		}

		err = querier.InsertServerRemote(ctx, sqlc.InsertServerRemoteParams{
			ServerID:         serverID,
			Transport:        remote.Type,
			TransportUrl:     remote.URL,
			TransportHeaders: headersJSON,
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
	ctx, span := s.startSpan(ctx, "dbService.PublishServerVersion")
	defer span.End()

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
		otel.AttrRegistryName.String(options.RegistryName),
		otel.AttrServerName.String(serverData.Name),
		otel.AttrServerVersion.String(serverData.Version),
	)

	// Defensive check: validate server name format (should never fail if API layer is correct)
	if !validators.IsValidServerName(serverData.Name) {
		err := fmt.Errorf("invalid server name format: %s", serverData.Name)
		otel.RecordError(span, err)
		return nil, err
	}

	// Execute the publish operation in a transaction
	if err := s.executePublishTransaction(ctx, options.RegistryName, serverData); err != nil {
		otel.RecordError(span, err)
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
		otel.RecordError(span, err)
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
	ctx, span := s.startSpan(ctx, "dbService.DeleteServerVersion")
	defer span.End()

	// 1. Parse options
	options := &service.DeleteServerVersionOptions{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			otel.RecordError(span, err)
			return fmt.Errorf("invalid option: %w", err)
		}
	}

	// Add tracing attributes
	span.SetAttributes(
		otel.AttrRegistryName.String(options.RegistryName),
		otel.AttrServerName.String(options.ServerName),
		otel.AttrServerVersion.String(options.Version),
	)

	// 2. Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		otel.RecordError(span, err)
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
			err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, options.RegistryName)
			otel.RecordError(span, err)
			return err
		}
		otel.RecordError(span, err)
		return fmt.Errorf("failed to get registry: %w", err)
	}

	// 4. Validate registry is MANAGED type
	if registry.RegType != sqlc.RegistryTypeMANAGED {
		err = fmt.Errorf("%w: registry %s has type %s",
			service.ErrNotManagedRegistry, options.RegistryName, registry.RegType)
		otel.RecordError(span, err)
		return err
	}

	// 5. Delete the server version
	rowsAffected, err := querier.DeleteServerVersion(ctx, sqlc.DeleteServerVersionParams{
		RegID:   registry.ID,
		Name:    options.ServerName,
		Version: options.Version,
	})
	if err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to delete server version: %w", err)
	}

	// 5.1. Check if the server version was found and deleted
	if rowsAffected == 0 {
		err = fmt.Errorf("%w: %s@%s",
			service.ErrServerNotFound, options.ServerName, options.Version)
		otel.RecordError(span, err)
		return err
	}

	// 6. Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
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
	packagesMap := make(map[uuid.UUID][]sqlc.ListServerPackagesRow)
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
	ctx, span := s.startSpan(ctx, "dbService.ListRegistries")
	defer span.End()

	// Begin a read-only transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		otel.RecordError(span, err)
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
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}

	// Convert to API response format
	result := make([]service.RegistryInfo, 0, len(registries))
	for _, reg := range registries {
		// Build RegistryInfo with all config fields
		info := buildRegistryInfoFromListRow(&reg)

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

		result = append(result, *info)
	}

	span.SetAttributes(otel.AttrResultCount.Int(len(result)))
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
	ctx, span := s.startSpan(ctx, "dbService.GetRegistryByName")
	defer span.End()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Begin a read-only transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		otel.RecordError(span, err)
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
			err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}

	// Build RegistryInfo with all config fields
	info := buildRegistryInfoFromGetByNameRow(&registry)

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

	return info, nil
}

// CreateRegistry creates a new API-managed registry
func (s *dbService) CreateRegistry(
	ctx context.Context,
	name string,
	req *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.CreateRegistry")
	defer span.End()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Validate configuration
	if err := service.ValidateRegistryConfig(req); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("%w: %v", service.ErrInvalidRegistryConfig, err)
	}

	// Add registry type attribute after validation
	span.SetAttributes(attribute.String("registry.type", string(req.GetSourceType())))

	// Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			_ = err
		}
	}()

	querier := sqlc.New(tx)

	// Check if registry already exists
	_, err = querier.GetRegistryByName(ctx, name)
	if err == nil {
		err = fmt.Errorf("%w: %s", service.ErrRegistryAlreadyExists, name)
		otel.RecordError(span, err)
		return nil, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to check registry existence: %w", err)
	}

	// Prepare insert parameters
	now := time.Now()
	sourceType := string(req.GetSourceType())
	format := req.Format
	if format == "" {
		format = "upstream" // default format
	}

	sourceConfig := serializeSourceConfigFromRequest(req)
	filterConfig := serializeFilterConfigFromRequest(req.Filter)
	syncSchedule := parseSyncScheduleFromRequest(req)
	syncable := !req.IsNonSyncedType()

	params := sqlc.InsertAPIRegistryParams{
		Name:         name,
		RegType:      mapSourceTypeToDBType(req.GetSourceType()),
		SourceType:   &sourceType,
		Format:       &format,
		SourceConfig: sourceConfig,
		FilterConfig: filterConfig,
		SyncSchedule: syncSchedule,
		Syncable:     syncable,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}

	// Insert the registry
	registry, err := querier.InsertAPIRegistry(ctx, params)
	if err != nil {
		// Check for unique constraint violation
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			err = fmt.Errorf("%w: %s", service.ErrRegistryAlreadyExists, name)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to insert registry: %w", err)
	}

	// Initialize sync status for the new registry
	// Follow same pattern as CONFIG registries:
	// - Non-synced (managed, kubernetes): COMPLETED
	// - Synced (git, api, file): FAILED with "No previous sync status found"
	// - Inline data: FAILED (needs processing, will be updated to COMPLETED after)
	initialSyncStatus := sqlc.SyncStatusFAILED
	initialErrorMsg := "No previous sync status found"
	if req.IsNonSyncedType() && !req.IsInlineData() {
		// Only managed/kubernetes start as COMPLETED
		// Inline data needs processing first
		initialSyncStatus = sqlc.SyncStatusCOMPLETED
		initialErrorMsg = fmt.Sprintf("Non-synced registry (type: %s)", sourceType)
	}
	err = querier.BulkInitializeRegistrySyncs(ctx, sqlc.BulkInitializeRegistrySyncsParams{
		RegIds:       []uuid.UUID{registry.ID},
		SyncStatuses: []sqlc.SyncStatus{initialSyncStatus},
		ErrorMsgs:    []string{initialErrorMsg},
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to initialize sync status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Registry created",
		"name", name,
		"type", registry.RegType,
		"source_type", sourceType,
		"request_id", middleware.GetReqID(ctx))

	// Build and return RegistryInfo
	return buildRegistryInfoFromDBRegistry(&registry), nil
}

// UpdateRegistry updates an existing API registry
func (s *dbService) UpdateRegistry(
	ctx context.Context,
	name string,
	req *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	ctx, span := s.startSpan(ctx, "dbService.UpdateRegistry")
	defer span.End()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Validate configuration
	if err := service.ValidateRegistryConfig(req); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("%w: %v", service.ErrInvalidRegistryConfig, err)
	}

	// Check if source type is changing (not allowed)
	if err := s.validateSourceTypeChange(ctx, name, req.GetSourceType()); err != nil {
		otel.RecordError(span, err)
		return nil, err
	}

	// For file source types, check if the subtype (path/url/data) is changing
	// We don't allow changing between path, url, and data - user must delete and recreate
	if req.GetSourceType() == config.SourceTypeFile {
		if err := s.validateFileSourceTypeChange(ctx, name, req); err != nil {
			otel.RecordError(span, err)
			return nil, err
		}
	}

	// Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			_ = err
		}
	}()

	querier := sqlc.New(tx)

	// Prepare update parameters
	now := time.Now()
	sourceType := string(req.GetSourceType())
	format := req.Format
	if format == "" {
		format = "upstream" // default format
	}

	sourceConfig := serializeSourceConfigFromRequest(req)
	filterConfig := serializeFilterConfigFromRequest(req.Filter)
	syncSchedule := parseSyncScheduleFromRequest(req)
	syncable := !req.IsNonSyncedType()

	params := sqlc.UpdateAPIRegistryParams{
		Name:         name,
		RegType:      mapSourceTypeToDBType(req.GetSourceType()),
		SourceType:   &sourceType,
		Format:       &format,
		SourceConfig: sourceConfig,
		FilterConfig: filterConfig,
		SyncSchedule: syncSchedule,
		Syncable:     syncable,
		UpdatedAt:    &now,
	}

	// Update the registry (only updates API type registries)
	registry, err := querier.UpdateAPIRegistry(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Registry not found or is CONFIG type, check which case
			existing, checkErr := querier.GetRegistryByName(ctx, name)
			if checkErr != nil {
				if errors.Is(checkErr, pgx.ErrNoRows) {
					err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
					otel.RecordError(span, err)
					return nil, err
				}
				otel.RecordError(span, checkErr)
				return nil, fmt.Errorf("failed to check registry: %w", checkErr)
			}
			// Registry exists but is CONFIG type
			if existing.CreationType == sqlc.CreationTypeCONFIG {
				err = fmt.Errorf("%w: %s", service.ErrConfigRegistry, name)
				otel.RecordError(span, err)
				return nil, err
			}
			// Should not reach here, but return not found just in case
			err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
			otel.RecordError(span, err)
			return nil, err
		}
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to update registry: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Registry updated",
		"name", name,
		"type", registry.RegType,
		"source_type", sourceType,
		"request_id", middleware.GetReqID(ctx))

	// Build and return RegistryInfo
	return buildRegistryInfoFromDBRegistry(&registry), nil
}

// DeleteRegistry deletes an API registry
func (s *dbService) DeleteRegistry(ctx context.Context, name string) error {
	ctx, span := s.startSpan(ctx, "dbService.DeleteRegistry")
	defer span.End()

	// Add tracing attributes
	span.SetAttributes(otel.AttrRegistryName.String(name))

	// Begin transaction
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			_ = err
		}
	}()

	querier := sqlc.New(tx)

	// Delete the registry (only deletes API type registries)
	rowsAffected, err := querier.DeleteAPIRegistry(ctx, name)
	if err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to delete registry: %w", err)
	}

	if rowsAffected == 0 {
		// Registry not found or is CONFIG type, check which case
		existing, checkErr := querier.GetRegistryByName(ctx, name)
		if checkErr != nil {
			if errors.Is(checkErr, pgx.ErrNoRows) {
				err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
				otel.RecordError(span, err)
				return err
			}
			otel.RecordError(span, checkErr)
			return fmt.Errorf("failed to check registry: %w", checkErr)
		}
		// Registry exists but is CONFIG type
		if existing.CreationType == sqlc.CreationTypeCONFIG {
			err = fmt.Errorf("%w: %s", service.ErrConfigRegistry, name)
			otel.RecordError(span, err)
			return err
		}
		// Should not reach here, but return not found just in case
		err = fmt.Errorf("%w: %s", service.ErrRegistryNotFound, name)
		otel.RecordError(span, err)
		return err
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		otel.RecordError(span, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "Registry deleted",
		"name", name,
		"request_id", middleware.GetReqID(ctx))

	return nil
}

// =============================================================================
// Helper functions for registry CRUD operations
// =============================================================================

// mapSourceTypeToDBType maps config.SourceType to database RegistryType
func mapSourceTypeToDBType(sourceType config.SourceType) sqlc.RegistryType {
	switch sourceType {
	case config.SourceTypeManaged:
		return sqlc.RegistryTypeMANAGED
	case config.SourceTypeFile:
		return sqlc.RegistryTypeFILE
	case config.SourceTypeGit, config.SourceTypeAPI:
		return sqlc.RegistryTypeREMOTE
	case config.SourceTypeKubernetes:
		return sqlc.RegistryTypeKUBERNETES
	default:
		return sqlc.RegistryTypeREMOTE
	}
}

// serializeSourceConfigFromRequest serializes the source config from request to JSON bytes
func serializeSourceConfigFromRequest(req *service.RegistryCreateRequest) []byte {
	if req == nil {
		return nil
	}

	sourceConfig := req.GetSourceConfig()
	if sourceConfig == nil {
		return nil
	}

	data, err := json.Marshal(sourceConfig)
	if err != nil {
		return nil
	}
	return data
}

// serializeFilterConfigFromRequest serializes the filter config from request to JSON bytes
func serializeFilterConfigFromRequest(filter *config.FilterConfig) []byte {
	if filter == nil {
		return nil
	}

	data, err := json.Marshal(filter)
	if err != nil {
		return nil
	}
	return data
}

// parseSyncScheduleFromRequest parses the sync schedule from request to pgtypes.Interval
func parseSyncScheduleFromRequest(req *service.RegistryCreateRequest) pgtypes.Interval {
	if req == nil || req.SyncPolicy == nil || req.SyncPolicy.Interval == "" {
		return pgtypes.NewNullInterval()
	}

	interval, err := pgtypes.ParseDuration(req.SyncPolicy.Interval)
	if err != nil {
		return pgtypes.NewNullInterval()
	}
	return interval
}

// deserializeSourceConfig deserializes source config from JSON bytes based on source type
func deserializeSourceConfig(sourceType string, data []byte) interface{} {
	if len(data) == 0 {
		return nil
	}

	switch config.SourceType(sourceType) {
	case config.SourceTypeGit:
		var cfg config.GitConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	case config.SourceTypeAPI:
		var cfg config.APIConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	case config.SourceTypeFile:
		var cfg config.FileConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	case config.SourceTypeManaged:
		var cfg config.ManagedConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	case config.SourceTypeKubernetes:
		var cfg config.KubernetesConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	default:
		return nil
	}
}

// deserializeFilterConfig deserializes filter config from JSON bytes
func deserializeFilterConfig(data []byte) *config.FilterConfig {
	if len(data) == 0 {
		return nil
	}

	var cfg config.FilterConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// buildRegistryInfoFromDBRegistry builds a RegistryInfo from a database Registry
func buildRegistryInfoFromDBRegistry(registry *sqlc.Registry) *service.RegistryInfo {
	info := &service.RegistryInfo{
		Name:         registry.Name,
		Type:         string(registry.RegType),
		CreationType: service.CreationType(registry.CreationType),
	}

	if registry.SourceType != nil {
		info.SourceType = config.SourceType(*registry.SourceType)
	}

	if registry.Format != nil {
		info.Format = *registry.Format
	}

	if registry.SourceType != nil {
		info.SourceConfig = deserializeSourceConfig(*registry.SourceType, registry.SourceConfig)
	}

	info.FilterConfig = deserializeFilterConfig(registry.FilterConfig)

	if registry.SyncSchedule.Valid {
		info.SyncSchedule = registry.SyncSchedule.Duration.String()
	}

	if registry.CreatedAt != nil {
		info.CreatedAt = *registry.CreatedAt
	}

	if registry.UpdatedAt != nil {
		info.UpdatedAt = *registry.UpdatedAt
	}

	return info
}

// buildRegistryInfoFromListRow builds a RegistryInfo from a ListRegistriesRow
func buildRegistryInfoFromListRow(row *sqlc.ListRegistriesRow) *service.RegistryInfo {
	info := &service.RegistryInfo{
		Name:         row.Name,
		Type:         string(row.RegType),
		CreationType: service.CreationType(row.CreationType),
	}

	if row.SourceType != nil {
		info.SourceType = config.SourceType(*row.SourceType)
	}

	if row.Format != nil {
		info.Format = *row.Format
	}

	if row.SourceType != nil {
		info.SourceConfig = deserializeSourceConfig(*row.SourceType, row.SourceConfig)
	}

	info.FilterConfig = deserializeFilterConfig(row.FilterConfig)

	if row.SyncSchedule.Valid {
		info.SyncSchedule = row.SyncSchedule.Duration.String()
	}

	if row.CreatedAt != nil {
		info.CreatedAt = *row.CreatedAt
	}

	if row.UpdatedAt != nil {
		info.UpdatedAt = *row.UpdatedAt
	}

	return info
}

// buildRegistryInfoFromGetByNameRow builds a RegistryInfo from a GetRegistryByNameRow
func buildRegistryInfoFromGetByNameRow(row *sqlc.GetRegistryByNameRow) *service.RegistryInfo {
	info := &service.RegistryInfo{
		Name:         row.Name,
		Type:         string(row.RegType),
		CreationType: service.CreationType(row.CreationType),
	}

	if row.SourceType != nil {
		info.SourceType = config.SourceType(*row.SourceType)
	}

	if row.Format != nil {
		info.Format = *row.Format
	}

	if row.SourceType != nil {
		info.SourceConfig = deserializeSourceConfig(*row.SourceType, row.SourceConfig)
	}

	info.FilterConfig = deserializeFilterConfig(row.FilterConfig)

	if row.SyncSchedule.Valid {
		info.SyncSchedule = row.SyncSchedule.Duration.String()
	}

	if row.CreatedAt != nil {
		info.CreatedAt = *row.CreatedAt
	}

	if row.UpdatedAt != nil {
		info.UpdatedAt = *row.UpdatedAt
	}

	return info
}

// ProcessInlineRegistryData processes inline registry data synchronously.
// It parses the data, validates it, and stores the servers in the database.
func (s *dbService) ProcessInlineRegistryData(ctx context.Context, name string, data string, format string) error {
	// Capture actual start time for accurate timing
	startTime := time.Now()

	// Parse the inline data using the registry validator
	validator := sources.NewRegistryDataValidator()
	registry, err := validator.ValidateData([]byte(data), format)
	if err != nil {
		// Update sync status to failed with actual timing
		errMsg := fmt.Sprintf("failed to parse registry data: %v", err)
		if updateErr := s.updateSyncStatusFailed(ctx, name, errMsg, startTime); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update sync status after parse error",
				"registry", name,
				"parse_error", err,
				"update_error", updateErr)
		}
		return fmt.Errorf("failed to parse inline registry data: %w", err)
	}

	// Create a sync writer to store the servers
	syncWriter, err := writer.NewDBSyncWriter(s.pool)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create sync writer: %v", err)
		if updateErr := s.updateSyncStatusFailed(ctx, name, errMsg, startTime); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update sync status after writer creation error",
				"registry", name,
				"writer_error", err,
				"update_error", updateErr)
		}
		return fmt.Errorf("failed to create sync writer: %w", err)
	}

	// Store the servers in the database
	if err := syncWriter.Store(ctx, name, registry); err != nil {
		errMsg := fmt.Sprintf("failed to store servers: %v", err)
		if updateErr := s.updateSyncStatusFailed(ctx, name, errMsg, startTime); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update sync status after store error",
				"registry", name,
				"store_error", err,
				"update_error", updateErr)
		}
		return fmt.Errorf("failed to store inline registry data: %w", err)
	}

	// Update sync status to completed with actual timing
	if err := s.updateSyncStatusCompleted(ctx, name, len(registry.Data.Servers), startTime); err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status to completed",
			"registry", name,
			"error", err)
		// Don't return error here - the data was successfully stored
	}

	slog.InfoContext(ctx, "Inline registry data processed successfully",
		"registry", name,
		"server_count", len(registry.Data.Servers))

	return nil
}

// updateSyncStatusFailed updates the sync status to failed with an error message.
// startTime is the time when processing began, endTime is captured when this is called.
func (s *dbService) updateSyncStatusFailed(
	ctx context.Context, name string, errorMsg string, startTime time.Time,
) error {
	querier := sqlc.New(s.pool)
	endTime := time.Now()
	return querier.UpsertRegistrySyncByName(ctx, sqlc.UpsertRegistrySyncByNameParams{
		Name:         name,
		SyncStatus:   sqlc.SyncStatusFAILED,
		ErrorMsg:     &errorMsg,
		StartedAt:    &startTime,
		EndedAt:      &endTime,
		AttemptCount: 1,
		ServerCount:  0,
	})
}

// updateSyncStatusCompleted updates the sync status to completed.
// startTime is the time when processing began, endTime is captured when this is called.
func (s *dbService) updateSyncStatusCompleted(
	ctx context.Context, name string, serverCount int, startTime time.Time,
) error {
	querier := sqlc.New(s.pool)
	endTime := time.Now()
	return querier.UpsertRegistrySyncByName(ctx, sqlc.UpsertRegistrySyncByNameParams{
		Name:         name,
		SyncStatus:   sqlc.SyncStatusCOMPLETED,
		ErrorMsg:     nil,
		StartedAt:    &startTime,
		EndedAt:      &endTime,
		AttemptCount: 1,
		ServerCount:  int64(serverCount),
	})
}

// validateSourceTypeChange checks if the registry source type is changing and returns an error if so.
// Users cannot change a registry's source type (e.g., git to file) - they must delete and recreate.
func (s *dbService) validateSourceTypeChange(
	ctx context.Context, name string, newSourceType config.SourceType,
) error {
	querier := sqlc.New(s.pool)
	existing, err := querier.GetRegistryByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Registry doesn't exist yet, no validation needed
			return nil
		}
		return fmt.Errorf("failed to get existing registry: %w", err)
	}

	// Check if source type is changing
	if existing.SourceType != nil && *existing.SourceType != string(newSourceType) {
		return fmt.Errorf("%w: cannot change from '%s' to '%s', delete and recreate the registry instead",
			service.ErrSourceTypeChangeNotAllowed, *existing.SourceType, newSourceType)
	}

	return nil
}

// validateFileSourceTypeChange checks if the file source subtype (path/url/data) is changing
// and returns an error if so. Users must delete and recreate to change file source subtypes.
func (s *dbService) validateFileSourceTypeChange(
	ctx context.Context, name string, newConfig *service.RegistryCreateRequest,
) error {
	// Get the existing registry
	querier := sqlc.New(s.pool)
	existing, err := querier.GetRegistryByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Registry doesn't exist yet, no validation needed
			return nil
		}
		return fmt.Errorf("failed to get existing registry: %w", err)
	}

	// If the existing registry is not a file type, no validation needed
	if existing.SourceType == nil || *existing.SourceType != string(config.SourceTypeFile) {
		return nil
	}

	// Deserialize the existing source config
	var existingFileConfig config.FileConfig
	if len(existing.SourceConfig) > 0 {
		if err := json.Unmarshal(existing.SourceConfig, &existingFileConfig); err != nil {
			return fmt.Errorf("failed to parse existing file config: %w", err)
		}
	}

	// Determine the existing and new file subtypes
	existingSubtype := getFileSourceSubtype(&existingFileConfig)
	newSubtype := getFileSourceSubtype(newConfig.File)

	// Check if the subtype is changing
	if existingSubtype != newSubtype {
		return fmt.Errorf("%w: cannot change file source type from '%s' to '%s', delete and recreate the registry instead",
			service.ErrInvalidRegistryConfig, existingSubtype, newSubtype)
	}

	return nil
}

// getFileSourceSubtype returns which file source subtype is being used (path, url, or data)
func getFileSourceSubtype(cfg *config.FileConfig) string {
	if cfg == nil {
		return unknownSubtype
	}
	switch {
	case cfg.Path != "":
		return "path"
	case cfg.URL != "":
		return "url"
	case cfg.Data != "":
		return "data"
	default:
		return unknownSubtype
	}
}

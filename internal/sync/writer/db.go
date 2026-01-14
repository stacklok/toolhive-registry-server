// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// Theme constants for icons (must match PostgreSQL icon_theme enum values)
const (
	iconThemeLight = "LIGHT"
	iconThemeDark  = "DARK"
)

// dbSyncWriter is a SyncWriter implementation that persists data to a database
type dbSyncWriter struct {
	pool *pgxpool.Pool
}

// NewDBSyncWriter creates a new dbSyncWriter with the given connection pool.
// The caller is responsible for closing the pool when done.
func NewDBSyncWriter(pool *pgxpool.Pool) (SyncWriter, error) {
	if pool == nil {
		return nil, fmt.Errorf("pgx pool is required")
	}
	return &dbSyncWriter{pool: pool}, nil
}

// Store saves a UpstreamRegistry instance to database storage for a specific registry.
//
// This method performs an efficient bulk sync using temporary tables and COPY operations:
// 1. Validates the registry exists
// 2. Creates temp tables, copies server data, and bulk upserts to preserve existing UUIDs
// 3. Deletes orphaned servers that no longer exist in upstream (CASCADE cleans related data)
// 4. For packages/remotes/icons: creates temp tables, copies data, bulk upserts, deletes orphans
// 5. Updates the latest_server_version table for each unique server name
//
// The operation is performed within a serializable transaction to ensure consistency.
// Temp tables are automatically dropped at transaction end (ON COMMIT DROP).
func (d *dbSyncWriter) Store(ctx context.Context, registryName string, reg *toolhivetypes.UpstreamRegistry) error {
	if reg == nil {
		return fmt.Errorf("registry data is required")
	}

	// Begin transaction with serializable isolation level for consistency
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			// Log rollback error in production; here we just ignore it
			_ = rollbackErr
		}
	}()

	querier := sqlc.New(tx)

	// Step 1: Validate registry exists and get registry ID
	registry, err := querier.GetRegistryByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("registry not found: %s", registryName)
		}
		return fmt.Errorf("failed to get registry: %w", err)
	}

	// Step 2: Upsert all servers using temp table and COPY, collect their IDs
	serverIDMap, err := d.storeSyncInTempTables(ctx, tx, registry.ID, reg.Data.Servers)
	if err != nil {
		return fmt.Errorf("failed to upsert servers: %w", err)
	}

	// Step 3: Delete orphaned servers (servers that no longer exist in upstream)
	if err := d.deleteOrphanedServers(ctx, tx, registry.ID, serverIDMap); err != nil {
		return fmt.Errorf("failed to delete orphaned servers: %w", err)
	}

	// Step 4: Insert related data (packages, remotes, icons) using temp tables
	if err := d.insertRelatedData(ctx, tx, serverIDMap, reg.Data.Servers); err != nil {
		return fmt.Errorf("failed to insert related data: %w", err)
	}

	// Step 5: Update latest_server_version table
	if err := d.updateLatestVersions(ctx, querier, registry.ID, serverIDMap, reg.Data.Servers); err != nil {
		return fmt.Errorf("failed to update latest versions: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// serverKey creates a unique key for a server based on name and version
func serverKey(name, version string) string {
	return name + "@" + version
}

// storeSyncInTempTables upserts all servers using temp table and COPY for maximum performance.
// Uses ON CONFLICT UPDATE to preserve existing server UUIDs.
// Returns a map of serverKey (name@version) to server UUID for subsequent operations.
func (*dbSyncWriter) storeSyncInTempTables(
	ctx context.Context,
	tx pgx.Tx,
	registryID uuid.UUID,
	servers []upstreamv0.ServerJSON,
) (map[string]uuid.UUID, error) {
	if len(servers) == 0 {
		return make(map[string]uuid.UUID), nil
	}

	// 1. Create temp table
	querier := sqlc.New(tx)
	if err := querier.CreateTempServerTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to create temp server table: %w", err)
	}

	// 2. Prepare rows for COPY
	now := time.Now()
	rows := make([][]any, 0, len(servers))

	for _, server := range servers {
		// Prepare repository fields
		var repoURL, repoID, repoSubfolder, repoType *string
		if server.Repository != nil {
			repoURL = &server.Repository.URL
			repoID = &server.Repository.ID
			repoSubfolder = &server.Repository.Subfolder
			repoType = &server.Repository.Source
		}

		// Serialize metadata
		serverMeta, err := serializeServerMeta(server.Meta)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize metadata for server %s: %w", server.Name, err)
		}

		rows = append(rows, []any{
			server.Name,
			server.Version,
			registryID,
			&now,
			&now,
			nilIfEmpty(server.Description),
			nilIfEmpty(server.Title),
			nilIfEmpty(server.WebsiteURL),
			nil, // upstream_meta
			serverMeta,
			repoURL,
			repoID,
			repoSubfolder,
			repoType,
		})
	}

	// 3. COPY into temp table
	copyCount, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"temp_mcp_server"},
		[]string{"name", "version", "reg_id", "created_at", "updated_at",
			"description", "title", "website", "upstream_meta", "server_meta",
			"repository_url", "repository_id", "repository_subfolder", "repository_type"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to copy servers to temp table: %w", err)
	}
	if int(copyCount) != len(servers) {
		return nil, fmt.Errorf("copy count mismatch: expected %d, got %d", len(servers), copyCount)
	}

	// 4. Bulk upsert from temp table to permanent table
	if err := querier.UpsertServersFromTemp(ctx); err != nil {
		return nil, fmt.Errorf("failed to upsert servers from temp table: %w", err)
	}

	// 5. Build a set of expected server keys from input
	expectedKeys := make(map[string]bool, len(servers))
	for _, server := range servers {
		expectedKeys[serverKey(server.Name, server.Version)] = true
	}

	// 6. Query back server IDs from permanent table
	dbServers, err := querier.GetServerIDsByRegistryNameVersion(ctx, registryID)
	if err != nil {
		return nil, fmt.Errorf("failed to query server IDs: %w", err)
	}

	// Build map - only include servers that were in the input (not pre-existing orphans)
	serverIDMap := make(map[string]uuid.UUID, len(servers))
	for _, dbServer := range dbServers {
		key := serverKey(dbServer.Name, dbServer.Version)
		if expectedKeys[key] {
			serverIDMap[key] = dbServer.ID
		}
	}

	return serverIDMap, nil
}

// deleteOrphanedServers removes servers for a registry that are not in the keepIDs set.
// Due to CASCADE constraints, this also removes related packages, remotes, icons, and latest_server_version entries.
func (*dbSyncWriter) deleteOrphanedServers(
	ctx context.Context,
	tx pgx.Tx,
	registryID uuid.UUID,
	serverIDMap map[string]uuid.UUID,
) error {
	querier := sqlc.New(tx)

	// Extract all server IDs from the map
	keepIDs := make([]uuid.UUID, 0, len(serverIDMap))
	for _, serverID := range serverIDMap {
		keepIDs = append(keepIDs, serverID)
	}

	// If no servers to keep, delete all servers for this registry
	if len(keepIDs) == 0 {
		return querier.DeleteServersByRegistry(ctx, registryID)
	}

	// Delete servers not in the keepIDs list
	return querier.DeleteOrphanedServers(ctx, sqlc.DeleteOrphanedServersParams{
		RegID:   registryID,
		KeepIds: keepIDs,
	})
}

// insertRelatedData inserts packages, remotes, and icons using temp tables and bulk operations.
// For each type of related data, it: creates temp table, copies data, upserts from temp, deletes orphans.
func (*dbSyncWriter) insertRelatedData(
	ctx context.Context,
	tx pgx.Tx,
	serverIDMap map[string]uuid.UUID,
	servers []upstreamv0.ServerJSON,
) error {
	// Bulk insert packages
	if err := bulkInsertPackages(ctx, tx, serverIDMap, servers); err != nil {
		return fmt.Errorf("failed to bulk insert packages: %w", err)
	}

	// Bulk insert remotes
	if err := bulkInsertRemotes(ctx, tx, serverIDMap, servers); err != nil {
		return fmt.Errorf("failed to bulk insert remotes: %w", err)
	}

	// Bulk insert icons
	if err := bulkInsertIcons(ctx, tx, serverIDMap, servers); err != nil {
		return fmt.Errorf("failed to bulk insert icons: %w", err)
	}

	return nil
}

// updateLatestVersions determines and inserts the latest version for each unique server name.
func (*dbSyncWriter) updateLatestVersions(
	ctx context.Context,
	querier *sqlc.Queries,
	registryID uuid.UUID,
	serverIDMap map[string]uuid.UUID,
	servers []upstreamv0.ServerJSON,
) error {
	// Group servers by name and find the latest version for each
	latestVersions := make(map[string]struct {
		version  string
		serverID uuid.UUID
	})

	for _, server := range servers {
		serverID := serverIDMap[serverKey(server.Name, server.Version)]

		existing, exists := latestVersions[server.Name]
		if !exists {
			latestVersions[server.Name] = struct {
				version  string
				serverID uuid.UUID
			}{
				version:  server.Version,
				serverID: serverID,
			}
			continue
		}

		// Compare versions using semver
		if isNewerVersion(server.Version, existing.version) {
			latestVersions[server.Name] = struct {
				version  string
				serverID uuid.UUID
			}{
				version:  server.Version,
				serverID: serverID,
			}
		}
	}

	// Insert latest version pointers
	for name, latest := range latestVersions {
		_, err := querier.UpsertLatestServerVersion(ctx, sqlc.UpsertLatestServerVersionParams{
			RegID:    registryID,
			Name:     name,
			Version:  latest.version,
			ServerID: latest.serverID,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert latest version for server %s: %w", name, err)
		}
	}

	return nil
}

// isNewerVersion compares two version strings and returns true if newVersion is greater than oldVersion.
// Falls back to string comparison if semantic versioning parsing fails.
func isNewerVersion(newVersion, oldVersion string) bool {
	newSemver, errNew := semver.NewVersion(newVersion)
	oldSemver, errOld := semver.NewVersion(oldVersion)

	if errNew != nil || errOld != nil {
		// Fallback to string comparison if semver parsing fails
		return newVersion > oldVersion
	}

	return newSemver.GreaterThan(oldSemver)
}

// bulkInsertPackages handles bulk upsert of packages using temp table and COPY.
func bulkInsertPackages(
	ctx context.Context,
	tx pgx.Tx,
	serverIDMap map[string]uuid.UUID,
	servers []upstreamv0.ServerJSON,
) error {
	// Collect all packages with their server IDs
	var packageRows [][]any
	serverIDs := make(map[uuid.UUID]bool)

	for _, server := range servers {
		serverID, ok := serverIDMap[serverKey(server.Name, server.Version)]
		if !ok {
			return fmt.Errorf("server ID not found for %s@%s", server.Name, server.Version)
		}
		serverIDs[serverID] = true

		for _, pkg := range server.Packages {
			packageRows = append(packageRows, []any{
				serverID,
				pkg.RegistryType,
				pkg.RegistryBaseURL,
				pkg.Identifier,
				pkg.Version,
				nilIfEmpty(pkg.RunTimeHint),
				extractArgumentValues(pkg.RuntimeArguments),
				extractArgumentValues(pkg.PackageArguments),
				serializeKeyValueInputs(pkg.EnvironmentVariables),
				nilIfEmpty(pkg.FileSHA256),
				pkg.Transport.Type,
				nilIfEmpty(pkg.Transport.URL),
				serializeKeyValueInputs(pkg.Transport.Headers),
			})
		}
	}

	if len(packageRows) == 0 {
		// No packages to insert, but still need to delete orphans
		return deleteOrphansWithEmptyTemp(ctx, tx, serverIDs, "package")
	}

	// Create temp table
	querier := sqlc.New(tx)
	if err := querier.CreateTempPackageTable(ctx); err != nil {
		return fmt.Errorf("failed to create temp package table: %w", err)
	}

	// COPY into temp table
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"temp_mcp_server_package"},
		[]string{"server_id", "registry_type", "pkg_registry_url", "pkg_identifier", "pkg_version",
			"runtime_hint", "runtime_arguments", "package_arguments", "env_vars", "sha256_hash",
			"transport", "transport_url", "transport_headers"},
		pgx.CopyFromRows(packageRows))
	if err != nil {
		return fmt.Errorf("failed to copy packages: %w", err)
	}

	// Upsert from temp
	if err := querier.UpsertPackagesFromTemp(ctx); err != nil {
		return fmt.Errorf("failed to upsert packages: %w", err)
	}

	// Delete orphans
	serverIDList := make([]uuid.UUID, 0, len(serverIDs))
	for id := range serverIDs {
		serverIDList = append(serverIDList, id)
	}
	if err := querier.DeleteOrphanedPackages(ctx, serverIDList); err != nil {
		return fmt.Errorf("failed to delete orphaned packages: %w", err)
	}

	return nil
}

// bulkInsertRemotes handles bulk upsert of remotes using temp table and COPY.
func bulkInsertRemotes(
	ctx context.Context,
	tx pgx.Tx,
	serverIDMap map[string]uuid.UUID,
	servers []upstreamv0.ServerJSON,
) error {
	// Collect all remotes with their server IDs
	var remoteRows [][]any
	serverIDs := make(map[uuid.UUID]bool)

	// Track seen remotes to deduplicate by (server_id, transport, transport_url)
	type remoteKey struct {
		serverID  uuid.UUID
		transport string
		url       string
	}
	seenRemotes := make(map[remoteKey]bool)
	skippedCount := 0

	for _, server := range servers {
		serverID, ok := serverIDMap[serverKey(server.Name, server.Version)]
		if !ok {
			return fmt.Errorf("server ID not found for %s@%s", server.Name, server.Version)
		}
		serverIDs[serverID] = true

		for _, remote := range server.Remotes {
			// Check for duplicate remote
			key := remoteKey{
				serverID:  serverID,
				transport: remote.Type,
				url:       remote.URL,
			}

			if seenRemotes[key] {
				// Skip duplicate - already processed this remote
				skippedCount++
				continue
			}
			seenRemotes[key] = true

			remoteRows = append(remoteRows, []any{
				serverID,
				remote.Type,
				remote.URL,
				serializeKeyValueInputs(remote.Headers),
			})
		}
	}

	// Log if duplicates were found
	if skippedCount > 0 {
		// TODO: Use structured logging when available
		_ = skippedCount // Placeholder
	}

	if len(remoteRows) == 0 {
		// No remotes to insert, but still need to delete orphans
		return deleteOrphansWithEmptyTemp(ctx, tx, serverIDs, "remote")
	}

	// Create temp table
	querier := sqlc.New(tx)
	if err := querier.CreateTempRemoteTable(ctx); err != nil {
		return fmt.Errorf("failed to create temp remote table: %w", err)
	}

	// COPY into temp table
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"temp_mcp_server_remote"},
		[]string{"server_id", "transport", "transport_url", "transport_headers"},
		pgx.CopyFromRows(remoteRows))
	if err != nil {
		return fmt.Errorf("failed to copy remotes: %w", err)
	}

	// Upsert from temp
	if err := querier.UpsertRemotesFromTemp(ctx); err != nil {
		return fmt.Errorf("failed to upsert remotes: %w", err)
	}

	// Delete orphans
	serverIDList := make([]uuid.UUID, 0, len(serverIDs))
	for id := range serverIDs {
		serverIDList = append(serverIDList, id)
	}
	if err := querier.DeleteOrphanedRemotes(ctx, serverIDList); err != nil {
		return fmt.Errorf("failed to delete orphaned remotes: %w", err)
	}

	return nil
}

// bulkInsertIcons handles bulk upsert of icons using temp table and COPY.
func bulkInsertIcons(
	ctx context.Context,
	tx pgx.Tx,
	serverIDMap map[string]uuid.UUID,
	servers []upstreamv0.ServerJSON,
) error {
	// Collect all icons with their server IDs
	var iconRows [][]any
	serverIDs := make(map[uuid.UUID]bool)

	for _, server := range servers {
		serverID, ok := serverIDMap[serverKey(server.Name, server.Version)]
		if !ok {
			return fmt.Errorf("server ID not found for %s@%s", server.Name, server.Version)
		}
		serverIDs[serverID] = true

		for _, icon := range server.Icons {
			// Convert theme string pointer to database enum value
			// Input values are lowercase ("light", "dark") but DB enum is uppercase ("LIGHT", "DARK")
			theme := iconThemeLight // Default to light
			if icon.Theme != nil {
				switch *icon.Theme {
				case "light":
					theme = iconThemeLight
				case "dark":
					theme = iconThemeDark
				default:
					theme = iconThemeLight // Default to light if unknown
				}
			}

			// Get MIME type, default to empty string if not provided
			mimeType := ""
			if icon.MimeType != nil {
				mimeType = *icon.MimeType
			}

			iconRows = append(iconRows, []any{
				serverID,
				icon.Src,
				mimeType,
				theme,
			})
		}
	}

	if len(iconRows) == 0 {
		// No icons to insert, but still need to delete orphans
		return deleteOrphansWithEmptyTemp(ctx, tx, serverIDs, "icon")
	}

	// Create temp table
	querier := sqlc.New(tx)
	if err := querier.CreateTempIconTable(ctx); err != nil {
		return fmt.Errorf("failed to create temp icon table: %w", err)
	}

	// COPY into temp table
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"temp_mcp_server_icon"},
		[]string{"server_id", "source_uri", "mime_type", "theme"},
		pgx.CopyFromRows(iconRows))
	if err != nil {
		return fmt.Errorf("failed to copy icons: %w", err)
	}

	// Upsert from temp
	if err := querier.UpsertIconsFromTemp(ctx); err != nil {
		return fmt.Errorf("failed to upsert icons: %w", err)
	}

	// Delete orphans
	serverIDList := make([]uuid.UUID, 0, len(serverIDs))
	for id := range serverIDs {
		serverIDList = append(serverIDList, id)
	}
	if err := querier.DeleteOrphanedIcons(ctx, serverIDList); err != nil {
		return fmt.Errorf("failed to delete orphaned icons: %w", err)
	}

	return nil
}

// deleteOrphansWithEmptyTemp handles the case when there are no rows to insert but need to delete orphans.
// This is called when a server previously had packages/remotes/icons but now has none.
func deleteOrphansWithEmptyTemp(ctx context.Context, tx pgx.Tx, serverIDs map[uuid.UUID]bool, dataType string) error {
	if len(serverIDs) == 0 {
		return nil
	}

	querier := sqlc.New(tx)
	for serverID := range serverIDs {
		var err error
		switch dataType {
		case "package":
			err = querier.DeleteServerPackagesByServerId(ctx, serverID)
		case "remote":
			err = querier.DeleteServerRemotesByServerId(ctx, serverID)
		case "icon":
			err = querier.DeleteServerIconsByServerId(ctx, serverID)
		default:
			return fmt.Errorf("unknown data type: %s", dataType)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// serializeServerMeta serializes the server metadata to JSON bytes for storage
func serializeServerMeta(meta *upstreamv0.ServerMeta) ([]byte, error) {
	if meta == nil || meta.PublisherProvided == nil || len(meta.PublisherProvided) == 0 {
		return nil, nil
	}

	bytes, err := json.Marshal(meta.PublisherProvided)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize server metadata: %w", err)
	}

	return bytes, nil
}

// extractArgumentValues extracts argument names from a slice of model.Argument
func extractArgumentValues(arguments []model.Argument) []string {
	result := make([]string, len(arguments))
	for i, arg := range arguments {
		result[i] = arg.Name
	}
	return result
}

// serializeKeyValueInputs serializes KeyValueInput slice to JSON bytes for database storage
// This preserves all metadata (default, description, isSecret, etc.) not just the name
func serializeKeyValueInputs(kvInputs []model.KeyValueInput) []byte {
	if len(kvInputs) == 0 {
		return []byte("[]")
	}

	bytes, err := json.Marshal(kvInputs)
	if err != nil {
		// Return empty array on error - this shouldn't happen with valid KeyValueInput
		return []byte("[]")
	}

	return bytes
}

// nilIfEmpty returns nil if the string is empty, otherwise returns a pointer to the string
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

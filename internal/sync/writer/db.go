// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/validators"
	"github.com/stacklok/toolhive-registry-server/internal/versions"
)

// Theme constants for icons (must match PostgreSQL icon_theme enum values)
const (
	iconThemeLight = "LIGHT"
	iconThemeDark  = "DARK"
)

// dbSyncWriter is a SyncWriter implementation that persists data to a database
type dbSyncWriter struct {
	pool        *pgxpool.Pool
	maxMetaSize int
}

// NewDBSyncWriter creates a new dbSyncWriter with the given connection pool.
// The caller is responsible for closing the pool when done.
// maxMetaSize specifies the maximum allowed size in bytes for publisher-provided
// metadata extensions and must be greater than zero.
func NewDBSyncWriter(pool *pgxpool.Pool, maxMetaSize int) (SyncWriter, error) {
	if pool == nil {
		return nil, fmt.Errorf("pgx pool is required")
	}
	return &dbSyncWriter{pool: pool, maxMetaSize: maxMetaSize}, nil
}

// Store saves a UpstreamRegistry instance to database storage for a specific registry.
//
// This method performs an efficient bulk sync using temporary tables and COPY operations:
//  1. Validates the registry exists
//  2. Upserts registry entries (one per name), entry versions (one per name+version),
//     and mcp_server rows via temp tables and COPY to preserve existing UUIDs
//  3. Deletes orphaned servers that no longer exist in upstream (CASCADE cleans related data)
//  4. For packages/remotes/icons: creates temp tables, copies data, bulk upserts, deletes orphans
//  5. Updates the latest_entry_version table for each unique server name
//
// The operation is performed within a serializable transaction to ensure consistency.
// Temp tables are automatically dropped at transaction end (ON COMMIT DROP).
func (d *dbSyncWriter) Store(
	ctx context.Context,
	registryName string,
	reg *toolhivetypes.UpstreamRegistry,
	opts ...StoreOption,
) error {
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
	registry, err := querier.GetSourceByName(ctx, registryName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("registry not found: %s", registryName)
		}
		return fmt.Errorf("failed to get registry: %w", err)
	}

	// Parse store options
	storeOpts := &StoreOptions{}
	for _, opt := range opts {
		opt(storeOpts)
	}

	// Step 2: Upsert all servers using temp table and COPY, collect their IDs
	serverIDMap, err := d.storeSyncInTempTables(ctx, tx, registry.ID, reg.Data.Servers, registry.Claims, storeOpts.PerEntryClaims)
	if err != nil {
		return fmt.Errorf("failed to upsert servers: %w", err)
	}

	// Drop server temp tables so they can be reused for skills
	if err := dropEntryTempTables(ctx, querier); err != nil {
		return err
	}

	// Step 3: Delete orphaned servers (servers that no longer exist in upstream)
	if err := d.deleteOrphanedEntries(ctx, tx, registry.ID, sqlc.EntryTypeMCP, collectValues(serverIDMap)); err != nil {
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

	// Step 6: Store skills
	if err := d.storeSkills(ctx, tx, registry.ID, reg.Data.Skills, registry.Claims); err != nil {
		return fmt.Errorf("failed to store skills: %w", err)
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
// Uses ON CONFLICT UPDATE to preserve existing UUIDs.
// Returns a map of serverKey (name@version) to entry_version UUID for subsequent operations.
func (d *dbSyncWriter) storeSyncInTempTables(
	ctx context.Context,
	tx pgx.Tx,
	registryID uuid.UUID,
	servers []upstreamv0.ServerJSON,
	claims []byte,
	perEntryClaims map[string][]byte,
) (map[string]uuid.UUID, error) {
	if len(servers) == 0 {
		return make(map[string]uuid.UUID), nil
	}

	querier := sqlc.New(tx)

	// 1. Upsert registry entries (one per unique name)
	entryMap, err := sqlCopyEntries(ctx, tx, registryID, servers, claims, perEntryClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to copy entries: %w", err)
	}

	// 2. Upsert entry versions (one per name+version)
	versionMap, err := sqlCopyEntryVersions(ctx, tx, entryMap, servers)
	if err != nil {
		return nil, fmt.Errorf("failed to copy entry versions: %w", err)
	}

	// 3. Copy mcp_server rows using entry_version IDs
	if err := sqlCopyServers(ctx, tx, servers, versionMap, d.maxMetaSize); err != nil {
		return nil, fmt.Errorf("failed to copy servers: %w", err)
	}

	// 4. Build a set of expected server keys from input
	expectedKeys := make(map[string]bool, len(servers))
	for _, server := range servers {
		expectedKeys[serverKey(server.Name, server.Version)] = true
	}

	// 5. Query back server IDs from permanent table
	dbServers, err := querier.GetServerIDsByRegistryNameVersion(ctx, registryID)
	if err != nil {
		return nil, fmt.Errorf("failed to query server IDs: %w", err)
	}

	// Build map - only include servers that were in the input (not pre-existing orphans)
	serverIDMap := make(map[string]uuid.UUID, len(servers))
	for _, dbServer := range dbServers {
		key := serverKey(dbServer.Name, dbServer.Version)
		if expectedKeys[key] {
			serverIDMap[key] = dbServer.VersionID
		}
	}

	return serverIDMap, nil
}

// deleteOrphanedEntries removes entry versions for a registry that are not in the keepIDs set.
// Due to CASCADE constraints, this also removes related packages, remotes, icons, and latest_entry_version entries.
func (*dbSyncWriter) deleteOrphanedEntries(
	ctx context.Context,
	tx pgx.Tx,
	registryID uuid.UUID,
	entryType sqlc.EntryType,
	keepIDs []uuid.UUID,
) error {
	querier := sqlc.New(tx)

	// If no entries to keep, delete all entries for this registry by type
	if len(keepIDs) == 0 {
		switch entryType {
		case sqlc.EntryTypeMCP:
			return querier.DeleteServersByRegistry(ctx, registryID)
		case sqlc.EntryTypeSKILL:
			return querier.DeleteSkillsByRegistry(ctx, registryID)
		default:
			return fmt.Errorf("unknown entry type: %s", entryType)
		}
	}

	// Delete entry versions not in the keepIDs list
	return querier.DeleteOrphanedEntryVersions(ctx, sqlc.DeleteOrphanedEntryVersionsParams{
		SourceID:  registryID,
		EntryType: entryType,
		KeepIds:   keepIDs,
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
		if versions.IsNewerVersion(server.Version, existing.version) {
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
			SourceID:  registryID,
			Name:      name,
			Version:   latest.version,
			VersionID: latest.serverID,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert latest version for server %s: %w", name, err)
		}
	}

	return nil
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

// serializeServerMeta serializes the server metadata to JSON bytes for storage.
// maxMetaSize specifies the maximum allowed size in bytes and must be greater than zero.
func serializeServerMeta(meta *upstreamv0.ServerMeta, maxMetaSize int) ([]byte, error) {
	return validators.SerializeServerMeta(meta, maxMetaSize)
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

// dropEntryTempTables drops the shared temp tables so they can be recreated for a different entry type.
func dropEntryTempTables(ctx context.Context, querier *sqlc.Queries) error {
	if err := querier.DropTempRegistryEntryTable(ctx); err != nil {
		return fmt.Errorf("failed to drop temp registry entry table: %w", err)
	}
	if err := querier.DropTempEntryVersionTable(ctx); err != nil {
		return fmt.Errorf("failed to drop temp entry version table: %w", err)
	}
	return nil
}

// collectValues extracts the values from a map into a slice.
func collectValues(m map[string]uuid.UUID) []uuid.UUID {
	vals := make([]uuid.UUID, 0, len(m))
	for _, v := range m {
		vals = append(vals, v)
	}
	return vals
}

// copyAndUpsertEntries creates a temp registry entry table, copies the pre-built rows into it,
// and upserts them into the permanent table. Returns a map of entry name to registry_entry UUID.
func copyAndUpsertEntries(ctx context.Context, tx pgx.Tx, entryRows [][]any) (map[string]uuid.UUID, error) {
	querier := sqlc.New(tx)

	if err := querier.CreateTempRegistryEntryTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to create temp registry entry table: %w", err)
	}

	entryCopyCount, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"temp_registry_entry"},
		[]string{"id", "source_id", "entry_type", "name", "claims", "created_at", "updated_at"},
		pgx.CopyFromRows(entryRows),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to copy entries to temp table: %w", err)
	}
	if int(entryCopyCount) != len(entryRows) {
		return nil, fmt.Errorf("copy count mismatch: expected %d, got %d", len(entryRows), entryCopyCount)
	}

	copiedRows, err := querier.UpsertRegistryEntriesFromTemp(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert registry entries from temp table: %w", err)
	}

	entryMap := make(map[string]uuid.UUID, len(copiedRows))
	for _, row := range copiedRows {
		entryMap[row.Name] = row.ID
	}

	return entryMap, nil
}

// sqlCopyEntries upserts registry entries for servers (one per unique name) using temp table and COPY.
// Returns a map of server name to registry_entry UUID.
func sqlCopyEntries(
	ctx context.Context,
	tx pgx.Tx,
	registryID uuid.UUID,
	servers []upstreamv0.ServerJSON,
	claims []byte,
	perEntryClaims map[string][]byte,
) (map[string]uuid.UUID, error) {
	// Deduplicate by name — one registry_entry per unique server name
	seen := make(map[string]bool, len(servers))
	var entryRows [][]any
	for _, server := range servers {
		if seen[server.Name] {
			continue
		}
		seen[server.Name] = true

		now := time.Now()
		entryID, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("failed to generate entry ID: %w", err)
		}

		// Use per-entry claims if available, otherwise fall back to source-level claims
		entryClaims := claims
		if ec, ok := perEntryClaims[server.Name]; ok {
			entryClaims = ec
		}

		entryRows = append(entryRows, []any{
			entryID,
			registryID,
			sqlc.EntryTypeMCP,
			server.Name,
			entryClaims,
			&now,
			&now,
		})
	}

	return copyAndUpsertEntries(ctx, tx, entryRows)
}

// copyAndUpsertEntryVersions creates a temp entry version table, copies the pre-built rows into it,
// and upserts them into the permanent table. Returns the upserted rows for caller-specific key mapping.
func copyAndUpsertEntryVersions(
	ctx context.Context, tx pgx.Tx, versionRows [][]any,
) ([]sqlc.UpsertEntryVersionsFromTempRow, error) {
	querier := sqlc.New(tx)

	if err := querier.CreateTempEntryVersionTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to create temp entry version table: %w", err)
	}

	copyCount, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"temp_entry_version"},
		[]string{"id", "entry_id", "name", "version", "title", "description", "created_at", "updated_at"},
		pgx.CopyFromRows(versionRows),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to copy entry versions to temp table: %w", err)
	}
	if int(copyCount) != len(versionRows) {
		return nil, fmt.Errorf("copy count mismatch: expected %d, got %d", len(versionRows), copyCount)
	}

	copiedRows, err := querier.UpsertEntryVersionsFromTemp(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert entry versions from temp table: %w", err)
	}

	return copiedRows, nil
}

// sqlCopyEntryVersions upserts entry versions for servers (one per name+version) using temp table and COPY.
// Returns a map of serverKey (name@version) to entry_version UUID.
func sqlCopyEntryVersions(
	ctx context.Context,
	tx pgx.Tx,
	entryMap map[string]uuid.UUID,
	servers []upstreamv0.ServerJSON,
) (map[string]uuid.UUID, error) {
	versionRows := make([][]any, 0, len(servers))
	for _, server := range servers {
		entryID, ok := entryMap[server.Name]
		if !ok {
			return nil, fmt.Errorf("entry ID not found for %s", server.Name)
		}

		now := time.Now()
		versionID, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("failed to generate version ID: %w", err)
		}

		versionRows = append(versionRows, []any{
			versionID,
			entryID,
			server.Name,
			server.Version,
			nilIfEmpty(server.Title),
			nilIfEmpty(server.Description),
			&now,
			&now,
		})
	}

	copiedRows, err := copyAndUpsertEntryVersions(ctx, tx, versionRows)
	if err != nil {
		return nil, err
	}

	// Build reverse lookup: entry_id → name
	entryIDToName := make(map[uuid.UUID]string, len(entryMap))
	for name, id := range entryMap {
		entryIDToName[id] = name
	}

	versionMap := make(map[string]uuid.UUID, len(copiedRows))
	for _, row := range copiedRows {
		name := entryIDToName[row.EntryID]
		versionMap[serverKey(name, row.Version)] = row.ID
	}

	return versionMap, nil
}

func sqlCopyServers(
	ctx context.Context,
	tx pgx.Tx,
	servers []upstreamv0.ServerJSON,
	entryIDMap map[string]uuid.UUID,
	maxMetaSize int,
) error {
	querier := sqlc.New(tx)

	if err := querier.CreateTempServerTable(ctx); err != nil {
		return fmt.Errorf("failed to create temp server table: %w", err)
	}

	mcpRows := make([][]any, 0, len(servers))
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
		serverMeta, err := serializeServerMeta(server.Meta, maxMetaSize)
		if err != nil {
			return fmt.Errorf("failed to serialize metadata for server %s: %w", server.Name, err)
		}

		mcpRows = append(mcpRows, []any{
			entryIDMap[serverKey(server.Name, server.Version)],
			nilIfEmpty(server.WebsiteURL),
			nil, // upstream_meta
			serverMeta,
			repoURL,
			repoID,
			repoSubfolder,
			repoType,
		})
	}

	copyCount, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"temp_mcp_server"},
		[]string{"version_id", "website", "upstream_meta", "server_meta",
			"repository_url", "repository_id", "repository_subfolder", "repository_type"},
		pgx.CopyFromRows(mcpRows),
	)
	if err != nil {
		return fmt.Errorf("failed to copy servers to temp table: %w", err)
	}
	if int(copyCount) != len(servers) {
		return fmt.Errorf("copy count mismatch: expected %d, got %d", len(servers), copyCount)
	}

	// 4. Bulk upsert from temp table to permanent table
	if err := querier.UpsertServersFromTemp(ctx); err != nil {
		return fmt.Errorf("failed to upsert servers from temp table: %w", err)
	}

	return nil
}

// skillKey creates a unique key for a skill based on namespace, name and version
func skillKey(namespace, name, version string) string {
	return namespace + "/" + name + "@" + version
}

// storeSkills persists skills from an upstream registry into the database.
// It follows the same bulk-sync pattern as server storage: temp tables with COPY,
// followed by upserts and orphan cleanup. Reuses the shared copyAndUpsertEntries
// and copyAndUpsertEntryVersions functions.
func (d *dbSyncWriter) storeSkills(
	ctx context.Context,
	tx pgx.Tx,
	registryID uuid.UUID,
	skills []toolhivetypes.Skill,
	claims []byte,
) error {
	querier := sqlc.New(tx)

	// If no skills, clean up any previously synced skills and return
	if len(skills) == 0 {
		return querier.DeleteSkillsByRegistry(ctx, registryID)
	}

	// 1. Upsert registry entries for skills (one per unique name)
	// Build entry rows from skills, then use the shared copyAndUpsertEntries
	seen := make(map[string]bool, len(skills))
	var entryRows [][]any
	for _, skill := range skills {
		if seen[skill.Name] {
			continue
		}
		seen[skill.Name] = true

		now := time.Now()
		entryID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("failed to generate entry ID: %w", err)
		}

		entryRows = append(entryRows, []any{
			entryID,
			registryID,
			sqlc.EntryTypeSKILL,
			skill.Name,
			claims,
			&now,
			&now,
		})
	}

	entryMap, err := copyAndUpsertEntries(ctx, tx, entryRows)
	if err != nil {
		return fmt.Errorf("failed to copy skill entries: %w", err)
	}

	// 2. Upsert entry versions for skills (one per name+version)
	// Build version rows from skills, then use the shared copyAndUpsertEntryVersions
	versionRows := make([][]any, 0, len(skills))
	for _, skill := range skills {
		entryID, ok := entryMap[skill.Name]
		if !ok {
			return fmt.Errorf("entry ID not found for skill %s", skill.Name)
		}

		now := time.Now()
		versionID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("failed to generate version ID: %w", err)
		}

		versionRows = append(versionRows, []any{
			versionID,
			entryID,
			skill.Name,
			skill.Version,
			nilIfEmpty(skill.Title),
			nilIfEmpty(skill.Description),
			&now,
			&now,
		})
	}

	copiedRows, err := copyAndUpsertEntryVersions(ctx, tx, versionRows)
	if err != nil {
		return fmt.Errorf("failed to copy skill entry versions: %w", err)
	}

	// Build reverse lookup: entry_id → name
	entryIDToName := make(map[uuid.UUID]string, len(entryMap))
	for name, id := range entryMap {
		entryIDToName[id] = name
	}

	// Build a lookup from name+version to namespace
	type nameVersion struct {
		name    string
		version string
	}
	nvToNamespace := make(map[nameVersion]string, len(skills))
	for _, skill := range skills {
		nvToNamespace[nameVersion{name: skill.Name, version: skill.Version}] = skill.Namespace
	}

	// Build version map keyed by namespace/name@version
	versionMap := make(map[string]uuid.UUID, len(copiedRows))
	for _, row := range copiedRows {
		name := entryIDToName[row.EntryID]
		ns := nvToNamespace[nameVersion{name: name, version: row.Version}]
		versionMap[skillKey(ns, name, row.Version)] = row.ID
	}

	// 3. Upsert skill-specific data and packages for each skill
	keepIDs, err := upsertSkillVersionsAndPackages(ctx, querier, skills, versionMap)
	if err != nil {
		return err
	}

	// 4. Delete orphaned skills that no longer exist in upstream
	if err := d.deleteOrphanedEntries(ctx, tx, registryID, sqlc.EntryTypeSKILL, keepIDs); err != nil {
		return fmt.Errorf("failed to delete orphaned skills: %w", err)
	}

	// 5. Update latest skill versions
	return updateLatestSkillVersions(ctx, querier, registryID, skills, versionMap)
}

// upsertSkillVersionsAndPackages upserts each skill's version data and replaces its packages.
// Returns the list of version IDs to keep (for orphan cleanup).
func upsertSkillVersionsAndPackages(
	ctx context.Context,
	querier *sqlc.Queries,
	skills []toolhivetypes.Skill,
	versionMap map[string]uuid.UUID,
) ([]uuid.UUID, error) {
	keepIDs := make([]uuid.UUID, 0, len(skills))

	for _, skill := range skills {
		key := skillKey(skill.Namespace, skill.Name, skill.Version)
		versionID, ok := versionMap[key]
		if !ok {
			return nil, fmt.Errorf("version ID not found for skill %s", key)
		}

		skillVersionID, err := upsertSingleSkillVersion(ctx, querier, skill, versionID)
		if err != nil {
			return nil, err
		}

		keepIDs = append(keepIDs, skillVersionID)

		if err := replaceSkillPackages(ctx, querier, skill, skillVersionID); err != nil {
			return nil, err
		}
	}

	return keepIDs, nil
}

// upsertSingleSkillVersion upserts the skill row for a single skill version.
func upsertSingleSkillVersion(
	ctx context.Context,
	querier *sqlc.Queries,
	skill toolhivetypes.Skill,
	versionID uuid.UUID,
) (uuid.UUID, error) {
	key := skillKey(skill.Namespace, skill.Name, skill.Version)

	repoJSON, err := marshalJSONOrNil(skill.Repository)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to marshal repository for skill %s: %w", key, err)
	}
	iconsJSON, err := marshalJSONOrNil(skill.Icons)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to marshal icons for skill %s: %w", key, err)
	}
	metadataJSON, err := marshalJSONOrNil(skill.Metadata)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to marshal metadata for skill %s: %w", key, err)
	}
	extMetaJSON, err := marshalJSONOrNil(skill.Meta)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to marshal extension meta for skill %s: %w", key, err)
	}

	status := sqlc.NullSkillStatus{}
	if skill.Status != "" {
		status = sqlc.NullSkillStatus{
			SkillStatus: sqlc.SkillStatus(strings.ToUpper(skill.Status)),
			Valid:       true,
		}
	}

	skillVersionID, err := querier.UpsertSkillVersionForSync(ctx, sqlc.UpsertSkillVersionForSyncParams{
		VersionID:     versionID,
		Namespace:     skill.Namespace,
		Status:        status,
		License:       nilIfEmpty(skill.License),
		Compatibility: nilIfEmpty(skill.Compatibility),
		AllowedTools:  skill.AllowedTools,
		Repository:    repoJSON,
		Icons:         iconsJSON,
		Metadata:      metadataJSON,
		ExtensionMeta: extMetaJSON,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to upsert skill version for %s: %w", key, err)
	}

	return skillVersionID, nil
}

// replaceSkillPackages deletes existing packages for a skill and inserts new ones.
func replaceSkillPackages(
	ctx context.Context,
	querier *sqlc.Queries,
	skill toolhivetypes.Skill,
	skillVersionID uuid.UUID,
) error {
	key := skillKey(skill.Namespace, skill.Name, skill.Version)

	if err := querier.DeleteSkillOciPackagesBySkillId(ctx, skillVersionID); err != nil {
		return fmt.Errorf("failed to delete OCI packages for skill %s: %w", key, err)
	}
	if err := querier.DeleteSkillGitPackagesBySkillId(ctx, skillVersionID); err != nil {
		return fmt.Errorf("failed to delete Git packages for skill %s: %w", key, err)
	}

	for _, pkg := range skill.Packages {
		switch pkg.RegistryType {
		case "oci":
			if err := querier.InsertSkillOciPackage(ctx, sqlc.InsertSkillOciPackageParams{
				SkillID:    skillVersionID,
				Identifier: pkg.Identifier,
				Digest:     nilIfEmpty(pkg.Digest),
				MediaType:  nilIfEmpty(pkg.MediaType),
			}); err != nil {
				return fmt.Errorf("failed to insert OCI package for skill %s: %w", key, err)
			}
		case "git":
			if err := querier.InsertSkillGitPackage(ctx, sqlc.InsertSkillGitPackageParams{
				SkillID:   skillVersionID,
				Url:       pkg.URL,
				Ref:       nilIfEmpty(pkg.Ref),
				CommitSha: nilIfEmpty(pkg.Commit),
				Subfolder: nilIfEmpty(pkg.Subfolder),
			}); err != nil {
				return fmt.Errorf("failed to insert Git package for skill %s: %w", key, err)
			}
		default:
			slog.Warn("Skipping unknown skill package type",
				"skill", key,
				"registryType", pkg.RegistryType)
		}
	}

	return nil
}

// updateLatestSkillVersions determines and upserts the latest version for each unique skill name.
func updateLatestSkillVersions(
	ctx context.Context,
	querier *sqlc.Queries,
	registryID uuid.UUID,
	skills []toolhivetypes.Skill,
	versionMap map[string]uuid.UUID,
) error {
	latestVersions := make(map[string]struct {
		version   string
		versionID uuid.UUID
	})

	for _, skill := range skills {
		key := skillKey(skill.Namespace, skill.Name, skill.Version)
		versionID := versionMap[key]

		existing, exists := latestVersions[skill.Name]
		if !exists || versions.IsNewerVersion(skill.Version, existing.version) {
			latestVersions[skill.Name] = struct {
				version   string
				versionID uuid.UUID
			}{
				version:   skill.Version,
				versionID: versionID,
			}
		}
	}

	for name, latest := range latestVersions {
		_, err := querier.UpsertLatestSkillVersion(ctx, sqlc.UpsertLatestSkillVersionParams{
			SourceID:  registryID,
			Name:      name,
			Version:   latest.version,
			VersionID: latest.versionID,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert latest version for skill %s: %w", name, err)
		}
	}

	return nil
}

// marshalJSONOrNil marshals the value to JSON, returning nil if the value is nil.
// It handles both untyped nil and typed nil (e.g. (*SomeStruct)(nil) stored in any).
func marshalJSONOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return nil, nil
	}
	return json.Marshal(v)
}

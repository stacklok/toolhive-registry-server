-- Temporary table operations for bulk sync
-- Note: These queries reference temp tables that don't exist in the schema.
-- sqlc cannot validate these, but we organize them here for maintainability.

-- Temp Server Table Operations

-- name: CreateTempServerTable :exec
CREATE TEMP TABLE temp_mcp_server ON COMMIT DROP AS
SELECT * FROM mcp_server
  WITH NO DATA;

-- name: UpsertServersFromTemp :exec
INSERT INTO mcp_server (
    name, version, reg_id, created_at, updated_at,
    description, title, website, upstream_meta, server_meta,
    repository_url, repository_id, repository_subfolder, repository_type
)
SELECT
    name, version, reg_id, created_at, updated_at,
    description, title, website, upstream_meta, server_meta,
    repository_url, repository_id, repository_subfolder, repository_type
FROM temp_mcp_server
ON CONFLICT (reg_id, name, version)
DO UPDATE SET
    updated_at = EXCLUDED.updated_at,
    description = EXCLUDED.description,
    title = EXCLUDED.title,
    website = EXCLUDED.website,
    upstream_meta = EXCLUDED.upstream_meta,
    server_meta = EXCLUDED.server_meta,
    repository_url = EXCLUDED.repository_url,
    repository_id = EXCLUDED.repository_id,
    repository_subfolder = EXCLUDED.repository_subfolder,
    repository_type = EXCLUDED.repository_type;

-- Temp Package Table Operations

-- name: CreateTempPackageTable :exec
CREATE TEMP TABLE temp_mcp_server_package ON COMMIT DROP AS
SELECT * FROM mcp_server_package
  WITH NO DATA;

-- name: UpsertPackagesFromTemp :exec
INSERT INTO mcp_server_package (
    server_id, registry_type, pkg_registry_url, pkg_identifier, pkg_version,
    runtime_hint, runtime_arguments, package_arguments, env_vars, sha256_hash,
    transport, transport_url, transport_headers
)
SELECT
    server_id, registry_type, pkg_registry_url, pkg_identifier, pkg_version,
    runtime_hint, runtime_arguments, package_arguments, env_vars, sha256_hash,
    transport, transport_url, transport_headers
FROM temp_mcp_server_package
ON CONFLICT (server_id, registry_type, pkg_identifier)
DO UPDATE SET
    pkg_registry_url = EXCLUDED.pkg_registry_url,
    pkg_version = EXCLUDED.pkg_version,
    runtime_hint = EXCLUDED.runtime_hint,
    runtime_arguments = EXCLUDED.runtime_arguments,
    package_arguments = EXCLUDED.package_arguments,
    env_vars = EXCLUDED.env_vars,
    sha256_hash = EXCLUDED.sha256_hash,
    transport = EXCLUDED.transport,
    transport_url = EXCLUDED.transport_url,
    transport_headers = EXCLUDED.transport_headers;

-- name: DeleteOrphanedPackages :exec
DELETE FROM mcp_server_package
WHERE server_id = ANY(sqlc.slice(server_ids)::UUID[])
  AND (server_id, registry_type, pkg_identifier) NOT IN (
    SELECT server_id, registry_type, pkg_identifier FROM temp_mcp_server_package
  );

-- Temp Remote Table Operations

-- name: CreateTempRemoteTable :exec
CREATE TEMP TABLE temp_mcp_server_remote ON COMMIT DROP AS
SELECT * FROM mcp_server_remote
  WITH NO DATA;

-- name: UpsertRemotesFromTemp :exec
INSERT INTO mcp_server_remote (server_id, transport, transport_url, transport_headers)
SELECT server_id, transport, transport_url, transport_headers
FROM temp_mcp_server_remote
ON CONFLICT (server_id, transport, transport_url)
DO UPDATE SET transport_headers = EXCLUDED.transport_headers;

-- name: DeleteOrphanedRemotes :exec
DELETE FROM mcp_server_remote
WHERE server_id = ANY(sqlc.slice(server_ids)::UUID[])
  AND (server_id, transport, transport_url) NOT IN (
    SELECT server_id, transport, transport_url FROM temp_mcp_server_remote
  );

-- Temp Icon Table Operations

-- name: CreateTempIconTable :exec
CREATE TEMP TABLE temp_mcp_server_icon ON COMMIT DROP AS
SELECT * FROM mcp_server_icon
  WITH NO DATA;

-- name: UpsertIconsFromTemp :exec
INSERT INTO mcp_server_icon (server_id, source_uri, mime_type, theme)
SELECT server_id, source_uri, mime_type, theme::icon_theme
FROM temp_mcp_server_icon
ON CONFLICT (server_id, source_uri, mime_type, theme)
DO NOTHING;

-- name: DeleteOrphanedIcons :exec
DELETE FROM mcp_server_icon
WHERE server_id = ANY(sqlc.slice(server_ids)::UUID[])
  AND (server_id, source_uri, mime_type, theme) NOT IN (
    SELECT server_id, source_uri, mime_type, theme FROM temp_mcp_server_icon
  );

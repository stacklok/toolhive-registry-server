-- Temporary table operations for bulk sync
-- Note: These queries reference temp tables that don't exist in the schema.
-- sqlc cannot validate these, but we organize them here for maintainability.

-- Temp Server Table Operations

-- name: CreateTempRegistryEntryTable :exec
CREATE TEMP TABLE temp_registry_entry ON COMMIT DROP AS
SELECT * FROM registry_entry
  WITH NO DATA;

-- name: UpsertRegistryEntriesFromTemp :many
INSERT INTO registry_entry (
    id, reg_id, entry_type, name, title, description, version, created_at, updated_at
)
SELECT id,
       reg_id,
       entry_type,
       name,
       title,
       description,
       version,
       created_at,
       updated_at
  FROM temp_registry_entry
    ON CONFLICT (reg_id, name, version)
    DO UPDATE SET
      entry_type = EXCLUDED.entry_type,
      title = EXCLUDED.title,
      description = EXCLUDED.description,
      updated_at = EXCLUDED.updated_at
RETURNING id, reg_id, entry_type, name, version;

-- name: CreateTempServerTable :exec
CREATE TEMP TABLE temp_mcp_server ON COMMIT DROP AS
SELECT * FROM mcp_server
  WITH NO DATA;

-- name: UpsertServersFromTemp :exec
INSERT INTO mcp_server (
    entry_id, website, upstream_meta, server_meta,
    repository_url, repository_id, repository_subfolder, repository_type
)
SELECT entry_id,
       website,
       upstream_meta,
       server_meta,
       repository_url,
       repository_id,
       repository_subfolder,
       repository_type
FROM temp_mcp_server
  ON CONFLICT (entry_id)
  DO UPDATE SET
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
    entry_id, registry_type, pkg_registry_url, pkg_identifier, pkg_version,
    runtime_hint, runtime_arguments, package_arguments, env_vars, sha256_hash,
    transport, transport_url, transport_headers
)
SELECT
    entry_id, registry_type, pkg_registry_url, pkg_identifier, pkg_version,
    runtime_hint, runtime_arguments, package_arguments, env_vars, sha256_hash,
    transport, transport_url, transport_headers
FROM temp_mcp_server_package
ON CONFLICT (entry_id, registry_type, pkg_identifier, transport)
DO UPDATE SET
    pkg_registry_url = EXCLUDED.pkg_registry_url,
    pkg_version = EXCLUDED.pkg_version,
    runtime_hint = EXCLUDED.runtime_hint,
    runtime_arguments = EXCLUDED.runtime_arguments,
    package_arguments = EXCLUDED.package_arguments,
    env_vars = EXCLUDED.env_vars,
    sha256_hash = EXCLUDED.sha256_hash,
    transport_url = EXCLUDED.transport_url,
    transport_headers = EXCLUDED.transport_headers;

-- name: DeleteOrphanedPackages :exec
DELETE FROM mcp_server_package
WHERE entry_id = ANY(sqlc.slice(entry_ids)::UUID[])
  AND (entry_id, pkg_identifier, transport) NOT IN (
    SELECT entry_id, pkg_identifier, transport FROM temp_mcp_server_package
  );

-- Temp Remote Table Operations

-- name: CreateTempRemoteTable :exec
CREATE TEMP TABLE temp_mcp_server_remote ON COMMIT DROP AS
SELECT * FROM mcp_server_remote
  WITH NO DATA;

-- name: UpsertRemotesFromTemp :exec
INSERT INTO mcp_server_remote (entry_id, transport, transport_url, transport_headers)
SELECT entry_id, transport, transport_url, transport_headers
FROM temp_mcp_server_remote
ON CONFLICT (entry_id, transport, transport_url)
DO UPDATE SET transport_headers = EXCLUDED.transport_headers;

-- name: DeleteOrphanedRemotes :exec
DELETE FROM mcp_server_remote
WHERE entry_id = ANY(sqlc.slice(entry_ids)::UUID[])
  AND (entry_id, transport, transport_url) NOT IN (
    SELECT entry_id, transport, transport_url FROM temp_mcp_server_remote
  );

-- Temp Icon Table Operations

-- name: CreateTempIconTable :exec
CREATE TEMP TABLE temp_mcp_server_icon ON COMMIT DROP AS
SELECT * FROM mcp_server_icon
  WITH NO DATA;

-- name: UpsertIconsFromTemp :exec
INSERT INTO mcp_server_icon (entry_id, source_uri, mime_type, theme)
SELECT entry_id, source_uri, mime_type, theme::icon_theme
FROM temp_mcp_server_icon
ON CONFLICT (entry_id, source_uri, mime_type, theme)
DO NOTHING;

-- name: DeleteOrphanedIcons :exec
DELETE FROM mcp_server_icon
WHERE entry_id = ANY(sqlc.slice(entry_ids)::UUID[])
  AND (entry_id, source_uri, mime_type, theme) NOT IN (
    SELECT entry_id, source_uri, mime_type, theme FROM temp_mcp_server_icon
  );

-- Temp Skill Table Operations

-- name: CreateTempSkillTable :exec
CREATE TEMP TABLE temp_skill ON COMMIT DROP AS
SELECT * FROM skill
  WITH NO DATA;

-- name: UpsertSkillsFromTemp :exec
INSERT INTO skill (
    entry_id, namespace, status, license, compatibility,
    allowed_tools, repository, icons, metadata, extension_meta
)
SELECT entry_id, namespace, status, license, compatibility,
       allowed_tools, repository, icons, metadata, extension_meta
FROM temp_skill
ON CONFLICT (entry_id)
DO UPDATE SET
    namespace = EXCLUDED.namespace,
    status = EXCLUDED.status,
    license = EXCLUDED.license,
    compatibility = EXCLUDED.compatibility,
    allowed_tools = EXCLUDED.allowed_tools,
    repository = EXCLUDED.repository,
    icons = EXCLUDED.icons,
    metadata = EXCLUDED.metadata,
    extension_meta = EXCLUDED.extension_meta;

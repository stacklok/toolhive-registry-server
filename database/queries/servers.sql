-- name: ListServers :many
-- Cursor-based pagination using (name, version) compound cursor.
-- The cursor_name and cursor_version parameters define the starting point.
-- When cursor is provided, results start AFTER the specified (name, version) tuple.
-- Returns position from registry_source for source priority ordering.
-- When name is provided, results are filtered to versions of that specific server.
SELECT src.source_type as registry_type,
       v.id,
       e.name,
       v.version,
       (l.latest_version_id IS NOT NULL)::boolean AS is_latest,
       v.created_at,
       v.updated_at,
       v.description,
       v.title,
       s.website,
       s.upstream_meta,
       s.server_meta,
       s.repository_url,
       s.repository_id,
       s.repository_subfolder,
       s.repository_type,
       e.claims,
       -- Sources not linked to the requested registry have no position; default to max int16
       -- so they sort after all explicitly positioned sources (lower position = higher priority).
       COALESCE(rs.position, 32767)::integer AS position
  FROM mcp_server s
  JOIN entry_version v ON s.version_id = v.id
  JOIN registry_entry e ON v.entry_id = e.id
  JOIN source src ON e.source_id = src.id
  LEFT JOIN latest_entry_version l ON v.id = l.latest_version_id
  JOIN registry_source rs ON rs.source_id = e.source_id
                          AND rs.registry_id = sqlc.arg(registry_id)::uuid
 WHERE (sqlc.narg(name)::text IS NULL OR e.name = sqlc.narg(name)::text)
   AND (sqlc.narg(search)::text IS NULL OR (
       LOWER(e.name) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
       OR LOWER(v.title) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
       OR LOWER(v.description) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
   ))
   -- Filter by updated_since if provided
   AND (sqlc.narg(updated_since)::timestamp with time zone IS NULL OR v.updated_at > sqlc.narg(updated_since)::timestamp with time zone)
   -- Compound cursor comparison: (name, version) > (cursor_name, cursor_version)
   -- This ensures deterministic pagination even when timestamps are identical
   AND (
       sqlc.narg(cursor_name)::text IS NULL
       OR (v.name, v.version) > (sqlc.narg(cursor_name)::text, sqlc.narg(cursor_version)::text)
   )
   AND (
       sqlc.narg(version)::text IS NULL OR
       v.version = sqlc.narg(version)::text OR
       (sqlc.narg(version)::text = 'latest' AND l.latest_version_id = v.id)
   )
 ORDER BY v.name ASC, v.version ASC, rs.position ASC
 LIMIT sqlc.arg(size)::bigint;

-- name: GetServerVersion :many
-- Despite the name, this query returns multiple rows. The actual number of
-- records is bounded by the number of sources that provide the same name and
-- version, which we currently don't expect to be more than a few.
-- Cursor-based pagination using (position, source_id) compound cursor.
-- position is the sort key but may not be unique; source_id is the tiebreaker.
SELECT src.source_type as registry_type,
       v.id,
       e.name,
       v.version,
       (l.latest_version_id IS NOT NULL)::boolean AS is_latest,
       v.created_at,
       v.updated_at,
       v.description,
       v.title,
       s.website,
       s.upstream_meta,
       s.server_meta,
       s.repository_url,
       s.repository_id,
       s.repository_subfolder,
       s.repository_type,
       e.claims,
       rs.source_id,
       -- Sources not linked to the requested registry have no position; default to max int16
       -- so they sort after all explicitly positioned sources (lower position = higher priority).
       COALESCE(rs.position, 32767)::integer AS position
  FROM entry_version v
  JOIN registry_entry e ON e.id = v.entry_id
  JOIN registry_source rs ON rs.source_id = e.source_id
                          AND rs.registry_id = sqlc.arg(registry_id)::uuid
  JOIN source src ON e.source_id = src.id
  JOIN mcp_server s ON s.version_id = v.id
  LEFT JOIN latest_entry_version l ON v.id = l.latest_version_id
 WHERE v.name = sqlc.arg(name)
  AND (
       v.version = sqlc.arg(version)
       OR (sqlc.arg(version) = 'latest' AND l.latest_version_id = v.id)
   )
   AND (sqlc.narg(source_name)::text IS NULL OR src.name = sqlc.narg(source_name)::text)
   AND (
       sqlc.narg(cursor_position)::integer IS NULL
       OR (rs.position > sqlc.narg(cursor_position)::integer
           AND rs.source_id > sqlc.narg(cursor_source_id)::uuid
       )
   )
 ORDER BY rs.position ASC, rs.source_id ASC
 LIMIT sqlc.arg(size)::bigint;

-- name: GetServerVersionBySourceName :one
-- Source-scoped variant of GetServerVersion used by the publish fetch-back path.
-- source_name and version are required; registry filtering is not applied.
SELECT src.source_type as registry_type,
       v.id,
       e.name,
       v.version,
       (l.latest_version_id IS NOT NULL)::boolean AS is_latest,
       v.created_at,
       v.updated_at,
       v.description,
       v.title,
       s.website,
       s.upstream_meta,
       s.server_meta,
       s.repository_url,
       s.repository_id,
       s.repository_subfolder,
       s.repository_type,
       e.claims,
       0::integer AS position
  FROM mcp_server s
  JOIN entry_version v ON s.version_id = v.id
  JOIN registry_entry e ON v.entry_id = e.id
  JOIN source src ON e.source_id = src.id
  LEFT JOIN latest_entry_version l ON v.id = l.latest_version_id
 WHERE v.name = sqlc.arg(name)
   AND v.version = sqlc.arg(version)
   AND src.name = sqlc.arg(source_name);

-- name: ListServerPackages :many
SELECT p.server_id,
       p.registry_type,
       p.pkg_registry_url,
       p.pkg_identifier,
       p.pkg_version,
       p.runtime_hint,
       p.runtime_arguments,
       p.package_arguments,
       p.env_vars,
       p.sha256_hash,
       p.transport,
       p.transport_url,
       p.transport_headers
  FROM mcp_server_package p
  JOIN mcp_server s ON p.server_id = s.version_id
 WHERE s.version_id = ANY(sqlc.slice(version_ids)::UUID[])
 ORDER BY p.pkg_version DESC;

-- name: ListServerRemotes :many
SELECT r.server_id,
       r.transport,
       r.transport_url,
       r.transport_headers
  FROM mcp_server_remote r
  JOIN mcp_server s ON r.server_id = s.version_id
 WHERE s.version_id = ANY(sqlc.slice(version_ids)::UUID[])
 ORDER BY r.transport, r.transport_url;

-- name: InsertServerVersion :one
INSERT INTO mcp_server (
    version_id,
    website,
    upstream_meta,
    server_meta,
    repository_url,
    repository_id,
    repository_subfolder,
    repository_type
) VALUES (
    sqlc.arg(version_id),
    sqlc.narg(website),
    sqlc.narg(upstream_meta),
    sqlc.narg(server_meta),
    sqlc.narg(repository_url),
    sqlc.narg(repository_id),
    sqlc.narg(repository_subfolder),
    sqlc.narg(repository_type)
)
RETURNING version_id;

-- name: UpsertLatestServerVersion :one
INSERT INTO latest_entry_version (
    source_id,
    name,
    version,
    latest_version_id
) VALUES (
    sqlc.arg(source_id),
    sqlc.arg(name),
    sqlc.arg(version),
    sqlc.arg(version_id)
) ON CONFLICT (source_id, name)
  DO UPDATE SET
    version = sqlc.arg(version),
    latest_version_id = sqlc.arg(version_id)
RETURNING latest_version_id;

-- name: InsertServerPackage :exec
-- TODO: this seems unused
INSERT INTO mcp_server_package (
    server_id,
    registry_type,
    pkg_registry_url,
    pkg_identifier,
    pkg_version,
    runtime_hint,
    runtime_arguments,
    package_arguments,
    env_vars,
    sha256_hash,
    transport,
    transport_url,
    transport_headers
) VALUES (
    sqlc.arg(server_id),
    sqlc.arg(registry_type),
    sqlc.arg(pkg_registry_url),
    sqlc.arg(pkg_identifier),
    sqlc.arg(pkg_version),
    sqlc.narg(runtime_hint),
    sqlc.narg(runtime_arguments),
    sqlc.narg(package_arguments),
    sqlc.narg(env_vars),
    sqlc.narg(sha256_hash),
    sqlc.arg(transport),
    sqlc.narg(transport_url),
    sqlc.narg(transport_headers)
);

-- name: InsertServerRemote :exec
INSERT INTO mcp_server_remote (
    server_id,
    transport,
    transport_url,
    transport_headers
) VALUES (
    sqlc.arg(server_id),
    sqlc.arg(transport),
    sqlc.arg(transport_url),
    sqlc.narg(transport_headers)
);

-- name: InsertServerIcon :exec
INSERT INTO mcp_server_icon (
    server_id,
    source_uri,
    mime_type,
    theme
) VALUES (
    sqlc.arg(server_id),
    sqlc.arg(source_uri),
    sqlc.arg(mime_type),
    sqlc.arg(theme)
);

-- name: DeleteServersByRegistry :exec
WITH registry_entries AS (
    SELECT e.id
      FROM registry_entry e
      JOIN entry_version v ON v.entry_id = e.id
      JOIN mcp_server s ON v.id = s.version_id
     WHERE e.source_id = sqlc.arg(source_id)
)
DELETE FROM registry_entry
 WHERE id IN (SELECT id FROM registry_entries);

-- name: InsertServerVersionForSync :one
INSERT INTO mcp_server (
    version_id,
    website,
    upstream_meta,
    server_meta,
    repository_url,
    repository_id,
    repository_subfolder,
    repository_type
) VALUES (
    sqlc.arg(version_id),
    sqlc.narg(website),
    sqlc.narg(upstream_meta),
    sqlc.narg(server_meta),
    sqlc.narg(repository_url),
    sqlc.narg(repository_id),
    sqlc.narg(repository_subfolder),
    sqlc.narg(repository_type)
)
RETURNING version_id;

-- name: UpsertServerVersionForSync :one
INSERT INTO mcp_server (
    version_id,
    website,
    upstream_meta,
    server_meta,
    repository_url,
    repository_id,
    repository_subfolder,
    repository_type
) VALUES (
    sqlc.arg(version_id),
    sqlc.narg(website),
    sqlc.narg(upstream_meta),
    sqlc.narg(server_meta),
    sqlc.narg(repository_url),
    sqlc.narg(repository_id),
    sqlc.narg(repository_subfolder),
    sqlc.narg(repository_type)
)
ON CONFLICT (version_id)
DO UPDATE SET
    website = sqlc.narg(website),
    upstream_meta = sqlc.narg(upstream_meta),
    server_meta = sqlc.narg(server_meta),
    repository_url = sqlc.narg(repository_url),
    repository_id = sqlc.narg(repository_id),
    repository_subfolder = sqlc.narg(repository_subfolder),
    repository_type = sqlc.narg(repository_type)
RETURNING version_id;

-- name: DeleteOrphanedEntryVersions :exec
WITH subset AS (
    SELECT v.id
      FROM entry_version v
      JOIN registry_entry e ON v.entry_id = e.id
     WHERE e.source_id = sqlc.arg(source_id)
       AND e.entry_type = sqlc.arg(entry_type)
       AND v.id != ALL(sqlc.slice(keep_ids)::UUID[])
)
DELETE FROM entry_version v
WHERE v.id IN (SELECT id FROM subset);

-- name: DeleteServerPackagesByServerId :exec
DELETE FROM mcp_server_package
WHERE server_id = sqlc.arg(server_id);

-- name: DeleteServerRemotesByServerId :exec
DELETE FROM mcp_server_remote
WHERE server_id = sqlc.arg(server_id);

-- name: DeleteServerIconsByServerId :exec
DELETE FROM mcp_server_icon
WHERE server_id = sqlc.arg(server_id);

-- name: GetServerIDsByRegistryNameVersion :many
SELECT s.version_id, e.name, v.version
  FROM entry_version v
  JOIN registry_entry e ON e.id = v.entry_id
  JOIN mcp_server s ON v.id = s.version_id
 WHERE e.source_id = sqlc.arg(source_id);

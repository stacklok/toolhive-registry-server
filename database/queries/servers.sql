-- name: ListServers :many
-- Cursor-based pagination using (name, version) compound cursor.
-- The cursor_name and cursor_version parameters define the starting point.
-- When cursor is provided, results start AFTER the specified (name, version) tuple.
SELECT r.reg_type as registry_type,
       e.id,
       e.name,
       e.version,
       (l.latest_entry_id IS NOT NULL)::boolean AS is_latest,
       e.created_at,
       e.updated_at,
       e.description,
       e.title,
       s.website,
       s.upstream_meta,
       s.server_meta,
       s.repository_url,
       s.repository_id,
       s.repository_subfolder,
       s.repository_type
  FROM mcp_server s
  JOIN registry_entry e ON s.entry_id = e.id
  JOIN registry r ON e.reg_id = r.id
  LEFT JOIN latest_entry_version l ON e.id = l.latest_entry_id
 WHERE (sqlc.narg(registry_name)::text IS NULL OR r.name = sqlc.narg(registry_name)::text)
   AND (sqlc.narg(search)::text IS NULL OR (
       LOWER(e.name) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
       OR LOWER(e.title) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
       OR LOWER(e.description) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
   ))
   -- Filter by updated_since if provided
   AND (sqlc.narg(updated_since)::timestamp with time zone IS NULL OR e.updated_at > sqlc.narg(updated_since)::timestamp with time zone)
   -- Compound cursor comparison: (name, version) > (cursor_name, cursor_version)
   -- This ensures deterministic pagination even when timestamps are identical
   AND (
       sqlc.narg(cursor_name)::text IS NULL
       OR (e.name, e.version) > (sqlc.narg(cursor_name)::text, sqlc.narg(cursor_version)::text)
   )
   AND (
       sqlc.narg(version)::text IS NULL OR
       e.version = sqlc.narg(version)::text OR
       (sqlc.narg(version)::text = 'latest' AND l.latest_entry_id = e.id)
   )
 ORDER BY e.name ASC, e.version ASC
 LIMIT sqlc.arg(size)::bigint;

-- name: ListServerVersions :many
SELECT r.reg_type as registry_type,
       e.id,
       e.name,
       e.version,
       (l.latest_entry_id IS NOT NULL)::boolean AS is_latest,
       e.created_at,
       e.updated_at,
       e.description,
       e.title,
       s.website,
       s.upstream_meta,
       s.server_meta,
       s.repository_url,
       s.repository_id,
       s.repository_subfolder,
       s.repository_type
  FROM mcp_server s
  JOIN registry_entry e ON s.entry_id = e.id
  JOIN registry r ON e.reg_id = r.id
  LEFT JOIN latest_entry_version l ON e.id = l.latest_entry_id
 WHERE e.name = sqlc.arg(name)
   AND (sqlc.narg(registry_name)::text IS NULL OR r.name = sqlc.narg(registry_name)::text)
   AND ((sqlc.narg(next)::timestamp with time zone IS NULL OR e.created_at > sqlc.narg(next))
    AND (sqlc.narg(prev)::timestamp with time zone IS NULL OR e.created_at < sqlc.narg(prev)))
 ORDER BY
 CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN e.created_at END ASC,
 CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN e.version END DESC -- acts as tie breaker
 LIMIT sqlc.arg(size)::bigint;

-- name: GetServerVersion :one
SELECT r.reg_type as registry_type,
       e.id,
       e.name,
       e.version,
       (l.latest_entry_id IS NOT NULL)::boolean AS is_latest,
       e.created_at,
       e.updated_at,
       e.description,
       e.title,
       s.website,
       s.upstream_meta,
       s.server_meta,
       s.repository_url,
       s.repository_id,
       s.repository_subfolder,
       s.repository_type
  FROM mcp_server s
  JOIN registry_entry e ON s.entry_id = e.id
  JOIN registry r ON e.reg_id = r.id
  LEFT JOIN latest_entry_version l ON e.id = l.latest_entry_id
 WHERE e.name = sqlc.arg(name)
   AND (
       e.version = sqlc.arg(version)
       OR (sqlc.arg(version) = 'latest' AND l.latest_entry_id = e.id)
   )
   AND (sqlc.narg(registry_name)::text IS NULL OR r.name = sqlc.narg(registry_name)::text);

-- name: GetLatestVersionForServer :one
SELECT l.version
  FROM latest_entry_version l
 WHERE l.name = sqlc.arg(name)
   AND l.reg_id = sqlc.arg(reg_id);

-- name: ListServerPackages :many
SELECT p.entry_id,
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
  JOIN mcp_server s ON p.entry_id = s.entry_id
 WHERE s.entry_id = ANY(sqlc.slice(entry_ids)::UUID[])
 ORDER BY p.pkg_version DESC;

-- name: ListServerRemotes :many
SELECT r.entry_id,
       r.transport,
       r.transport_url,
       r.transport_headers
  FROM mcp_server_remote r
  JOIN mcp_server s ON r.entry_id = s.entry_id
 WHERE s.entry_id = ANY(sqlc.slice(entry_ids)::UUID[])
 ORDER BY r.transport, r.transport_url;

-- name: InsertServerVersion :one
INSERT INTO mcp_server (
    entry_id,
    website,
    upstream_meta,
    server_meta,
    repository_url,
    repository_id,
    repository_subfolder,
    repository_type
) VALUES (
    sqlc.arg(entry_id),
    sqlc.narg(website),
    sqlc.narg(upstream_meta),
    sqlc.narg(server_meta),
    sqlc.narg(repository_url),
    sqlc.narg(repository_id),
    sqlc.narg(repository_subfolder),
    sqlc.narg(repository_type)
)
RETURNING entry_id;

-- name: UpsertLatestServerVersion :one
INSERT INTO latest_entry_version (
    reg_id,
    name,
    version,
    latest_entry_id
) VALUES (
    sqlc.arg(reg_id),
    sqlc.arg(name),
    sqlc.arg(version),
    sqlc.arg(entry_id)
) ON CONFLICT (reg_id, name)
  DO UPDATE SET
    version = sqlc.arg(version),
    latest_entry_id = sqlc.arg(entry_id)
RETURNING latest_entry_id;

-- name: InsertServerPackage :exec
-- TODO: this seems unused
INSERT INTO mcp_server_package (
    entry_id,
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
    sqlc.arg(entry_id),
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
    entry_id,
    transport,
    transport_url,
    transport_headers
) VALUES (
    sqlc.arg(entry_id),
    sqlc.arg(transport),
    sqlc.arg(transport_url),
    sqlc.narg(transport_headers)
);

-- name: InsertServerIcon :exec
INSERT INTO mcp_server_icon (
    entry_id,
    source_uri,
    mime_type,
    theme
) VALUES (
    sqlc.arg(entry_id),
    sqlc.arg(source_uri),
    sqlc.arg(mime_type),
    sqlc.arg(theme)
);

-- name: DeleteServersByRegistry :exec
WITH registry_entries AS (
    SELECT e.id
      FROM registry_entry e
      JOIN mcp_server s ON e.id = s.entry_id
     WHERE e.reg_id = sqlc.arg(reg_id)
)
DELETE FROM registry_entry
 WHERE id IN (SELECT id FROM registry_entries);

-- name: InsertServerVersionForSync :one
INSERT INTO mcp_server (
    entry_id,
    website,
    upstream_meta,
    server_meta,
    repository_url,
    repository_id,
    repository_subfolder,
    repository_type
) VALUES (
    sqlc.arg(entry_id),
    sqlc.narg(website),
    sqlc.narg(upstream_meta),
    sqlc.narg(server_meta),
    sqlc.narg(repository_url),
    sqlc.narg(repository_id),
    sqlc.narg(repository_subfolder),
    sqlc.narg(repository_type)
)
RETURNING entry_id;

-- name: UpsertServerVersionForSync :one
INSERT INTO mcp_server (
    entry_id,
    website,
    upstream_meta,
    server_meta,
    repository_url,
    repository_id,
    repository_subfolder,
    repository_type
) VALUES (
    sqlc.arg(entry_id),
    sqlc.narg(website),
    sqlc.narg(upstream_meta),
    sqlc.narg(server_meta),
    sqlc.narg(repository_url),
    sqlc.narg(repository_id),
    sqlc.narg(repository_subfolder),
    sqlc.narg(repository_type)
)
ON CONFLICT (entry_id)
DO UPDATE SET
    website = sqlc.narg(website),
    upstream_meta = sqlc.narg(upstream_meta),
    server_meta = sqlc.narg(server_meta),
    repository_url = sqlc.narg(repository_url),
    repository_id = sqlc.narg(repository_id),
    repository_subfolder = sqlc.narg(repository_subfolder),
    repository_type = sqlc.narg(repository_type)
RETURNING entry_id;

-- name: DeleteOrphanedServers :exec
WITH subset AS (
    SELECT e.id
      FROM registry_entry e
     WHERE reg_id = sqlc.arg(reg_id)
       AND e.id != ALL(sqlc.slice(keep_ids)::UUID[])
)
DELETE FROM mcp_server s
WHERE s.entry_id IN (SELECT id FROM subset);

-- name: DeleteServerPackagesByServerId :exec
DELETE FROM mcp_server_package
WHERE entry_id = sqlc.arg(entry_id);

-- name: DeleteServerRemotesByServerId :exec
DELETE FROM mcp_server_remote
WHERE entry_id = sqlc.arg(entry_id);

-- name: DeleteServerIconsByServerId :exec
DELETE FROM mcp_server_icon
WHERE entry_id = sqlc.arg(entry_id);

-- name: GetServerIDsByRegistryNameVersion :many
SELECT entry_id, name, version
FROM registry_entry e
JOIN mcp_server s ON e.id = s.entry_id
WHERE e.reg_id = sqlc.arg(reg_id);

-- name: ListServers :many
SELECT r.reg_type as registry_type,
       s.id,
       s.name,
       s.version,
       (l.latest_server_id IS NOT NULL)::boolean AS is_latest,
       s.created_at,
       s.updated_at,
       s.description,
       s.title,
       s.website,
       s.upstream_meta,
       s.server_meta,
       s.repository_url,
       s.repository_id,
       s.repository_subfolder,
       s.repository_type
  FROM mcp_server s
  JOIN registry r ON s.reg_id = r.id
  LEFT JOIN latest_server_version l ON s.id = l.latest_server_id
 WHERE (sqlc.narg(next)::timestamp with time zone IS NULL OR s.created_at > sqlc.narg(next))
   AND (sqlc.narg(prev)::timestamp with time zone IS NULL OR s.created_at < sqlc.narg(prev))
 ORDER BY
 -- next page sorting
 CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN r.reg_type END ASC,
 CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN s.name END ASC,
 CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN s.created_at END ASC,
 CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN s.version END ASC, -- acts as tie breaker
 -- previous page sorting
 CASE WHEN sqlc.narg(prev)::timestamp with time zone IS NULL THEN r.reg_type END DESC,
 CASE WHEN sqlc.narg(prev)::timestamp with time zone IS NULL THEN s.name END DESC,
 CASE WHEN sqlc.narg(prev)::timestamp with time zone IS NULL THEN s.created_at END DESC,
 CASE WHEN sqlc.narg(prev)::timestamp with time zone IS NULL THEN s.version END DESC -- acts as tie breaker
 LIMIT sqlc.arg(size)::bigint;

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
  JOIN mcp_server s ON p.server_id = s.id
 WHERE s.id = ANY(sqlc.slice(server_ids)::UUID[])
 ORDER BY p.pkg_version DESC;

-- name: ListServerRemotes :many
SELECT r.server_id,
       r.transport,
       r.transport_url,
       r.transport_headers
  FROM mcp_server_remote r
 WHERE r.server_id = ANY(sqlc.slice(server_ids)::UUID[])
 ORDER BY r.transport, r.transport_url;

-- name: ListServerVersions :many
SELECT s.id,
       s.name,
       s.version,
       s.created_at,
       s.updated_at,
       s.description,
       s.title,
       s.website,
       s.upstream_meta,
       s.server_meta,
       s.repository_url,
       s.repository_id,
       s.repository_subfolder,
       s.repository_type
  FROM mcp_server s
 WHERE s.name = sqlc.arg(name)
   AND ((sqlc.narg(next)::timestamp with time zone IS NULL OR s.created_at > sqlc.narg(next))
    AND (sqlc.narg(prev)::timestamp with time zone IS NULL OR s.created_at < sqlc.narg(prev)))
 ORDER BY
 CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN s.created_at END ASC,
 CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN s.version END DESC -- acts as tie breaker
 LIMIT sqlc.arg(size)::bigint;

-- name: UpsertServerVersion :one
INSERT INTO mcp_server (
    name,
    version,
    reg_id,
    created_at,
    updated_at,
    description,
    title,
    website,
    upstream_meta,
    server_meta,
    repository_url,
    repository_id,
    repository_subfolder,
    repository_type
) VALUES (
    sqlc.arg(name),
    sqlc.arg(version),
    sqlc.arg(reg_id),
    sqlc.arg(created_at),
    sqlc.arg(updated_at),
    sqlc.narg(description),
    sqlc.narg(title),
    sqlc.narg(website),
    sqlc.narg(upstream_meta),
    sqlc.narg(server_meta),
    sqlc.narg(repository_url),
    sqlc.narg(repository_id),
    sqlc.narg(repository_subfolder),
    sqlc.narg(repository_type)
) ON CONFLICT (reg_id, name, version)
  DO UPDATE SET
    updated_at = sqlc.arg(updated_at),
    description = sqlc.narg(description),
    title = sqlc.narg(title),
    website = sqlc.narg(website),
    upstream_meta = sqlc.narg(upstream_meta),
    server_meta = sqlc.narg(server_meta),
    repository_url = sqlc.narg(repository_url),
    repository_id = sqlc.narg(repository_id),
    repository_subfolder = sqlc.narg(repository_subfolder),
    repository_type = sqlc.narg(repository_type)
RETURNING id;

-- name: UpsertLatestServerVersion :one
INSERT INTO latest_server_version (
    reg_id,
    name,
    version,
    latest_server_id
) VALUES (
    sqlc.arg(reg_id),
    sqlc.arg(name),
    sqlc.arg(version),
    sqlc.arg(server_id)
) ON CONFLICT (reg_id, name)
  DO UPDATE SET
    version = sqlc.arg(version),
    latest_server_id = sqlc.arg(server_id)
RETURNING latest_server_id;

-- name: UpsertServerPackage :exec
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

-- name: UpsertServerRemote :exec
INSERT INTO mcp_server_remote (
    server_id,
    transport,
    transport_url,
    transport_headers
) VALUES (
    sqlc.arg(server_id),
    sqlc.arg(transport),
    sqlc.narg(transport_url),
    sqlc.narg(transport_headers)
) ON CONFLICT (server_id, transport, transport_url)
  DO UPDATE SET
    transport_headers = sqlc.narg(transport_headers);

-- name: UpsertServerIcon :exec
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
) ON CONFLICT (server_id, source_uri, mime_type, theme)
  DO UPDATE SET
    theme = sqlc.arg(theme);

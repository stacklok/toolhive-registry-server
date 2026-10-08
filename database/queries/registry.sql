-- Queries for the new lightweight registry table and registry_source junction.

-- name: ListRegistries :many
SELECT id, name, claims, creation_type, created_at, updated_at
FROM registry
WHERE (sqlc.narg(cursor)::text IS NULL OR name > sqlc.narg(cursor))
ORDER BY name
LIMIT sqlc.arg(size)::bigint;

-- name: GetRegistryByName :one
SELECT id, name, claims, creation_type, created_at, updated_at
FROM registry WHERE name = sqlc.arg(name);

-- name: GetAPIRegistriesByNames :many
SELECT id, name, claims, creation_type, created_at, updated_at
FROM registry
WHERE name = ANY(sqlc.arg(names)::text[])
  AND creation_type = 'API';

-- name: UpsertConfigRegistry :one
-- Insert or update a CONFIG registry without overwriting API-owned rows.
INSERT INTO registry (name, claims, creation_type, created_at, updated_at)
VALUES (sqlc.arg(name), sqlc.narg(claims), 'CONFIG', sqlc.arg(created_at), sqlc.arg(updated_at))
ON CONFLICT (name) DO UPDATE SET claims = EXCLUDED.claims, updated_at = EXCLUDED.updated_at
WHERE registry.creation_type = 'CONFIG'
RETURNING *;

-- name: UpsertRegistry :one
-- Insert or update a registry. The creation_type is passed as a parameter.
-- Business logic in Go guards against cross-type overwrites.
INSERT INTO registry (name, claims, creation_type, created_at, updated_at)
VALUES (sqlc.arg(name), sqlc.narg(claims), sqlc.arg(creation_type), sqlc.arg(created_at), sqlc.arg(updated_at))
ON CONFLICT (name) DO UPDATE SET claims = EXCLUDED.claims, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: DeleteRegistry :execrows
-- Delete a registry by name. Go callers guard against deleting wrong creation_type.
DELETE FROM registry WHERE name = sqlc.arg(name);

-- name: LinkRegistrySource :exec
INSERT INTO registry_source (registry_id, source_id, position)
VALUES (sqlc.arg(registry_id), sqlc.arg(source_id), sqlc.arg(position))
ON CONFLICT (registry_id, source_id) DO UPDATE SET position = EXCLUDED.position;

-- name: UnlinkRegistrySource :exec
DELETE FROM registry_source
WHERE registry_id = sqlc.arg(registry_id) AND source_id = sqlc.arg(source_id);

-- name: UnlinkAllRegistrySources :exec
DELETE FROM registry_source WHERE registry_id = sqlc.arg(registry_id);

-- name: DeleteConfigLinksToObsoleteManagedSources :exec
-- Unlink only CONFIG views before removing obsolete managed CONFIG sources.
-- API view links remain, so the source FK rejects their removal.
DELETE FROM registry_source rs
USING registry r, source s
WHERE rs.registry_id = r.id AND rs.source_id = s.id
  AND r.creation_type = 'CONFIG' AND s.creation_type = 'CONFIG'
  AND s.source_type = 'managed' AND s.name != ALL(sqlc.arg(keep_names)::text[]);

-- name: DeleteConfigRegistriesNotInList :exec
-- Delete CONFIG registry rows whose names are not in the provided list.
-- Used during config sync to clean up registry/junction rows before deleting orphaned sources.
DELETE FROM registry
WHERE creation_type = 'CONFIG'
  AND name NOT IN (SELECT unnest(sqlc.arg(keep_names)::text[]));

-- name: CountRegistriesBySourceID :one
-- Count how many registries reference a given source (via registry_source junction).
SELECT COUNT(*) FROM registry_source WHERE source_id = sqlc.arg(source_id);

-- name: ListRegistrySources :many
SELECT s.id, s.name
FROM registry_source rs
JOIN source s ON rs.source_id = s.id
WHERE rs.registry_id = sqlc.arg(registry_id)
ORDER BY rs.position;


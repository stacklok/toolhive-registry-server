-- Queries for the new lightweight registry table and registry_source junction.

-- name: ListRegistries :many
SELECT id, name, claims, creation_type, created_at, updated_at
FROM registry ORDER BY name;

-- name: GetRegistryByName :one
SELECT id, name, claims, creation_type, created_at, updated_at
FROM registry WHERE name = sqlc.arg(name);

-- name: UpsertAPIRegistry :one
-- Insert or update an API registry. The ON CONFLICT clause only fires when the
-- existing row is also API-created, preventing CONFIG registries from being overwritten.
INSERT INTO registry (name, claims, creation_type, created_at, updated_at)
VALUES (sqlc.arg(name), sqlc.narg(claims), 'API', sqlc.arg(created_at), sqlc.arg(updated_at))
ON CONFLICT (name) DO UPDATE SET updated_at = EXCLUDED.updated_at
WHERE registry.creation_type = 'API'
RETURNING *;

-- name: UpsertConfigRegistry :one
-- Insert or update a CONFIG registry. The ON CONFLICT clause only fires when the
-- existing row is also CONFIG-created, preventing API registries from being overwritten.
INSERT INTO registry (name, claims, creation_type, created_at, updated_at)
VALUES (sqlc.arg(name), sqlc.narg(claims), 'CONFIG', sqlc.arg(created_at), sqlc.arg(updated_at))
ON CONFLICT (name) DO UPDATE SET updated_at = EXCLUDED.updated_at
WHERE registry.creation_type = 'CONFIG'
RETURNING *;

-- name: DeleteRegistry :execrows
DELETE FROM registry WHERE name = sqlc.arg(name);

-- name: DeleteAPIRegistry :execrows
-- Delete an API registry by name (returns 0 if not found or is CONFIG type)
DELETE FROM registry WHERE name = sqlc.arg(name) AND creation_type = 'API';

-- name: LinkRegistrySource :exec
INSERT INTO registry_source (registry_id, source_id, position)
VALUES (sqlc.arg(registry_id), sqlc.arg(source_id), sqlc.arg(position))
ON CONFLICT (registry_id, source_id) DO UPDATE SET position = EXCLUDED.position;

-- name: UnlinkRegistrySource :exec
DELETE FROM registry_source
WHERE registry_id = sqlc.arg(registry_id) AND source_id = sqlc.arg(source_id);

-- name: DeleteConfigRegistriesNotInList :exec
-- Delete CONFIG registry rows whose names are not in the provided list.
-- Used during config sync to clean up registry/junction rows before deleting orphaned sources.
DELETE FROM registry
WHERE creation_type = 'CONFIG'
  AND name NOT IN (SELECT unnest(sqlc.arg(keep_names)::text[]));

-- name: ListRegistrySources :many
SELECT s.id, s.name
FROM registry_source rs
JOIN source s ON rs.source_id = s.id
WHERE rs.registry_id = sqlc.arg(registry_id)
ORDER BY rs.position;


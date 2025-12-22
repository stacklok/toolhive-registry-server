-- name: ListRegistries :many
SELECT id,
       name,
       reg_type,
       creation_type,
       source_type,
       format,
       source_config,
       filter_config,
       sync_schedule,
       created_at,
       updated_at
  FROM registry
 WHERE (sqlc.narg(next)::timestamp with time zone IS NULL OR created_at > sqlc.narg(next))
   AND (sqlc.narg(prev)::timestamp with time zone IS NULL OR created_at < sqlc.narg(prev))
 ORDER BY
  -- next page sorting
  CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN created_at END ASC,
  CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN name END ASC,
  -- previous page sorting
  CASE WHEN sqlc.narg(prev)::timestamp with time zone IS NULL THEN created_at END DESC,
  CASE WHEN sqlc.narg(prev)::timestamp with time zone IS NULL THEN name END DESC
 LIMIT sqlc.arg(size)::bigint;

-- name: GetRegistry :one
SELECT id,
       name,
       reg_type,
       creation_type,
       source_type,
       format,
       source_config,
       filter_config,
       sync_schedule,
       created_at,
       updated_at
  FROM registry
 WHERE id = sqlc.arg(id);

-- name: GetRegistryByName :one
SELECT id,
       name,
       reg_type,
       creation_type,
       source_type,
       format,
       source_config,
       filter_config,
       sync_schedule,
       created_at,
       updated_at
  FROM registry
 WHERE name = sqlc.arg(name);

-- ============================================================================
-- CONFIG Registry Queries (only operate on creation_type='CONFIG')
-- ============================================================================

-- name: InsertConfigRegistry :one
-- Insert a new CONFIG registry with full configuration
INSERT INTO registry (
    name,
    reg_type,
    creation_type,
    source_type,
    format,
    source_config,
    filter_config,
    sync_schedule,
    syncable,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(name),
    sqlc.arg(reg_type),
    'CONFIG',
    sqlc.arg(source_type),
    sqlc.narg(format),
    sqlc.narg(source_config),
    sqlc.narg(filter_config),
    sqlc.narg(sync_schedule),
    sqlc.arg(syncable),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
) RETURNING id;

-- name: UpsertConfigRegistry :one
-- Insert or update a CONFIG registry (only updates if existing is CONFIG type)
INSERT INTO registry (
    name,
    reg_type,
    creation_type,
    source_type,
    format,
    source_config,
    filter_config,
    sync_schedule,
    syncable,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(name),
    sqlc.arg(reg_type),
    'CONFIG',
    sqlc.arg(source_type),
    sqlc.narg(format),
    sqlc.narg(source_config),
    sqlc.narg(filter_config),
    sqlc.narg(sync_schedule),
    sqlc.arg(syncable),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
)
ON CONFLICT (name) DO UPDATE SET
    reg_type = EXCLUDED.reg_type,
    source_type = EXCLUDED.source_type,
    format = EXCLUDED.format,
    source_config = EXCLUDED.source_config,
    filter_config = EXCLUDED.filter_config,
    sync_schedule = EXCLUDED.sync_schedule,
    syncable = EXCLUDED.syncable,
    updated_at = EXCLUDED.updated_at
WHERE registry.creation_type = 'CONFIG'
RETURNING id;

-- name: BulkUpsertConfigRegistries :many
-- Bulk insert or update CONFIG registries (only updates existing CONFIG registries)
INSERT INTO registry (
    name,
    reg_type,
    creation_type,
    source_type,
    format,
    source_config,
    filter_config,
    sync_schedule,
    syncable,
    created_at,
    updated_at
)
SELECT
    unnest(sqlc.arg(names)::text[]),
    unnest(sqlc.arg(reg_types)::registry_type[]),
    'CONFIG',
    unnest(sqlc.arg(source_types)::text[]),
    unnest(sqlc.arg(formats)::text[]),
    unnest(sqlc.arg(source_configs)::jsonb[]),
    unnest(sqlc.arg(filter_configs)::jsonb[]),
    unnest(sqlc.arg(sync_schedules)::interval[]),
    unnest(sqlc.arg(syncables)::boolean[]),
    unnest(sqlc.arg(created_ats)::timestamp with time zone[]),
    unnest(sqlc.arg(updated_ats)::timestamp with time zone[])
ON CONFLICT (name) DO UPDATE SET
    reg_type = EXCLUDED.reg_type,
    source_type = EXCLUDED.source_type,
    format = EXCLUDED.format,
    source_config = EXCLUDED.source_config,
    filter_config = EXCLUDED.filter_config,
    sync_schedule = EXCLUDED.sync_schedule,
    syncable = EXCLUDED.syncable,
    updated_at = EXCLUDED.updated_at
WHERE registry.creation_type = 'CONFIG'
RETURNING id, name;

-- name: DeleteConfigRegistriesNotInList :exec
-- Delete CONFIG registries not in the provided list (for config file sync)
DELETE FROM registry
WHERE id NOT IN (SELECT unnest(sqlc.arg(ids)::uuid[]))
  AND creation_type = 'CONFIG';

-- name: DeleteConfigRegistry :execrows
-- Delete a CONFIG registry by name (returns 0 if not found or is API type)
DELETE FROM registry
WHERE name = sqlc.arg(name)
  AND creation_type = 'CONFIG';

-- ============================================================================
-- API Registry Queries (only operate on creation_type='API')
-- ============================================================================

-- name: InsertAPIRegistry :one
-- Insert a new API registry with full configuration
INSERT INTO registry (
    name,
    reg_type,
    creation_type,
    source_type,
    format,
    source_config,
    filter_config,
    sync_schedule,
    syncable,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(name),
    sqlc.arg(reg_type),
    'API',
    sqlc.arg(source_type),
    sqlc.narg(format),
    sqlc.narg(source_config),
    sqlc.narg(filter_config),
    sqlc.narg(sync_schedule),
    sqlc.arg(syncable),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
) RETURNING *;

-- name: UpdateAPIRegistry :one
-- Update an existing API registry (returns NULL if not found or is CONFIG type)
UPDATE registry SET
    reg_type = sqlc.arg(reg_type),
    source_type = sqlc.arg(source_type),
    format = sqlc.narg(format),
    source_config = sqlc.narg(source_config),
    filter_config = sqlc.narg(filter_config),
    sync_schedule = sqlc.narg(sync_schedule),
    syncable = sqlc.arg(syncable),
    updated_at = sqlc.arg(updated_at)
WHERE name = sqlc.arg(name)
  AND creation_type = 'API'
RETURNING *;

-- name: DeleteAPIRegistry :execrows
-- Delete an API registry by name (returns 0 if not found or is CONFIG type)
DELETE FROM registry
WHERE name = sqlc.arg(name)
  AND creation_type = 'API';

-- name: ListAllRegistryNames :many
SELECT name FROM registry ORDER BY name;

-- name: GetAPIRegistriesByNames :many
SELECT id,
       name,
       reg_type,
       creation_type,
       source_type,
       format,
       source_config,
       filter_config,
       sync_schedule,
       created_at,
       updated_at
FROM registry
WHERE name = ANY(sqlc.arg(names)::text[])
  AND creation_type = 'API';
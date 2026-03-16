-- name: ListSources :many
SELECT id,
       name,
       creation_type,
       source_type,
       format,
       source_config,
       filter_config,
       sync_schedule,
       created_at,
       updated_at
  FROM source
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

-- name: GetSource :one
SELECT id,
       name,
       creation_type,
       source_type,
       format,
       source_config,
       filter_config,
       sync_schedule,
       created_at,
       updated_at
  FROM source
 WHERE id = sqlc.arg(id);

-- name: GetSourceByName :one
SELECT id,
       name,
       creation_type,
       source_type,
       format,
       source_config,
       filter_config,
       sync_schedule,
       created_at,
       updated_at
  FROM source
 WHERE name = sqlc.arg(name);

-- ============================================================================
-- CONFIG Source Queries (only operate on creation_type='CONFIG')
-- ============================================================================

-- name: InsertConfigSource :one
-- Insert a new CONFIG source with full configuration
INSERT INTO source (
    name,
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

-- name: UpsertConfigSource :one
-- Insert or update a CONFIG source (only updates if existing is CONFIG type)
INSERT INTO source (
    name,
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
    source_type = EXCLUDED.source_type,
    format = EXCLUDED.format,
    source_config = EXCLUDED.source_config,
    filter_config = EXCLUDED.filter_config,
    sync_schedule = EXCLUDED.sync_schedule,
    syncable = EXCLUDED.syncable,
    updated_at = EXCLUDED.updated_at
WHERE source.creation_type = 'CONFIG'
RETURNING id;

-- name: BulkUpsertConfigSources :many
-- Bulk insert or update CONFIG sources (only updates existing CONFIG sources)
INSERT INTO source (
    name,
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
    source_type = EXCLUDED.source_type,
    format = EXCLUDED.format,
    source_config = EXCLUDED.source_config,
    filter_config = EXCLUDED.filter_config,
    sync_schedule = EXCLUDED.sync_schedule,
    syncable = EXCLUDED.syncable,
    updated_at = EXCLUDED.updated_at
WHERE source.creation_type = 'CONFIG'
RETURNING id, name;

-- name: DeleteConfigSourcesNotInList :exec
-- Delete CONFIG sources not in the provided list (for config file sync)
DELETE FROM source
WHERE id NOT IN (SELECT unnest(sqlc.arg(ids)::uuid[]))
  AND creation_type = 'CONFIG';

-- name: DeleteConfigSource :execrows
-- Delete a CONFIG source by name (returns 0 if not found or is API type)
DELETE FROM source
WHERE name = sqlc.arg(name)
  AND creation_type = 'CONFIG';

-- ============================================================================
-- API Source Queries (only operate on creation_type='API')
-- ============================================================================

-- name: InsertAPISource :one
-- Insert a new API source with full configuration
INSERT INTO source (
    name,
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

-- name: UpdateAPISource :one
-- Update an existing API source (returns NULL if not found or is CONFIG type)
UPDATE source SET
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

-- name: DeleteAPISource :execrows
-- Delete an API source by name (returns 0 if not found or is CONFIG type)
DELETE FROM source
WHERE name = sqlc.arg(name)
  AND creation_type = 'API';

-- name: ListAllSourceNames :many
SELECT name FROM source ORDER BY name;

-- name: GetAPISourcesByNames :many
SELECT id,
       name,
       creation_type,
       source_type,
       format,
       source_config,
       filter_config,
       sync_schedule,
       created_at,
       updated_at
FROM source
WHERE name = ANY(sqlc.arg(names)::text[])
  AND creation_type = 'API';

-- name: GetManagedSource :one
SELECT id, name, source_type, creation_type, created_at, updated_at
FROM source
WHERE source_type = 'managed'
LIMIT 1;

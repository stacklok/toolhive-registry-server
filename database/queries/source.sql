-- name: ListSources :many
SELECT id,
       name,
       creation_type,
       source_type,
       source_config,
       filter_config,
       sync_schedule,
       claims,
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
       source_config,
       filter_config,
       sync_schedule,
       claims,
       created_at,
       updated_at
  FROM source
 WHERE id = sqlc.arg(id);

-- name: GetSourceByName :one
SELECT id,
       name,
       creation_type,
       source_type,
       source_config,
       filter_config,
       sync_schedule,
       claims,
       created_at,
       updated_at
  FROM source
 WHERE name = sqlc.arg(name);

-- ============================================================================
-- CONFIG Source Queries (only operate on creation_type='CONFIG')
-- ============================================================================

-- name: UpsertSource :one
-- Insert or update a source. The creation_type is passed as a parameter.
-- Business logic in Go guards against cross-type overwrites.
INSERT INTO source (
    name,
    creation_type,
    source_type,
    source_config,
    filter_config,
    sync_schedule,
    syncable,
    claims,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(name),
    sqlc.arg(creation_type),
    sqlc.arg(source_type),
    sqlc.narg(source_config),
    sqlc.narg(filter_config),
    sqlc.narg(sync_schedule),
    sqlc.arg(syncable),
    sqlc.narg(claims),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
)
ON CONFLICT (name) DO UPDATE SET
    source_type = EXCLUDED.source_type,
    source_config = EXCLUDED.source_config,
    filter_config = EXCLUDED.filter_config,
    sync_schedule = EXCLUDED.sync_schedule,
    syncable = EXCLUDED.syncable,
    claims = EXCLUDED.claims,
    updated_at = EXCLUDED.updated_at
RETURNING id;

-- name: BulkUpsertConfigSources :many
-- Bulk insert or update CONFIG sources (only updates existing CONFIG sources)
INSERT INTO source (
    name,
    creation_type,
    source_type,
    source_config,
    filter_config,
    sync_schedule,
    syncable,
    claims,
    created_at,
    updated_at
)
SELECT
    unnest(sqlc.arg(names)::text[]),
    'CONFIG',
    unnest(sqlc.arg(source_types)::text[]),
    unnest(sqlc.arg(source_configs)::jsonb[]),
    unnest(sqlc.arg(filter_configs)::jsonb[]),
    unnest(sqlc.arg(sync_schedules)::interval[]),
    unnest(sqlc.arg(syncables)::boolean[]),
    unnest(sqlc.arg(claims)::jsonb[]),
    unnest(sqlc.arg(created_ats)::timestamp with time zone[]),
    unnest(sqlc.arg(updated_ats)::timestamp with time zone[])
ON CONFLICT (name) DO UPDATE SET
    source_type = EXCLUDED.source_type,
    source_config = EXCLUDED.source_config,
    filter_config = EXCLUDED.filter_config,
    sync_schedule = EXCLUDED.sync_schedule,
    syncable = EXCLUDED.syncable,
    claims = EXCLUDED.claims,
    updated_at = EXCLUDED.updated_at
WHERE source.creation_type = 'CONFIG'
RETURNING id, name;

-- name: DeleteConfigSourcesNotInList :exec
-- Delete CONFIG sources not in the provided list (for config file sync)
DELETE FROM source
WHERE id NOT IN (SELECT unnest(sqlc.arg(ids)::uuid[]))
  AND creation_type = 'CONFIG';

-- name: DeleteSource :execrows
-- Delete a source by name. Go callers guard against deleting wrong creation_type.
DELETE FROM source
WHERE name = sqlc.arg(name);

-- ============================================================================
-- Source Queries (unified, creation_type guards are in Go)
-- ============================================================================

-- name: InsertSource :one
-- Insert a new source with full configuration. creation_type is passed as a parameter.
INSERT INTO source (
    name,
    creation_type,
    source_type,
    source_config,
    filter_config,
    sync_schedule,
    syncable,
    claims,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(name),
    sqlc.arg(creation_type),
    sqlc.arg(source_type),
    sqlc.narg(source_config),
    sqlc.narg(filter_config),
    sqlc.narg(sync_schedule),
    sqlc.arg(syncable),
    sqlc.narg(claims),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
) RETURNING *;

-- name: UpdateSource :one
-- Update an existing source. Go callers guard against modifying wrong creation_type.
UPDATE source SET
    source_type = sqlc.arg(source_type),
    source_config = sqlc.narg(source_config),
    filter_config = sqlc.narg(filter_config),
    sync_schedule = sqlc.narg(sync_schedule),
    syncable = sqlc.arg(syncable),
    claims = sqlc.narg(claims),
    updated_at = sqlc.arg(updated_at)
WHERE name = sqlc.arg(name)
RETURNING *;

-- name: ListAllSourceNames :many
SELECT name FROM source ORDER BY name;

-- name: GetAPISourcesByNames :many
SELECT id,
       name,
       creation_type,
       source_type,
       source_config,
       filter_config,
       sync_schedule,
       claims,
       created_at,
       updated_at
FROM source
WHERE name = ANY(sqlc.arg(names)::text[])
  AND creation_type = 'API';

-- name: GetManagedSources :many
SELECT id, name, source_type, creation_type, claims, created_at, updated_at
FROM source WHERE source_type = 'managed';

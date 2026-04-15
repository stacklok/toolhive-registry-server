-- name: GetSourceSync :one
SELECT id,
       source_id,
       sync_status,
       error_msg,
       started_at,
       ended_at,
       attempt_count,
       last_sync_hash,
       last_applied_filter_hash,
       server_count,
       skill_count
FROM registry_sync
WHERE id = sqlc.arg(id);

-- name: InsertSourceSync :one
INSERT INTO registry_sync (
    source_id,
    sync_status,
    error_msg,
    started_at
) VALUES (
    sqlc.arg(source_id),
    sqlc.arg(sync_status),
    sqlc.narg(error_msg),
    sqlc.arg(started_at)
) RETURNING id;

-- name: UpdateSourceSync :exec
UPDATE registry_sync SET
    sync_status = sqlc.arg(sync_status),
    error_msg = sqlc.narg(error_msg),
    ended_at = sqlc.arg(ended_at)
WHERE id = sqlc.arg(id);

-- name: GetSourceSyncByName :one
SELECT rs.id,
       rs.source_id,
       rs.sync_status,
       rs.error_msg,
       rs.started_at,
       rs.ended_at,
       rs.attempt_count,
       rs.last_sync_hash,
       rs.last_applied_filter_hash,
       rs.server_count,
       rs.skill_count
FROM registry_sync rs
INNER JOIN source s ON rs.source_id = s.id
WHERE s.name = sqlc.arg(name);

-- name: ListSourceSyncs :many
SELECT s.name,
       rs.id,
       rs.source_id,
       rs.sync_status,
       rs.error_msg,
       rs.started_at,
       rs.ended_at,
       rs.attempt_count,
       rs.last_sync_hash,
       rs.last_applied_filter_hash,
       rs.server_count,
       rs.skill_count,
       s.sync_schedule::interval AS sync_schedule
FROM registry_sync rs
INNER JOIN source s ON rs.source_id = s.id
ORDER BY rs.started_at DESC, s.name ASC;

-- name: UpsertSourceSyncByName :exec
INSERT INTO registry_sync (
    source_id,
    sync_status,
    error_msg,
    started_at,
    ended_at,
    attempt_count,
    last_sync_hash,
    last_applied_filter_hash,
    server_count,
    skill_count
) VALUES (
    (SELECT id FROM source WHERE name = sqlc.arg(name)),
    sqlc.arg(sync_status),
    sqlc.narg(error_msg),
    sqlc.narg(started_at),
    sqlc.arg(ended_at),
    sqlc.arg(attempt_count),
    sqlc.narg(last_sync_hash),
    sqlc.narg(last_applied_filter_hash),
    sqlc.arg(server_count),
    sqlc.arg(skill_count)
)
ON CONFLICT (source_id) DO UPDATE SET
    sync_status = EXCLUDED.sync_status,
    error_msg = EXCLUDED.error_msg,
    started_at = EXCLUDED.started_at,
    ended_at = EXCLUDED.ended_at,
    attempt_count = EXCLUDED.attempt_count,
    last_sync_hash = EXCLUDED.last_sync_hash,
    last_applied_filter_hash = EXCLUDED.last_applied_filter_hash,
    server_count = EXCLUDED.server_count,
    skill_count = EXCLUDED.skill_count;

-- name: InitializeSourceSync :exec
INSERT INTO registry_sync (
    source_id,
    sync_status,
    error_msg
) VALUES (
    (SELECT id FROM source WHERE name = sqlc.arg(name)),
    sqlc.arg(sync_status),
    sqlc.arg(error_msg)
)
ON CONFLICT (source_id) DO NOTHING;

-- name: BulkInitializeSourceSyncs :exec
INSERT INTO registry_sync (
    source_id,
    sync_status,
    error_msg
)
SELECT
    unnest(sqlc.arg(source_ids)::uuid[]),
    unnest(sqlc.arg(sync_statuses)::sync_status[]),
    unnest(sqlc.arg(error_msgs)::text[])
ON CONFLICT (source_id) DO NOTHING;

-- name: ListSourceSyncsByLastUpdate :many
SELECT s.name,
       rs.id,
       rs.source_id,
       rs.sync_status,
       rs.error_msg,
       rs.started_at,
       rs.ended_at,
       rs.attempt_count,
       rs.last_sync_hash,
       rs.last_applied_filter_hash,
       rs.server_count,
       rs.skill_count,
       s.sync_schedule::interval AS sync_schedule
FROM registry_sync rs
INNER JOIN source s ON rs.source_id = s.id
WHERE s.syncable = true
ORDER BY rs.ended_at ASC NULLS FIRST, s.name ASC
FOR UPDATE OF rs SKIP LOCKED;

-- name: UpdateSourceSyncStatusByName :exec
UPDATE registry_sync
SET sync_status = sqlc.arg(sync_status),
    started_at = sqlc.arg(started_at)
WHERE source_id = (SELECT id FROM source WHERE name = sqlc.arg(name));

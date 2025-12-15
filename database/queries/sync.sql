-- name: GetRegistrySync :one
SELECT id,
       reg_id,
       sync_status,
       error_msg,
       started_at,
       ended_at,
       attempt_count,
       last_sync_hash,
       last_applied_filter_hash,
       server_count
FROM registry_sync
WHERE id = sqlc.arg(id);

-- name: InsertRegistrySync :one
INSERT INTO registry_sync (
    reg_id,
    sync_status,
    error_msg,
    started_at
) VALUES (
    sqlc.arg(reg_id),
    sqlc.arg(sync_status),
    sqlc.narg(error_msg),
    sqlc.arg(started_at)
) RETURNING id;

-- name: UpdateRegistrySync :exec
UPDATE registry_sync SET
    sync_status = sqlc.arg(sync_status),
    error_msg = sqlc.narg(error_msg),
    ended_at = sqlc.arg(ended_at)
WHERE id = sqlc.arg(id);

-- name: GetRegistrySyncByName :one
SELECT rs.id,
       rs.reg_id,
       rs.sync_status,
       rs.error_msg,
       rs.started_at,
       rs.ended_at,
       rs.attempt_count,
       rs.last_sync_hash,
       rs.last_applied_filter_hash,
       rs.server_count
FROM registry_sync rs
INNER JOIN registry r ON rs.reg_id = r.id
WHERE r.name = sqlc.arg(name);

-- name: ListRegistrySyncs :many
SELECT r.name,
       rs.id,
       rs.reg_id,
       rs.sync_status,
       rs.error_msg,
       rs.started_at,
       rs.ended_at,
       rs.attempt_count,
       rs.last_sync_hash,
       rs.last_applied_filter_hash,
       rs.server_count,
       r.sync_schedule::interval AS sync_schedule
FROM registry_sync rs
INNER JOIN registry r ON rs.reg_id = r.id
ORDER BY rs.started_at DESC, r.name ASC;

-- name: UpsertRegistrySyncByName :exec
INSERT INTO registry_sync (
    reg_id,
    sync_status,
    error_msg,
    started_at,
    ended_at,
    attempt_count,
    last_sync_hash,
    last_applied_filter_hash,
    server_count
) VALUES (
    (SELECT id FROM registry WHERE name = sqlc.arg(name)),
    sqlc.arg(sync_status),
    sqlc.narg(error_msg),
    sqlc.narg(started_at),
    sqlc.arg(ended_at),
    sqlc.arg(attempt_count),
    sqlc.narg(last_sync_hash),
    sqlc.narg(last_applied_filter_hash),
    sqlc.arg(server_count)
)
ON CONFLICT (reg_id) DO UPDATE SET
    sync_status = EXCLUDED.sync_status,
    error_msg = EXCLUDED.error_msg,
    started_at = EXCLUDED.started_at,
    ended_at = EXCLUDED.ended_at,
    attempt_count = EXCLUDED.attempt_count,
    last_sync_hash = EXCLUDED.last_sync_hash,
    last_applied_filter_hash = EXCLUDED.last_applied_filter_hash,
    server_count = EXCLUDED.server_count;

-- name: InitializeRegistrySync :exec
INSERT INTO registry_sync (
    reg_id,
    sync_status,
    error_msg
) VALUES (
    (SELECT id FROM registry WHERE name = sqlc.arg(name)),
    sqlc.arg(sync_status),
    sqlc.arg(error_msg)
)
ON CONFLICT (reg_id) DO NOTHING;

-- name: BulkInitializeRegistrySyncs :exec
INSERT INTO registry_sync (
    reg_id,
    sync_status,
    error_msg
)
SELECT
    unnest(sqlc.arg(reg_ids)::uuid[]),
    unnest(sqlc.arg(sync_statuses)::sync_status[]),
    unnest(sqlc.arg(error_msgs)::text[])
ON CONFLICT (reg_id) DO NOTHING;

-- name: ListRegistrySyncsByLastUpdate :many
SELECT r.name,
       rs.id,
       rs.reg_id,
       rs.sync_status,
       rs.error_msg,
       rs.started_at,
       rs.ended_at,
       rs.attempt_count,
       rs.last_sync_hash,
       rs.last_applied_filter_hash,
       rs.server_count,
       r.sync_schedule::interval AS sync_schedule
FROM registry_sync rs
INNER JOIN registry r ON rs.reg_id = r.id
ORDER BY rs.ended_at ASC NULLS FIRST, r.name ASC
FOR UPDATE OF rs SKIP LOCKED;

-- name: UpdateRegistrySyncStatusByName :exec
UPDATE registry_sync
SET sync_status = sqlc.arg(sync_status),
    started_at = sqlc.arg(started_at)
WHERE reg_id = (SELECT id FROM registry WHERE name = sqlc.arg(name));

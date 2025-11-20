-- name: GetRegistrySync :one
SELECT id,
       reg_id,
       sync_status,
       error_msg,
       started_at,
       ended_at
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

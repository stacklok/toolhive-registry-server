-- name: ListRegistries :many
SELECT id,
       name,
       reg_type,
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
       created_at,
       updated_at
  FROM registry
 WHERE id = sqlc.arg(id);

-- name: GetRegistryByName :one
SELECT id,
       name,
       reg_type,
       created_at,
       updated_at
  FROM registry
 WHERE name = sqlc.arg(name);

-- name: InsertRegistry :one
INSERT INTO registry (
    name,
    reg_type,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(name),
    sqlc.arg(reg_type),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
) RETURNING id;

-- name: UpsertRegistry :one
INSERT INTO registry (
    name,
    reg_type,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(name),
    sqlc.arg(reg_type),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
) ON CONFLICT (name) DO UPDATE SET
    reg_type = EXCLUDED.reg_type,
    updated_at = EXCLUDED.updated_at
RETURNING id, name, reg_type, created_at, updated_at;

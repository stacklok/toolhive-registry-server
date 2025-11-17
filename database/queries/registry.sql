-- name: ListRegistries :many
SELECT id,
       name,
       reg_type,
       created_at,
       updated_at
  FROM registry
 WHERE (sqlc.narg(next)::timestamp with time zone IS NULL OR created_at > sqlc.narg(next))
    OR (sqlc.narg(prev)::timestamp with time zone IS NULL AND created_at < sqlc.narg(prev))
 ORDER BY
  -- next page sorting
  CASE WHEN sqlc.narg(next)::timestamp with time zone IS NULL THEN created_at END ASC,
  -- previous page sorting
  CASE WHEN sqlc.narg(prev)::timestamp with time zone IS NULL THEN created_at END DESC
 LIMIT sqlc.arg(size)::bigint;

-- name: GetRegistry :one
SELECT id,
       name,
       reg_type,
       created_at,
       updated_at
  FROM registry
 WHERE id = sqlc.arg(id);

-- name: InsertRegistry :one
INSERT INTO registry (name, reg_type) VALUES ($1, $2) RETURNING id;

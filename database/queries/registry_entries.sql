-- name: InsertRegistryEntry :one
INSERT INTO registry_entry (
    reg_id,
    entry_type,
    name,
    title,
    description,
    version,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(reg_id),
    sqlc.arg(entry_type),
    sqlc.arg(name),
    sqlc.arg(title),
    sqlc.arg(description),
    sqlc.arg(version),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
) RETURNING id;

-- name: DeleteRegistryEntry :execrows
DELETE FROM registry_entry
WHERE reg_id = sqlc.arg(reg_id)
  AND name = sqlc.arg(name)
  AND version = sqlc.arg(version);

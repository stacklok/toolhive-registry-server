-- name: GetRegistryEntryByName :one
SELECT id, claims
  FROM registry_entry
 WHERE source_id = sqlc.arg(source_id)
   AND entry_type = sqlc.arg(entry_type)
   AND name = sqlc.arg(name);

-- name: InsertRegistryEntry :one
INSERT INTO registry_entry (
    source_id,
    entry_type,
    name,
    claims,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(source_id),
    sqlc.arg(entry_type),
    sqlc.arg(name),
    sqlc.arg(claims),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
) RETURNING id;

-- name: DeleteRegistryEntry :execrows
DELETE FROM registry_entry
WHERE source_id = sqlc.arg(source_id)
  AND entry_type = sqlc.arg(entry_type)
  AND name = sqlc.arg(name);

-- name: InsertEntryVersion :one
INSERT INTO entry_version (
    entry_id,
    version,
    title,
    description,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(entry_id),
    sqlc.arg(version),
    sqlc.arg(title),
    sqlc.arg(description),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
) RETURNING id;

-- name: DeleteEntryVersion :execrows
DELETE FROM entry_version
WHERE entry_id = sqlc.arg(entry_id)
  AND version = sqlc.arg(version);

-- name: CountEntryVersions :one
SELECT count(*) FROM entry_version
WHERE entry_id = sqlc.arg(entry_id);

-- name: DeleteRegistryEntryByID :execrows
DELETE FROM registry_entry
WHERE id = sqlc.arg(id);

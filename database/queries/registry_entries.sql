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
    name,
    version,
    title,
    description,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(entry_id),
    sqlc.arg(name),
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

-- name: ListEntryVersions :many
SELECT id, version
  FROM entry_version
 WHERE entry_id = sqlc.arg(entry_id)
 ORDER BY version ASC;

-- name: GetLatestEntryVersion :one
SELECT l.version
  FROM latest_entry_version l
 WHERE l.name = sqlc.arg(name)
   AND l.source_id = sqlc.arg(source_id);

-- name: PropagateSourceClaimsToEntries :exec
-- Update all registry entries for a source to match the source's current claims.
-- Used during initialization to fix drift when source claims change without data change.
UPDATE registry_entry
   SET claims = sqlc.narg(claims),
       updated_at = NOW()
 WHERE source_id = sqlc.arg(source_id)
   AND (claims IS DISTINCT FROM sqlc.narg(claims));

-- name: ListEntriesBySource :many
SELECT e.entry_type,
       e.name,
       e.claims,
       v.version,
       v.title,
       v.description,
       v.created_at,
       v.updated_at
  FROM registry_entry e
  JOIN entry_version v ON v.entry_id = e.id
 WHERE e.source_id = sqlc.arg(source_id)
 ORDER BY v.name ASC, v.version ASC;

-- name: ListEntriesByRegistry :many
SELECT e.entry_type,
       e.name,
       v.version,
       v.title,
       v.description,
       v.created_at,
       v.updated_at,
       src.name AS source_name,
       rs.position
  FROM registry_source rs
  JOIN source src ON rs.source_id = src.id
  JOIN registry_entry e ON e.source_id = rs.source_id
  JOIN entry_version v ON v.entry_id = e.id
 WHERE rs.registry_id = sqlc.arg(registry_id)
 ORDER BY v.name ASC, v.version ASC, rs.position ASC;

-- name: UpdateRegistryEntryClaims :execrows
UPDATE registry_entry
   SET claims = sqlc.narg(claims),
       updated_at = NOW()
 WHERE source_id = sqlc.arg(source_id)
   AND entry_type = sqlc.arg(entry_type)
   AND name = sqlc.arg(name);

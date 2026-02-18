-- name: ListSkills :many
-- Cursor-based pagination using (name, version) compound cursor.
-- The cursor_name and cursor_version parameters define the starting point.
-- When cursor is provided, results start AFTER the specified (name, version) tuple.
SELECT r.reg_type AS registry_type,
       s.entry_id,
       e.name,
       e.version,
       (l.latest_entry_id IS NOT NULL)::boolean AS is_latest,
       e.created_at,
       e.updated_at,
       e.description,
       e.title,
       s.namespace,
       s.status,
       s.license,
       s.compatibility,
       s.allowed_tools,
       s.repository,
       s.icons,
       s.metadata,
       s.extension_meta
  FROM skill s
  JOIN registry_entry e ON s.entry_id = e.id
  JOIN registry r ON e.reg_id = r.id
  LEFT JOIN latest_entry_version l ON e.id = l.latest_entry_id
 WHERE (sqlc.narg(registry_name)::text IS NULL OR r.name = sqlc.narg(registry_name)::text)
   AND (sqlc.narg(namespace)::text IS NULL OR s.namespace = sqlc.narg(namespace)::text)
   AND (sqlc.narg(name)::text IS NULL OR e.name = sqlc.narg(name)::text)
   AND (sqlc.narg(search)::text IS NULL OR (
       LOWER(e.name) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
       OR LOWER(e.title) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
       OR LOWER(e.description) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
   ))
   AND (sqlc.narg(updated_since)::timestamp with time zone IS NULL OR e.updated_at > sqlc.narg(updated_since)::timestamp with time zone)
   AND (
       sqlc.narg(cursor_name)::text IS NULL
       OR (e.name, e.version) > (sqlc.narg(cursor_name)::text, sqlc.narg(cursor_version)::text)
   )
 ORDER BY e.name ASC, e.version ASC
 LIMIT sqlc.arg(size)::bigint;

-- name: GetSkillVersion :one
SELECT r.reg_type AS registry_type,
       e.id,
       e.name,
       e.version,
       (l.latest_entry_id IS NOT NULL)::boolean AS is_latest,
       e.created_at,
       e.updated_at,
       e.description,
       e.title,
       s.entry_id AS skill_entry_id,
       s.namespace,
       s.status,
       s.license,
       s.compatibility,
       s.allowed_tools,
       s.repository,
       s.icons,
       s.metadata,
       s.extension_meta
  FROM skill s
  JOIN registry_entry e ON s.entry_id = e.id
  JOIN registry r ON e.reg_id = r.id
  LEFT JOIN latest_entry_version l ON e.id = l.latest_entry_id
 WHERE e.name = sqlc.arg(name)
   AND (e.version = sqlc.arg(version)::text
       OR (sqlc.arg(version)::text = 'latest' AND l.latest_entry_id = e.id)
   )
   AND (sqlc.narg(registry_name)::text IS NULL OR r.name = sqlc.narg(registry_name)::text);

-- name: ListSkillOciPackages :many
SELECT p.id,
       p.skill_entry_id,
       p.identifier,
       p.digest,
       p.media_type
  FROM skill_oci_package p
  JOIN skill s ON p.skill_entry_id = s.entry_id
 WHERE s.entry_id = ANY(sqlc.slice(entry_ids)::UUID[]);

-- name: ListSkillGitPackages :many
SELECT p.id,
       p.skill_entry_id,
       p.url,
       p.ref,
       p.commit_sha,
       p.subfolder
  FROM skill_git_package p
  JOIN skill s ON p.skill_entry_id = s.entry_id
 WHERE s.entry_id = ANY(sqlc.slice(entry_ids)::UUID[]);

-- name: InsertSkillVersion :one
INSERT INTO skill (
    entry_id,
    namespace,
    status,
    license,
    compatibility,
    allowed_tools,
    repository,
    icons,
    metadata,
    extension_meta
) VALUES (
    sqlc.arg(entry_id),
    sqlc.arg(namespace),
    COALESCE(sqlc.narg(status)::skill_status, 'ACTIVE'),
    sqlc.narg(license),
    sqlc.narg(compatibility),
    sqlc.narg(allowed_tools),
    sqlc.narg(repository),
    sqlc.narg(icons),
    sqlc.narg(metadata),
    sqlc.narg(extension_meta)
)
RETURNING entry_id;

-- name: UpsertLatestSkillVersion :one
INSERT INTO latest_entry_version (
    reg_id,
    name,
    version,
    latest_entry_id
) VALUES (
    sqlc.arg(reg_id),
    sqlc.arg(name),
    sqlc.arg(version),
    sqlc.arg(entry_id)
) ON CONFLICT (reg_id, name)
  DO UPDATE SET
    version = sqlc.arg(version),
    latest_entry_id = sqlc.arg(entry_id)
RETURNING latest_entry_id;

-- name: InsertSkillOciPackage :exec
INSERT INTO skill_oci_package (
    skill_entry_id,
    identifier,
    digest,
    media_type
) VALUES (
    sqlc.arg(skill_entry_id),
    sqlc.arg(identifier),
    sqlc.narg(digest),
    sqlc.narg(media_type)
);

-- name: InsertSkillGitPackage :exec
INSERT INTO skill_git_package (
    skill_entry_id,
    url,
    ref,
    commit_sha,
    subfolder
) VALUES (
    sqlc.arg(skill_entry_id),
    sqlc.arg(url),
    sqlc.narg(ref),
    sqlc.narg(commit_sha),
    sqlc.narg(subfolder)
);

-- name: DeleteSkillsByRegistry :exec
WITH registry_entries AS (
    SELECT e.id
      FROM registry_entry e
      JOIN skill s ON e.id = s.entry_id
     WHERE e.reg_id = sqlc.arg(reg_id)
)
DELETE FROM registry_entry
 WHERE id IN (SELECT id FROM registry_entries);

-- name: InsertSkillVersionForSync :one
INSERT INTO skill (
    entry_id,
    namespace,
    status,
    license,
    compatibility,
    allowed_tools,
    repository,
    icons,
    metadata,
    extension_meta
) VALUES (
    sqlc.arg(entry_id),
    sqlc.arg(namespace),
    COALESCE(sqlc.narg(status)::skill_status, 'ACTIVE'),
    sqlc.narg(license),
    sqlc.narg(compatibility),
    sqlc.narg(allowed_tools),
    sqlc.narg(repository),
    sqlc.narg(icons),
    sqlc.narg(metadata),
    sqlc.narg(extension_meta)
)
RETURNING entry_id;

-- name: UpsertSkillVersionForSync :one
INSERT INTO skill (
    entry_id,
    namespace,
    status,
    license,
    compatibility,
    allowed_tools,
    repository,
    icons,
    metadata,
    extension_meta
) VALUES (
    sqlc.arg(entry_id),
    sqlc.arg(namespace),
    COALESCE(sqlc.narg(status)::skill_status, 'ACTIVE'),
    sqlc.narg(license),
    sqlc.narg(compatibility),
    sqlc.narg(allowed_tools),
    sqlc.narg(repository),
    sqlc.narg(icons),
    sqlc.narg(metadata),
    sqlc.narg(extension_meta)
)
ON CONFLICT (entry_id)
DO UPDATE SET
    status = COALESCE(sqlc.narg(status)::skill_status, skill.status),
    license = sqlc.narg(license),
    compatibility = sqlc.narg(compatibility),
    allowed_tools = sqlc.narg(allowed_tools),
    repository = sqlc.narg(repository),
    icons = sqlc.narg(icons),
    metadata = sqlc.narg(metadata),
    extension_meta = sqlc.narg(extension_meta)
RETURNING entry_id;

-- name: DeleteOrphanedSkills :exec
WITH subset AS (
    SELECT e.id
      FROM registry_entry e
     WHERE reg_id = sqlc.arg(reg_id)
       AND e.id != ALL(sqlc.slice(keep_ids)::UUID[])
)
DELETE FROM skill s
 WHERE s.entry_id IN (SELECT id FROM subset);

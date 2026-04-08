-- name: ListSkills :many
-- Cursor-based pagination using (name, version) compound cursor.
-- The cursor_name and cursor_version parameters define the starting point.
-- When cursor is provided, results start AFTER the specified (name, version) tuple.
-- Returns position from registry_source for source priority ordering.
SELECT src.source_type AS registry_type,
       s.version_id,
       e.name,
       v.version,
       (l.latest_version_id IS NOT NULL)::boolean AS is_latest,
       v.created_at,
       v.updated_at,
       v.description,
       v.title,
       s.namespace,
       s.status,
       s.license,
       s.compatibility,
       s.allowed_tools,
       s.repository,
       s.icons,
       s.metadata,
       s.extension_meta,
       e.claims,
       -- Sources not linked to the requested registry have no position; default to max int16
       -- so they sort after all explicitly positioned sources (lower position = higher priority).
       COALESCE(rs.position, 32767)::integer AS position
  FROM skill s
  JOIN entry_version v ON s.version_id = v.id
  JOIN registry_entry e ON v.entry_id = e.id
  JOIN source src ON e.source_id = src.id
  LEFT JOIN latest_entry_version l ON v.id = l.latest_version_id
  JOIN registry_source rs ON rs.source_id = e.source_id
                          AND rs.registry_id = sqlc.arg(registry_id)::uuid
 WHERE (sqlc.narg(namespace)::text IS NULL OR s.namespace = sqlc.narg(namespace)::text)
   AND (sqlc.narg(name)::text IS NULL OR e.name = sqlc.narg(name)::text)
   AND (sqlc.narg(search)::text IS NULL OR (
       LOWER(e.name) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
       OR LOWER(v.title) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
       OR LOWER(v.description) LIKE LOWER('%' || sqlc.narg(search)::text || '%')
   ))
   AND (sqlc.narg(updated_since)::timestamp with time zone IS NULL OR v.updated_at > sqlc.narg(updated_since)::timestamp with time zone)
   AND (
       sqlc.narg(cursor_name)::text IS NULL
       OR (v.name, v.version) > (sqlc.narg(cursor_name)::text, sqlc.narg(cursor_version)::text)
   )
 ORDER BY v.name ASC, v.version ASC, rs.position ASC
 LIMIT sqlc.arg(size)::bigint;

-- name: GetSkillVersion :many
-- Despite the name, this query returns multiple rows. The actual number of
-- records is bounded by the number of sources that provide the same name and
-- version, which we currently don't expect to be more than a few.
-- Cursor-based pagination using (position, source_id) compound cursor.
-- position is the sort key but may not be unique; source_id is the tiebreaker.
SELECT src.source_type AS registry_type,
       v.id,
       e.name,
       v.version,
       (l.latest_version_id IS NOT NULL)::boolean AS is_latest,
       v.created_at,
       v.updated_at,
       v.description,
       v.title,
       s.version_id AS skill_version_id,
       s.namespace,
       s.status,
       s.license,
       s.compatibility,
       s.allowed_tools,
       s.repository,
       s.icons,
       s.metadata,
       s.extension_meta,
       e.claims,
       rs.source_id,
       -- Sources not linked to the requested registry have no position; default to max int16
       -- so they sort after all explicitly positioned sources (lower position = higher priority).
       COALESCE(rs.position, 32767)::integer AS position
  FROM entry_version v
  JOIN registry_entry e ON e.id = v.entry_id
  JOIN registry_source rs ON rs.source_id = e.source_id
                          AND rs.registry_id = sqlc.arg(registry_id)::uuid
  JOIN source src ON e.source_id = src.id
  JOIN skill s ON s.version_id = v.id
  LEFT JOIN latest_entry_version l ON v.id = l.latest_version_id
 WHERE v.name = sqlc.arg(name)
   AND (v.version = sqlc.arg(version)::text
       OR (sqlc.arg(version)::text = 'latest' AND l.latest_version_id = v.id)
   )
   AND (sqlc.narg(source_name)::text IS NULL OR src.name = sqlc.narg(source_name)::text)
   AND (sqlc.narg(namespace)::text IS NULL OR s.namespace = sqlc.narg(namespace)::text)
   AND (
       sqlc.narg(cursor_position)::integer IS NULL
       OR (rs.position > sqlc.narg(cursor_position)::integer
           AND rs.source_id > sqlc.narg(cursor_source_id)::uuid
       )
   )
 ORDER BY rs.position ASC, rs.source_id ASC
 LIMIT sqlc.arg(size)::bigint;

-- name: GetSkillVersionBySourceName :one
-- Source-scoped variant of GetSkillVersion used by the publish fetch-back path.
-- source_name and version are required; registry filtering is not applied.
SELECT src.source_type AS registry_type,
       v.id,
       e.name,
       v.version,
       (l.latest_version_id IS NOT NULL)::boolean AS is_latest,
       v.created_at,
       v.updated_at,
       v.description,
       v.title,
       s.version_id AS skill_version_id,
       s.namespace,
       s.status,
       s.license,
       s.compatibility,
       s.allowed_tools,
       s.repository,
       s.icons,
       s.metadata,
       s.extension_meta,
       e.claims,
       0::integer AS position
  FROM skill s
  JOIN entry_version v ON s.version_id = v.id
  JOIN registry_entry e ON v.entry_id = e.id
  JOIN source src ON e.source_id = src.id
  LEFT JOIN latest_entry_version l ON v.id = l.latest_version_id
 WHERE v.name = sqlc.arg(name)
   AND v.version = sqlc.arg(version)
   AND src.name = sqlc.arg(source_name);

-- name: ListSkillOciPackages :many
SELECT p.id,
       p.skill_id,
       p.identifier,
       p.digest,
       p.media_type
  FROM skill_oci_package p
  JOIN skill s ON p.skill_id = s.version_id
 WHERE s.version_id = ANY(sqlc.slice(version_ids)::UUID[]);

-- name: ListSkillGitPackages :many
SELECT p.id,
       p.skill_id,
       p.url,
       p.ref,
       p.commit_sha,
       p.subfolder
  FROM skill_git_package p
  JOIN skill s ON p.skill_id = s.version_id
 WHERE s.version_id = ANY(sqlc.slice(version_ids)::UUID[]);

-- name: InsertSkillVersion :one
INSERT INTO skill (
    version_id,
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
    sqlc.arg(version_id),
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
RETURNING version_id;

-- name: UpsertLatestSkillVersion :one
INSERT INTO latest_entry_version (
    source_id,
    name,
    version,
    latest_version_id
) VALUES (
    sqlc.arg(source_id),
    sqlc.arg(name),
    sqlc.arg(version),
    sqlc.arg(version_id)
) ON CONFLICT (source_id, name)
  DO UPDATE SET
    version = sqlc.arg(version),
    latest_version_id = sqlc.arg(version_id)
RETURNING latest_version_id;

-- name: InsertSkillOciPackage :exec
INSERT INTO skill_oci_package (
    skill_id,
    identifier,
    digest,
    media_type
) VALUES (
    sqlc.arg(skill_id),
    sqlc.arg(identifier),
    sqlc.narg(digest),
    sqlc.narg(media_type)
);

-- name: InsertSkillGitPackage :exec
INSERT INTO skill_git_package (
    skill_id,
    url,
    ref,
    commit_sha,
    subfolder
) VALUES (
    sqlc.arg(skill_id),
    sqlc.arg(url),
    sqlc.narg(ref),
    sqlc.narg(commit_sha),
    sqlc.narg(subfolder)
);

-- name: DeleteSkillsByRegistry :exec
WITH skill_entries AS (
    SELECT DISTINCT v.entry_id
      FROM entry_version v
      JOIN registry_entry e ON v.entry_id = e.id
      JOIN skill s ON v.id = s.version_id
     WHERE e.source_id = sqlc.arg(source_id)
)
DELETE FROM registry_entry
 WHERE id IN (SELECT entry_id FROM skill_entries);

-- name: InsertSkillVersionForSync :one
INSERT INTO skill (
    version_id,
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
    sqlc.arg(version_id),
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
RETURNING version_id;

-- name: UpsertSkillVersionForSync :one
INSERT INTO skill (
    version_id,
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
    sqlc.arg(version_id),
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
ON CONFLICT (version_id)
DO UPDATE SET
    status = COALESCE(sqlc.narg(status)::skill_status, skill.status),
    license = sqlc.narg(license),
    compatibility = sqlc.narg(compatibility),
    allowed_tools = sqlc.narg(allowed_tools),
    repository = sqlc.narg(repository),
    icons = sqlc.narg(icons),
    metadata = sqlc.narg(metadata),
    extension_meta = sqlc.narg(extension_meta)
RETURNING version_id;

-- name: DeleteOrphanedSkills :exec
WITH subset AS (
    SELECT v.id
      FROM entry_version v
      JOIN registry_entry e ON v.entry_id = e.id
     WHERE e.source_id = sqlc.arg(source_id)
       AND e.entry_type = 'SKILL'
       AND v.id != ALL(sqlc.slice(keep_ids)::UUID[])
)
DELETE FROM skill s
 WHERE s.version_id IN (SELECT id FROM subset);

-- name: DeleteSkillOciPackagesBySkillId :exec
DELETE FROM skill_oci_package WHERE skill_id = sqlc.arg(skill_id);

-- name: DeleteSkillGitPackagesBySkillId :exec
DELETE FROM skill_git_package WHERE skill_id = sqlc.arg(skill_id);

-- name: EntrySourceExists :one
SELECT EXISTS(SELECT 1 FROM source WHERE id=sqlc.arg(source_id)::uuid);

-- name: EntryManagedSource :one
SELECT id FROM source WHERE source_type='managed' FOR UPDATE;

-- name: EntrySource :one
SELECT id, name, source_type, (claims IS NOT NULL)::boolean AS has_claims
FROM source WHERE id=sqlc.arg(source_id)::uuid FOR UPDATE;

-- name: EntryHasClaims :one
SELECT EXISTS(SELECT 1 FROM registry_entry WHERE source_id=sqlc.arg(source_id)::uuid AND claims IS NOT NULL);

-- name: EntryDeleteEmptyNames :exec
DELETE FROM registry_entry e WHERE e.source_id=sqlc.arg(source_id)::uuid
AND NOT EXISTS (SELECT 1 FROM entry_version v WHERE v.entry_id=e.id);

-- name: EntryVersionExists :one
SELECT EXISTS(SELECT 1 FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
WHERE e.source_id=sqlc.arg(source_id)::uuid AND e.entry_type=sqlc.arg(entry_type)
AND e.name=sqlc.arg(name) AND v.version=sqlc.arg(version));

-- name: EntryDeleteVersion :execrows
DELETE FROM entry_version v WHERE v.id IN (
 SELECT v.id FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
 WHERE e.source_id=sqlc.arg(source_id)::uuid AND e.entry_type=sqlc.arg(entry_type)
 AND e.name=sqlc.arg(name) AND v.version=sqlc.arg(version));

-- name: EntryVersionsForName :many
SELECT v.id, v.version FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
WHERE e.source_id=sqlc.arg(source_id)::uuid AND e.entry_type=sqlc.arg(entry_type) AND e.name=sqlc.arg(name);

-- name: EntryDropLatest :exec
DELETE FROM latest_entry_version WHERE source_id=sqlc.arg(source_id)::uuid
AND entry_type=sqlc.arg(entry_type) AND name=sqlc.arg(name);

-- name: EntryList :many
SELECT v.id::text AS id, e.name, v.version, v.title, v.description,
 v.created_at, v.updated_at, coalesce(l.latest_version_id = v.id, false)::boolean AS is_latest,
 to_jsonb(s) AS server_data, to_jsonb(sk) AS skill_data, to_jsonb(pl) AS plugin_data,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.pkg_identifier,p.transport), '[]'::jsonb)
    FROM mcp_server_package p WHERE p.server_id=v.id)::text AS server_packages,
 (SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.transport,r.transport_url), '[]'::jsonb)
    FROM mcp_server_remote r WHERE r.server_id=v.id)::text AS server_remotes,
 (SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY i.source_uri), '[]'::jsonb)
    FROM mcp_server_icon i WHERE i.server_id=v.id)::text AS server_icons,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.identifier), '[]'::jsonb)
    FROM skill_oci_package p WHERE p.skill_id=v.id)::text AS skill_oci,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.url), '[]'::jsonb)
    FROM skill_git_package p WHERE p.skill_id=v.id)::text AS skill_git,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.identifier), '[]'::jsonb)
    FROM plugin_oci_package p WHERE p.plugin_id=v.id)::text AS plugin_oci,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.url), '[]'::jsonb)
    FROM plugin_git_package p WHERE p.plugin_id=v.id)::text AS plugin_git
FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
LEFT JOIN latest_entry_version l ON l.source_id=e.source_id AND l.entry_type=e.entry_type AND l.name=e.name
LEFT JOIN mcp_server s ON s.version_id=v.id
LEFT JOIN skill sk ON sk.version_id=v.id
LEFT JOIN plugin pl ON pl.version_id=v.id
WHERE e.source_id=sqlc.arg(source_id)::uuid AND e.entry_type=sqlc.arg(entry_type)
AND (sqlc.narg(name)::text IS NULL OR e.name=sqlc.narg(name)::text)
AND (sqlc.narg(version)::text IS NULL OR v.version=sqlc.narg(version)::text)
AND (NOT sqlc.arg(latest_only)::boolean OR l.latest_version_id=v.id)
AND (sqlc.narg(search)::text IS NULL OR strpos(lower(e.name),lower(sqlc.narg(search)::text))>0
 OR strpos(lower(coalesce(v.title,'')),lower(sqlc.narg(search)::text))>0
 OR strpos(lower(coalesce(v.description,'')),lower(sqlc.narg(search)::text))>0)
AND (sqlc.narg(cursor_name)::text IS NULL OR
 (e.name COLLATE "C",v.version COLLATE "C") >
 (sqlc.narg(cursor_name)::text COLLATE "C",sqlc.narg(cursor_version)::text COLLATE "C"))
ORDER BY e.name COLLATE "C",v.version COLLATE "C"
LIMIT sqlc.arg(page_size)::bigint;

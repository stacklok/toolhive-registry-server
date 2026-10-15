-- name: ViewEntrySources :many
SELECT s.id::text AS id FROM registry_source rs JOIN source s ON s.id=rs.source_id
WHERE rs.registry_id=sqlc.arg(view_id)::uuid ORDER BY rs.position,s.name COLLATE "C";

-- name: ViewEntryList :many
WITH ranked_names AS (
 SELECT e.id,e.source_id,e.entry_type,e.name,
 row_number() OVER (PARTITION BY e.entry_type,e.name ORDER BY rs.position,src.name COLLATE "C") AS priority
 FROM registry_source rs JOIN source src ON src.id=rs.source_id
 JOIN registry_entry e ON e.source_id=rs.source_id
 WHERE rs.registry_id=sqlc.arg(view_id)::uuid
 AND EXISTS(SELECT 1 FROM entry_version existing WHERE existing.entry_id=e.id)
), winners AS (
 SELECT id,source_id,entry_type,name FROM ranked_names WHERE priority=1
)
SELECT e.source_id::text AS source_id,v.id::text AS id,e.name,v.version,v.title,v.description,
 v.created_at,v.updated_at,coalesce(l.latest_version_id=v.id,false)::boolean AS is_latest,
 to_jsonb(s) AS server_data,to_jsonb(sk) AS skill_data,to_jsonb(pl) AS plugin_data,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.pkg_identifier,p.transport),'[]'::jsonb)
 FROM mcp_server_package p WHERE p.server_id=v.id)::text AS server_packages,
 (SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.transport,r.transport_url),'[]'::jsonb)
 FROM mcp_server_remote r WHERE r.server_id=v.id)::text AS server_remotes,
 (SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY i.source_uri),'[]'::jsonb)
 FROM mcp_server_icon i WHERE i.server_id=v.id)::text AS server_icons,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.identifier),'[]'::jsonb)
 FROM skill_oci_package p WHERE p.skill_id=v.id)::text AS skill_oci,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.url),'[]'::jsonb)
 FROM skill_git_package p WHERE p.skill_id=v.id)::text AS skill_git,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.identifier),'[]'::jsonb)
 FROM plugin_oci_package p WHERE p.plugin_id=v.id)::text AS plugin_oci,
 (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.url),'[]'::jsonb)
 FROM plugin_git_package p WHERE p.plugin_id=v.id)::text AS plugin_git
FROM winners e JOIN entry_version v ON v.entry_id=e.id
LEFT JOIN latest_entry_version l ON l.source_id=e.source_id AND l.entry_type=e.entry_type AND l.name=e.name
LEFT JOIN mcp_server s ON s.version_id=v.id
LEFT JOIN skill sk ON sk.version_id=v.id
LEFT JOIN plugin pl ON pl.version_id=v.id
WHERE e.entry_type=sqlc.arg(entry_type)
AND (sqlc.narg(name)::text IS NULL OR e.name=sqlc.narg(name)::text)
AND (sqlc.narg(namespace)::text IS NULL OR sk.namespace=sqlc.narg(namespace)::text OR pl.namespace=sqlc.narg(namespace)::text)
AND (sqlc.narg(version)::text IS NULL OR v.version=sqlc.narg(version)::text)
AND (NOT sqlc.arg(latest_only)::boolean OR l.latest_version_id=v.id)
AND (sqlc.narg(updated_since)::timestamptz IS NULL OR v.updated_at>sqlc.narg(updated_since)::timestamptz)
AND (sqlc.narg(search)::text IS NULL OR strpos(lower(e.name),lower(sqlc.narg(search)::text))>0
 OR strpos(lower(coalesce(v.title,'')),lower(sqlc.narg(search)::text))>0
 OR strpos(lower(coalesce(v.description,'')),lower(sqlc.narg(search)::text))>0)
AND (sqlc.narg(cursor_name)::text IS NULL OR (e.name COLLATE "C",v.version COLLATE "C")>
 (sqlc.narg(cursor_name)::text COLLATE "C",sqlc.narg(cursor_version)::text COLLATE "C"))
ORDER BY e.name COLLATE "C",v.version COLLATE "C"
LIMIT sqlc.arg(page_size)::bigint;

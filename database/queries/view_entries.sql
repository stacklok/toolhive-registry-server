-- name: ViewEntryList :many
-- Walks entries of one kind in name order, then their versions, and keeps a
-- version only when its entry wins the name: no entry with a version in a
-- higher-priority source of the view (position, then source name) has the same
-- kind and name and, for skills and plugins, a version in the same namespace,
-- so io.a/x and io.b/x from different sources are both reachable. Winners are
-- chosen before the version, latest, search and time filters, so a filter never
-- promotes a lower-priority source. Names and versions sort and compare in the
-- database's default collation, as v1.5.4's ORDER BY name, version did, so
-- v1.5.4 cursors keep their position; namespace is the last sort key, so equal
-- name/version pairs page deterministically. A cursor without a namespace
-- (servers, and v1.5.4 cursors) resumes after its whole name/version. Search is v1.5.4's
-- LOWER(col) LIKE LOWER('%'||search||'%'): % and _ are wildcards and a
-- backslash escapes the next character. The walk follows
-- registry_entry_type_name_idx and stops at the page size; the redundant
-- e.name >= cursor bound lets the index scan start at the cursor.
-- caller is NULL for the library. The server's claims mode passes the caller's
-- JWT claims; an entry the caller cannot see then neither appears nor hides a
-- lower-priority source's entry of the same name (claims_visible, migration
-- 28). This is the one sanctioned JSONB claims check in SQL.
WITH page AS (
 SELECT v.id
 FROM registry_entry e
 JOIN registry_source rs ON rs.source_id=e.source_id AND rs.registry_id=sqlc.arg(view_id)::uuid
 JOIN source src ON src.id=e.source_id
 JOIN entry_version v ON v.entry_id=e.id
 LEFT JOIN skill sk ON sk.version_id=v.id
 LEFT JOIN plugin pl ON pl.version_id=v.id
 WHERE e.entry_type=sqlc.arg(entry_type)
 AND NOT EXISTS (
  SELECT 1 FROM registry_entry hi
  JOIN registry_source hrs ON hrs.source_id=hi.source_id AND hrs.registry_id=sqlc.arg(view_id)::uuid
  JOIN source hsrc ON hsrc.id=hi.source_id
  WHERE hi.entry_type=e.entry_type AND hi.name=e.name AND hi.id<>e.id
  AND (hrs.position<rs.position
   OR (hrs.position=rs.position AND hsrc.name COLLATE "C"<src.name COLLATE "C"))
  AND EXISTS(SELECT 1 FROM entry_version hv
   LEFT JOIN skill hsk ON hsk.version_id=hv.id LEFT JOIN plugin hpl ON hpl.version_id=hv.id
   WHERE hv.entry_id=hi.id
   AND coalesce(hsk.namespace,hpl.namespace,'')=coalesce(sk.namespace,pl.namespace,''))
  AND (sqlc.narg(caller)::jsonb IS NULL OR claims_visible(sqlc.narg(caller)::jsonb,hi.claims)))
 AND (sqlc.narg(caller)::jsonb IS NULL OR claims_visible(sqlc.narg(caller)::jsonb,e.claims))
 AND (sqlc.narg(name)::text IS NULL OR e.name=sqlc.narg(name)::text)
 AND (sqlc.narg(namespace)::text IS NULL OR sk.namespace=sqlc.narg(namespace)::text OR pl.namespace=sqlc.narg(namespace)::text)
 AND (sqlc.narg(version)::text IS NULL OR v.version=sqlc.narg(version)::text)
 AND (NOT sqlc.arg(latest_only)::boolean OR EXISTS(
  SELECT 1 FROM latest_entry_version lv WHERE lv.latest_version_id=v.id))
 AND (sqlc.narg(updated_since)::timestamptz IS NULL OR v.updated_at>sqlc.narg(updated_since)::timestamptz)
 AND (sqlc.narg(search)::text IS NULL
  OR LOWER(e.name) LIKE LOWER('%'||sqlc.narg(search)::text||'%')
  OR LOWER(v.title) LIKE LOWER('%'||sqlc.narg(search)::text||'%')
  OR LOWER(v.description) LIKE LOWER('%'||sqlc.narg(search)::text||'%'))
 AND (sqlc.narg(cursor_name)::text IS NULL OR e.name>=sqlc.narg(cursor_name)::text)
 AND (sqlc.narg(cursor_name)::text IS NULL
  OR (sqlc.narg(cursor_namespace)::text IS NULL
   AND (e.name,v.version)>(sqlc.narg(cursor_name)::text,sqlc.narg(cursor_version)::text))
  OR (e.name,v.version,coalesce(sk.namespace,pl.namespace,''))>
  (sqlc.narg(cursor_name)::text,sqlc.narg(cursor_version)::text,sqlc.narg(cursor_namespace)::text))
 ORDER BY e.name,v.version,coalesce(sk.namespace,pl.namespace,'')
 LIMIT sqlc.arg(page_size)::bigint
)
SELECT e.source_id::text AS source_id,v.id::text AS id,e.name,v.version,v.title,v.description,
 coalesce(sk.namespace,pl.namespace,'')::text AS namespace,
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
FROM page JOIN entry_version v ON v.id=page.id
JOIN registry_entry e ON e.id=v.entry_id
LEFT JOIN latest_entry_version l ON l.source_id=e.source_id AND l.entry_type=e.entry_type AND l.name=e.name
LEFT JOIN mcp_server s ON s.version_id=v.id
LEFT JOIN skill sk ON sk.version_id=v.id
LEFT JOIN plugin pl ON pl.version_id=v.id
ORDER BY e.name,v.version,coalesce(sk.namespace,pl.namespace,'');

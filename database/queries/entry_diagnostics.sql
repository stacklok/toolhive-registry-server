-- Lightweight admin projections deliberately omit claims, payloads and winner selection.
-- name: DiagnosticSourceID :one
SELECT id FROM source WHERE name = $1;

-- name: DiagnosticSourceVersions :many
SELECT e.entry_type, e.name, v.version, v.title, v.description, v.created_at, v.updated_at
FROM registry_entry e
JOIN entry_version v ON v.entry_id = e.id
WHERE e.source_id = $1
ORDER BY e.name COLLATE "C", v.version COLLATE "C", e.entry_type::text COLLATE "C";

-- name: DiagnosticViewVersions :many
SELECT e.entry_type, e.name, v.version, v.title, v.description, v.created_at, v.updated_at,
       src.name AS source_name, src.id::text AS source_id, rs.position
FROM registry_source rs
JOIN source src ON src.id = rs.source_id
JOIN registry_entry e ON e.source_id = src.id
JOIN entry_version v ON v.entry_id = e.id
WHERE rs.registry_id = $1
ORDER BY e.name COLLATE "C", v.version COLLATE "C", e.entry_type::text COLLATE "C",
         rs.position, src.name COLLATE "C";

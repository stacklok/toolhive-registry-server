-- name: RepairListVersions :many
SELECT e.source_id, e.entry_type, e.name, v.version, v.id AS version_id
FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
ORDER BY e.source_id, e.entry_type, e.name COLLATE "C", v.version COLLATE "C";

-- name: RepairSetLatest :exec
INSERT INTO latest_entry_version(source_id,entry_type,name,version,latest_version_id)
VALUES(sqlc.arg(source_id),sqlc.arg(entry_type),sqlc.arg(name),sqlc.arg(version),sqlc.arg(version_id))
ON CONFLICT(source_id,entry_type,name) DO UPDATE SET
version=excluded.version,latest_version_id=excluded.latest_version_id;

-- name: RepairPruneLatest :exec
DELETE FROM latest_entry_version l WHERE NOT EXISTS (
 SELECT 1 FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
 WHERE e.source_id=l.source_id AND e.entry_type=l.entry_type AND e.name=l.name);

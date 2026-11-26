-- name: RepairListVersions :many
SELECT e.source_id, e.entry_type, e.name, v.version, v.id AS version_id
FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
ORDER BY e.source_id, e.entry_type, e.name COLLATE "C", v.version COLLATE "C";

-- name: RepairSetLatest :exec
INSERT INTO latest_entry_version(source_id,entry_type,name,version,latest_version_id)
VALUES(sqlc.arg(source_id),sqlc.arg(entry_type),sqlc.arg(name),sqlc.arg(version),sqlc.arg(version_id))
ON CONFLICT(source_id,entry_type,name) DO UPDATE SET
version=excluded.version,latest_version_id=excluded.latest_version_id;

-- name: RepairCreateTempLatest :exec
-- The winners computed in Go are copied here, then applied in one statement.
-- entry_type is text so COPY needs no enum codec on the connection.
CREATE TEMP TABLE temp_latest_repair (
    source_id UUID NOT NULL,
    entry_type TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    version_id UUID NOT NULL
) ON COMMIT DROP;

-- name: RepairUpsertLatestFromTemp :exec
INSERT INTO latest_entry_version(source_id,entry_type,name,version,latest_version_id)
SELECT source_id, entry_type::entry_type, name, version, version_id FROM temp_latest_repair
ON CONFLICT(source_id,entry_type,name) DO UPDATE SET
version=excluded.version,latest_version_id=excluded.latest_version_id
WHERE (latest_entry_version.version, latest_entry_version.latest_version_id)
 IS DISTINCT FROM (excluded.version, excluded.latest_version_id);

-- name: RepairPruneLatest :exec
DELETE FROM latest_entry_version l WHERE NOT EXISTS (
 SELECT 1 FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
 WHERE e.source_id=l.source_id AND e.entry_type=l.entry_type AND e.name=l.name);

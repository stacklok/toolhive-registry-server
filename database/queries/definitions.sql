-- name: DefListSources :many
SELECT id::text AS id, name, creation_type::text AS origin, source_type, source_config, filter_config,
 COALESCE((extract(epoch from sync_schedule)*1000000)::bigint,0) AS micros
FROM source ORDER BY name COLLATE "C";

-- name: DefGetSource :one
SELECT id::text AS id, name, creation_type::text AS origin, source_type, source_config, filter_config,
 COALESCE((extract(epoch from sync_schedule)*1000000)::bigint,0) AS micros
FROM source WHERE name=$1;

-- name: DefLockSource :one
SELECT id::text AS id, name, creation_type::text AS origin, source_type, source_config, filter_config,
 COALESCE((extract(epoch from sync_schedule)*1000000)::bigint,0) AS micros
FROM source WHERE name=$1 FOR UPDATE;

-- name: DefInsertSource :one
INSERT INTO source(name,creation_type,source_type,source_config,filter_config,sync_schedule,syncable)
VALUES($1,'API',$2,$3::jsonb,$4::jsonb,$5::interval,$6) RETURNING id::text;

-- name: DefUpdateSource :exec
UPDATE source SET source_config=$2::jsonb,filter_config=$3::jsonb,sync_schedule=$4::interval,syncable=$5,updated_at=now()
WHERE name=$1 AND creation_type='API';

-- name: DefSourceClaims :one
SELECT (claims IS NOT NULL)::boolean AS has_claims FROM source WHERE name=$1;

-- name: DefSourceRaw :one
SELECT source_config, filter_config FROM source WHERE name=$1;

-- name: DefSourceUsed :one
SELECT EXISTS(SELECT 1 FROM registry_source WHERE source_id=$1::uuid);

-- name: DefDeleteAPISource :exec
DELETE FROM source WHERE id=$1::uuid AND creation_type='API';

-- name: DefInsertAPIView :one
INSERT INTO registry(name,creation_type) VALUES($1,'API') RETURNING id::text;

-- name: DefGetView :one
SELECT name, creation_type::text AS origin FROM registry WHERE name=$1;

-- name: DefLockView :one
SELECT name, creation_type::text AS origin FROM registry WHERE name=$1 FOR UPDATE;

-- name: DefListViewNames :many
SELECT name FROM registry ORDER BY name COLLATE "C";

-- name: DefViewSources :many
SELECT s.name FROM registry_source rs JOIN registry r ON r.id=rs.registry_id JOIN source s ON s.id=rs.source_id
WHERE r.name=$1 ORDER BY rs.position,s.name COLLATE "C";

-- name: DefViewID :one
SELECT id::text FROM registry WHERE name=$1;

-- name: DefViewClaims :one
SELECT (claims IS NOT NULL)::boolean AS has_claims FROM registry WHERE name=$1;

-- name: DefDeleteViewLinks :exec
DELETE FROM registry_source WHERE registry_id=$1::uuid;

-- name: DefLink :execrows
INSERT INTO registry_source(registry_id,source_id,position)
SELECT $1::uuid,id,$3::int FROM source WHERE name=$2;

-- name: DefTouchAPIView :exec
UPDATE registry SET updated_at=now() WHERE id=$1::uuid AND creation_type='API';

-- name: DefDeleteAPIView :exec
DELETE FROM registry WHERE name=$1 AND creation_type='API';

-- name: DefPruneViews :exec
DELETE FROM registry WHERE creation_type='CONFIG' AND NOT (name=ANY($1::text[])) AND claims IS NULL;

-- name: DefBlockedViews :one
SELECT EXISTS(SELECT 1 FROM registry WHERE creation_type='CONFIG' AND NOT (name=ANY($1::text[])) AND claims IS NOT NULL);

-- name: DefDeleteConfigLinks :exec
DELETE FROM registry_source WHERE registry_id IN (SELECT id FROM registry WHERE creation_type='CONFIG');

-- name: DefPruneSources :exec
DELETE FROM source WHERE creation_type='CONFIG' AND NOT (name=ANY($1::text[])) AND claims IS NULL;

-- name: DefBlockedSources :one
SELECT EXISTS(SELECT 1 FROM source WHERE creation_type='CONFIG' AND NOT (name=ANY($1::text[])) AND claims IS NOT NULL);

-- name: DefUpsertConfigSource :execrows
INSERT INTO source(name,creation_type,source_type,source_config,filter_config,sync_schedule,syncable)
VALUES($1,'CONFIG',$2,$3::jsonb,$4::jsonb,$5::interval,$6)
ON CONFLICT(name) DO UPDATE SET source_config=excluded.source_config,filter_config=excluded.filter_config,
sync_schedule=excluded.sync_schedule,syncable=excluded.syncable,updated_at=now()
WHERE source.creation_type='CONFIG' AND source.source_type=excluded.source_type AND source.claims IS NULL;

-- name: DefUpsertConfigView :one
INSERT INTO registry(name,creation_type) VALUES($1,'CONFIG')
ON CONFLICT(name) DO UPDATE SET updated_at=now()
WHERE registry.creation_type='CONFIG' AND registry.claims IS NULL
RETURNING id::text;

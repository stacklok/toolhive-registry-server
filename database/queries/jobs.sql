-- name: JobNext :one
SELECT s.name FROM source s LEFT JOIN registry_sync rs ON rs.source_id=s.id
WHERE s.syncable AND s.source_type IN ('git','api','file') AND s.sync_schedule IS NOT NULL
 AND NOT (s.name = ANY($1::text[]))
 AND (rs.lease_id IS NULL OR rs.lease_expires_at <= clock_timestamp() OR rs.lease_generation <> s.definition_generation)
 AND (rs.ended_at IS NULL OR rs.ended_at + s.sync_schedule <= clock_timestamp()
      OR rs.observed_generation IS DISTINCT FROM s.definition_generation)
ORDER BY rs.ended_at ASC NULLS FIRST, s.name COLLATE "C"
LIMIT 1 FOR UPDATE OF s SKIP LOCKED;

-- name: JobLockSource :one
SELECT id, definition_generation FROM source WHERE name=$1 FOR UPDATE;

-- name: JobAcquire :one
INSERT INTO registry_sync(source_id,sync_status,error_msg,started_at,ended_at,attempt_count,
 lease_id,lease_expires_at,lease_generation,observed_generation)
VALUES($1::uuid,'IN_PROGRESS',NULL,clock_timestamp(),NULL,1,gen_random_uuid(),
 clock_timestamp()+($3::bigint * interval '1 microsecond'),$2::bigint,$2::bigint)
ON CONFLICT(source_id) DO UPDATE SET
 sync_status='IN_PROGRESS',error_msg=NULL,started_at=clock_timestamp(),ended_at=NULL,
 attempt_count=registry_sync.attempt_count+1,
 lease_id=gen_random_uuid(),lease_expires_at=clock_timestamp()+($3::bigint * interval '1 microsecond'),
 lease_generation=$2::bigint,observed_generation=$2::bigint
WHERE registry_sync.lease_id IS NULL OR registry_sync.lease_expires_at <= clock_timestamp()
 OR registry_sync.lease_generation <> $2::bigint
RETURNING lease_id, lease_expires_at;

-- name: JobRenew :one
UPDATE registry_sync SET lease_expires_at=clock_timestamp()+($4::bigint * interval '1 microsecond')
WHERE source_id=$1::uuid AND lease_id=$2::uuid AND lease_generation=$3::bigint
 AND sync_status='IN_PROGRESS' AND lease_expires_at>clock_timestamp()
 AND lease_generation=(SELECT definition_generation FROM source WHERE id=$1::uuid)
RETURNING lease_expires_at;

-- name: JobFence :one
SELECT rs.source_id FROM registry_sync rs JOIN source s ON s.id=rs.source_id
WHERE rs.source_id=$1::uuid AND rs.lease_id=$2::uuid AND rs.lease_generation=$3::bigint
 AND rs.sync_status='IN_PROGRESS' AND rs.lease_expires_at>clock_timestamp()
 AND s.definition_generation=$3::bigint AND s.source_type <> 'managed'
FOR UPDATE OF s,rs;

-- name: JobFinish :execrows
UPDATE registry_sync SET sync_status='COMPLETED',error_msg=NULL,ended_at=clock_timestamp(),
 lease_id=NULL,lease_expires_at=NULL,lease_generation=NULL,
 last_sync_hash=$4,last_applied_filter_hash=$5,server_count=$6,skill_count=$7,plugin_count=$8,
 applied_generation=$3::bigint
WHERE source_id=$1::uuid AND lease_id=$2::uuid AND lease_generation=$3::bigint
 AND lease_expires_at>clock_timestamp() AND sync_status='IN_PROGRESS'
 AND lease_generation=(SELECT definition_generation FROM source WHERE id=$1::uuid);

-- name: JobFinishUnchanged :execrows
UPDATE registry_sync SET sync_status='COMPLETED',error_msg=NULL,ended_at=clock_timestamp(),
 lease_id=NULL,lease_expires_at=NULL,lease_generation=NULL
WHERE source_id=$1::uuid AND lease_id=$2::uuid AND lease_generation=$3::bigint
 AND lease_expires_at>clock_timestamp() AND sync_status='IN_PROGRESS'
 AND lease_generation=(SELECT definition_generation FROM source WHERE id=$1::uuid)
 AND applied_generation=$3::bigint AND last_sync_hash=$4::text
 AND last_applied_filter_hash=$5::text;

-- name: JobFail :execrows
UPDATE registry_sync SET sync_status='FAILED',error_msg=$4,ended_at=clock_timestamp(),
 lease_id=NULL,lease_expires_at=NULL,lease_generation=NULL
WHERE source_id=$1::uuid AND lease_id=$2::uuid AND lease_generation=$3::bigint
 AND lease_expires_at>clock_timestamp() AND sync_status='IN_PROGRESS'
 AND lease_generation=(SELECT definition_generation FROM source WHERE id=$1::uuid);

-- name: JobStatus :one
SELECT s.id::text AS source_id, COALESCE(rs.sync_status::text,'PENDING')::text AS phase, rs.attempt_count, rs.started_at,
 rs.ended_at, rs.lease_expires_at, rs.last_sync_hash,rs.last_applied_filter_hash,
 rs.server_count,rs.skill_count,rs.plugin_count,rs.error_msg,
 (rs.applied_generation=s.definition_generation AND rs.last_sync_hash IS NOT NULL
  AND rs.last_applied_filter_hash IS NOT NULL) AS baseline_valid
FROM source s LEFT JOIN registry_sync rs ON rs.source_id=s.id WHERE s.name=$1;

-- name: JobSnapshotAllowed :one
SELECT NOT EXISTS(SELECT 1 FROM registry_sync WHERE source_id=$1::uuid
 AND lease_id IS NOT NULL AND lease_expires_at>clock_timestamp()
 AND lease_generation=(SELECT definition_generation FROM source WHERE id=$1::uuid));

-- name: JobInvalidateSnapshot :exec
UPDATE source SET definition_generation=definition_generation+1 WHERE id=$1::uuid;

-- name: JobClearSnapshotBaseline :exec
UPDATE registry_sync SET applied_generation=NULL,last_sync_hash=NULL,last_applied_filter_hash=NULL,
 server_count=0,skill_count=0,plugin_count=0 WHERE source_id=$1::uuid;

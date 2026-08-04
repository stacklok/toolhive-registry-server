-- Migration to preserve full metadata for package and runtime arguments.
-- Changes TEXT[] columns to JSONB so complete model.Argument objects are stored.
--
-- This is the same class of bug fixed for env_vars and transport_headers in
-- migration 000009: a list of objects was typed as a list of scalars, so every
-- field except the flag name was discarded on write.
--
-- Backfill is deliberately conservative. The pre-migration column only ever held
-- flag names, so value/default/valueHint/format are unrecoverable here. We restore
-- the names and nothing else -- notably NOT `type`, which was never stored and
-- would be a guess. Positional arguments were stored as empty strings (they have
-- no name), carrying zero information, so those elements are dropped rather than
-- resurrected as `{"name": ""}`.
--
-- Synced sources do NOT recover on their own: `HasDataChanged` compares the
-- upstream content hash against `registry_sync.last_sync_hash` and skips the write
-- entirely when the source content is unchanged. Since a schema migration does not
-- change upstream content, every synced source would keep serving the name-only
-- backfill indefinitely. Clearing the stored hashes below forces a full re-ingest
-- on the next sync cycle, which restores complete argument metadata.
--
-- Entries published through the API into a managed source have no upstream to
-- re-read. Recovering those requires deleting the affected version and publishing
-- it again, because re-publishing the same name@version returns
-- `ErrVersionAlreadyExists` (HTTP 409):
--
--   DELETE /v1/entries/server/<url-encoded-name>/versions/<version>   -> 204
--   POST   /v1/entries                                                -> 201
--
-- The version is absent from the registry between those two calls.
--
-- !! THIS UPGRADE IS NOT SAFE UNDER A ROLLING DEPLOYMENT !!
--
-- Changing the column type in place is incompatible with binaries built before
-- this migration, which expect `[]string` for these columns. During any window
-- where old and new replicas run together against the migrated schema:
--
--   * An old replica reading a migrated row fails to scan JSONB objects into
--     []string, so its package-list queries error out.
--   * An old replica writing arguments stores a bare JSON string array. Newer
--     readers salvage the flag names (see DeserializeArguments) but the values are
--     gone, and if that write also restores `last_sync_hash` the source will look
--     unchanged and stay degraded until its upstream content changes.
--
-- Scale the deployment to zero (or set `strategy: Recreate`) before upgrading, then
-- scale back up. See "Upgrading across migration 000023" in
-- docs/deployment-kubernetes.md. Note that the HA example in that document
-- specifies `maxUnavailable: 0`, which guarantees the unsafe overlap, so it must
-- not be used for this one upgrade.

-- Helper function: ALTER COLUMN ... USING does not permit subqueries, so the
-- reshaping logic is wrapped in an IMMUTABLE function and dropped afterwards.
CREATE FUNCTION arguments_text_to_jsonb(text[]) RETURNS jsonb AS $$
  SELECT CASE
    WHEN $1 IS NULL THEN NULL
    ELSE COALESCE(
      (SELECT jsonb_agg(jsonb_build_object('name', elem))
         FROM unnest($1) AS elem
        WHERE elem <> ''),
      '[]'::jsonb)
  END
$$ LANGUAGE sql IMMUTABLE;

ALTER TABLE mcp_server_package
    ALTER COLUMN runtime_arguments TYPE JSONB
        USING arguments_text_to_jsonb(runtime_arguments),
    ALTER COLUMN package_arguments TYPE JSONB
        USING arguments_text_to_jsonb(package_arguments);

DROP FUNCTION arguments_text_to_jsonb(text[]);

-- Force a full re-ingest of every synced source on the next sync cycle so the
-- name-only backfill above is replaced with complete argument metadata. A NULL
-- hash makes HasDataChanged return true regardless of upstream content.
-- Non-synced sources (managed, kubernetes) have no stored hash and are unaffected.
UPDATE registry_sync SET last_sync_hash = NULL;

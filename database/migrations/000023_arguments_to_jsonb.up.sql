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
-- re-read and must be re-published to regain argument values.

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

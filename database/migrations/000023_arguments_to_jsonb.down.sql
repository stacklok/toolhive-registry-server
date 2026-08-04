-- Rollback migration: convert JSONB back to TEXT[] (preserves only names).
--
-- This is lossy in exactly the way the pre-000023 code already was, so rolling
-- back restores the previous behaviour rather than degrading past it. Positional
-- arguments have no name and become empty strings again.

CREATE FUNCTION arguments_jsonb_to_text(jsonb) RETURNS text[] AS $$
  SELECT CASE
    WHEN $1 IS NULL THEN NULL
    ELSE COALESCE(
      (SELECT array_agg(COALESCE(elem->>'name', ''))
         FROM jsonb_array_elements($1) AS elem),
      '{}'::text[])
  END
$$ LANGUAGE sql IMMUTABLE;

ALTER TABLE mcp_server_package
    ALTER COLUMN runtime_arguments TYPE TEXT[]
        USING arguments_jsonb_to_text(runtime_arguments),
    ALTER COLUMN package_arguments TYPE TEXT[]
        USING arguments_jsonb_to_text(package_arguments);

DROP FUNCTION arguments_jsonb_to_text(jsonb);

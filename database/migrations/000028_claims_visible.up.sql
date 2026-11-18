-- claims_visible(caller, record) reports whether a caller whose JWT claims are
-- caller may see a registry entry whose stored claims are record. It mirrors
-- the server's Go read-path rule exactly (internal/claims), so the claims-mode
-- list query can filter rows before choosing each name's winning source:
--   * record SQL NULL: hidden; JSON null or {}: visible (no requirements);
--     any other non-object: hidden.
--   * AND across record keys; the caller must hold every key.
--   * A record value must be a string or an array of strings, else hidden.
--   * An empty array is satisfied by the key's presence alone.
--   * Otherwise the caller's string values (a string, or the strings in an
--     array) must overlap the record's values.
-- Only the server's claims-mode reader passes a caller; the library never does.
CREATE FUNCTION claims_visible(caller jsonb, record jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
SELECT CASE
  WHEN record IS NULL THEN false
  WHEN jsonb_typeof(record) = 'null' THEN true
  WHEN jsonb_typeof(record) <> 'object' THEN false
  ELSE NOT EXISTS (
    SELECT 1 FROM jsonb_each(record) AS r(k, v)
    WHERE NOT (
      (jsonb_typeof(r.v) = 'string'
        OR (jsonb_typeof(r.v) = 'array'
            AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(r.v) AS e(x) WHERE jsonb_typeof(e.x) <> 'string')))
      AND caller ? r.k
      AND ((jsonb_typeof(r.v) = 'array' AND jsonb_array_length(r.v) = 0)
        OR EXISTS (
          SELECT 1
          FROM (SELECT r.v #>> '{}' AS val WHERE jsonb_typeof(r.v) = 'string'
                UNION ALL
                SELECT e.x #>> '{}' FROM jsonb_array_elements(
                  CASE WHEN jsonb_typeof(r.v) = 'array' THEN r.v ELSE '[]'::jsonb END) AS e(x)) AS want
          JOIN (SELECT caller -> r.k #>> '{}' AS val WHERE jsonb_typeof(caller -> r.k) = 'string'
                UNION ALL
                SELECT e.x #>> '{}' FROM jsonb_array_elements(
                  CASE WHEN jsonb_typeof(caller -> r.k) = 'array' THEN caller -> r.k ELSE '[]'::jsonb END) AS e(x)
                WHERE jsonb_typeof(e.x) = 'string') AS have
          ON want.val = have.val))
    )
  )
END
$$;

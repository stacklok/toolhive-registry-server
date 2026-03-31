-- Index to support cursor-based pagination in GetServerVersion.
-- For a given registry_id, rows are stored in (position, source_id) order,
-- which matches the ORDER BY and the (position, source_id) compound cursor.
-- This allows PostgreSQL to skip the sort and seek directly to the cursor
-- position without scanning the full registry_source table.
CREATE INDEX registry_source_registry_id_position_source_id_idx
    ON registry_source (registry_id, position ASC, source_id ASC);

-- Index to support the join in GetServerVersion between entry_version and latest_entry_version.
-- Without this, PostgreSQL does a full seq scan of latest_entry_version (~100k rows) for each
-- row in the result set, resulting in O(n * 100k) comparisons via nested loop.
CREATE INDEX latest_entry_version_latest_version_id_idx
    ON latest_entry_version (latest_version_id);

-- Downgrading always succeeds: it restores the pre-25 shape and drops data the
-- older binary cannot represent. Skill provenance, server schema URLs,
-- transport variables and icon sizes/theme are dropped; synced sources
-- re-derive what the older binary keeps on their next sync.
--
-- Pre-25 pointers are keyed by (source, name) alone. Where a skill, plugin
-- and/or server share a name in one source, keep one pointer (MCP, then SKILL,
-- then PLUGIN). Before 25 the last writer won here, so either choice matches
-- something the older binary could have produced.
DROP TABLE IF EXISTS latest_entry_version_repair;
DROP TRIGGER check_latest_entry_kind ON latest_entry_version;
DROP FUNCTION check_latest_entry_kind();
DELETE FROM latest_entry_version l
WHERE EXISTS (
    SELECT 1 FROM latest_entry_version k
    WHERE k.source_id = l.source_id AND k.name = l.name
      AND array_position(ARRAY['MCP','SKILL','PLUGIN']::entry_type[], k.entry_type)
        < array_position(ARRAY['MCP','SKILL','PLUGIN']::entry_type[], l.entry_type)
);
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_entry_version_pkey;
ALTER TABLE latest_entry_version ADD CONSTRAINT latest_entry_version_pkey PRIMARY KEY (source_id, name);
ALTER TABLE latest_entry_version DROP COLUMN entry_type;
ALTER TABLE skill DROP COLUMN provenance;
ALTER TABLE mcp_server DROP COLUMN schema_url;
ALTER TABLE mcp_server_package DROP COLUMN transport_variables;
ALTER TABLE mcp_server_remote DROP COLUMN transport_variables;
ALTER TABLE mcp_server_icon DROP COLUMN sizes, DROP COLUMN theme_present;

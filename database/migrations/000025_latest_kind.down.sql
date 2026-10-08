-- A downgrade cannot represent two kinds with the same source/name. Refuse it
-- rather than dropping either pointer or silently reassigning it.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
               GROUP BY e.source_id, e.name HAVING count(DISTINCT e.entry_type) > 1) THEN
        RAISE EXCEPTION 'remove cross-kind version collisions before downgrading migration 25';
    END IF;
    IF EXISTS (SELECT 1 FROM skill WHERE provenance IS NOT NULL)
       OR EXISTS (SELECT 1 FROM mcp_server WHERE schema_url IS NOT NULL)
       OR EXISTS (SELECT 1 FROM mcp_server_package WHERE transport_variables IS NOT NULL)
       OR EXISTS (SELECT 1 FROM mcp_server_remote WHERE transport_variables IS NOT NULL)
       OR EXISTS (SELECT 1 FROM mcp_server_icon WHERE sizes IS NOT NULL OR theme_present IS NOT NULL) THEN
        RAISE EXCEPTION 'remove skill provenance, server schema URLs, package/remote transport variables, and icon sizes/theme presence before downgrading migration 25';
    END IF;
END $$;
DROP TRIGGER check_latest_entry_kind ON latest_entry_version;
DROP FUNCTION check_latest_entry_kind();
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_entry_version_pkey;
ALTER TABLE latest_entry_version ADD CONSTRAINT latest_entry_version_pkey PRIMARY KEY (source_id, name);
ALTER TABLE latest_entry_version DROP COLUMN entry_type;
ALTER TABLE skill DROP COLUMN provenance;
ALTER TABLE mcp_server DROP COLUMN schema_url;
ALTER TABLE mcp_server_package DROP COLUMN transport_variables;
ALTER TABLE mcp_server_remote DROP COLUMN transport_variables;
ALTER TABLE mcp_server_icon DROP COLUMN sizes, DROP COLUMN theme_present;

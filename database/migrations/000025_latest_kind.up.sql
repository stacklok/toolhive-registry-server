-- The name is shared by all three catalog kinds; a latest pointer belongs to
-- the (source, kind, name) tuple, not to the source/name alone.
ALTER TABLE latest_entry_version ADD COLUMN entry_type entry_type;
ALTER TABLE skill ADD COLUMN provenance JSONB;
ALTER TABLE mcp_server ADD COLUMN schema_url TEXT;
ALTER TABLE mcp_server_package ADD COLUMN transport_variables JSONB;
ALTER TABLE mcp_server_remote ADD COLUMN transport_variables JSONB;
ALTER TABLE mcp_server_icon ADD COLUMN sizes TEXT[], ADD COLUMN theme_present BOOLEAN;
UPDATE latest_entry_version l SET entry_type = e.entry_type
FROM entry_version v JOIN registry_entry e ON e.id = v.entry_id
WHERE v.id = l.latest_version_id;
ALTER TABLE latest_entry_version ALTER COLUMN entry_type SET NOT NULL;
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_entry_version_pkey;
ALTER TABLE latest_entry_version ADD CONSTRAINT latest_entry_version_pkey
    PRIMARY KEY (source_id, entry_type, name);
-- Existing rows retain their IDs. Other kind/name groups are repaired using the
-- Go total comparator by ReconcileLatestVersions before serving reads.
CREATE FUNCTION check_latest_entry_kind() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
      SELECT 1 FROM entry_version v JOIN registry_entry e ON e.id = v.entry_id
      WHERE v.id = NEW.latest_version_id AND e.source_id = NEW.source_id
        AND e.entry_type = NEW.entry_type AND e.name = NEW.name
    ) THEN
        RAISE EXCEPTION 'latest version must belong to its source, kind and name' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER check_latest_entry_kind BEFORE INSERT OR UPDATE ON latest_entry_version
FOR EACH ROW EXECUTE FUNCTION check_latest_entry_kind();

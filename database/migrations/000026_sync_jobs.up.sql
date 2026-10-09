-- Source generation advances for ingestion-relevant definition changes and
-- explicit direct-snapshot invalidation, including legacy source updates.
ALTER TABLE source ADD COLUMN definition_generation BIGINT NOT NULL DEFAULT 1;

CREATE FUNCTION bump_source_definition_generation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (OLD.source_config, OLD.filter_config, OLD.sync_schedule, OLD.source_type, OLD.syncable)
       IS DISTINCT FROM
       (NEW.source_config, NEW.filter_config, NEW.sync_schedule, NEW.source_type, NEW.syncable) THEN
        NEW.definition_generation := OLD.definition_generation + 1;
    ELSE
        -- Explicit direct-snapshot invalidation also advances the revision.
        NEW.definition_generation := GREATEST(OLD.definition_generation, NEW.definition_generation);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER source_definition_generation BEFORE UPDATE ON source
    FOR EACH ROW EXECUTE FUNCTION bump_source_definition_generation();

ALTER TABLE registry_sync ADD COLUMN lease_id UUID;
ALTER TABLE registry_sync ADD COLUMN lease_expires_at TIMESTAMPTZ;
ALTER TABLE registry_sync ADD COLUMN lease_generation BIGINT;
ALTER TABLE registry_sync ADD COLUMN observed_generation BIGINT;
-- Only a leased CommitSnapshot establishes a reusable no-change baseline.
ALTER TABLE registry_sync ADD COLUMN applied_generation BIGINT;
ALTER TABLE registry_sync ADD CONSTRAINT registry_sync_lease_columns CHECK
    ((lease_id IS NULL AND lease_expires_at IS NULL AND lease_generation IS NULL)
      OR (lease_id IS NOT NULL AND lease_expires_at IS NOT NULL AND lease_generation IS NOT NULL));

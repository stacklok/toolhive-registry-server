-- Do not silently discard live ownership when downgrading.
LOCK TABLE source, registry_sync IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM registry_sync WHERE lease_id IS NOT NULL
               AND lease_expires_at > clock_timestamp()) THEN
        RAISE EXCEPTION 'drain active sync jobs before downgrade';
    END IF;
END $$;
ALTER TABLE registry_sync DROP CONSTRAINT registry_sync_lease_columns;
ALTER TABLE registry_sync DROP COLUMN applied_generation;
ALTER TABLE registry_sync DROP COLUMN observed_generation;
ALTER TABLE registry_sync DROP COLUMN lease_generation;
ALTER TABLE registry_sync DROP COLUMN lease_expires_at;
ALTER TABLE registry_sync DROP COLUMN lease_id;
DROP TRIGGER source_definition_generation ON source;
DROP FUNCTION bump_source_definition_generation();
ALTER TABLE source DROP COLUMN definition_generation;

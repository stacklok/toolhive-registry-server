-- Remove additional status fields and unique constraint from registry_sync table

-- Remove is_managed field from registry table (in reverse order from up migration)
ALTER TABLE registry DROP COLUMN is_managed;

-- Remove the additional columns (in reverse order from up migration)
ALTER TABLE registry_sync DROP COLUMN server_count;
ALTER TABLE registry_sync DROP COLUMN last_applied_filter_hash;
ALTER TABLE registry_sync DROP COLUMN last_sync_hash;
ALTER TABLE registry_sync DROP COLUMN attempt_count;

-- Remove the unique constraint
ALTER TABLE registry_sync DROP CONSTRAINT registry_sync_reg_id_key;

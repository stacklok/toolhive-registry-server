-- Drop the partial index
DROP INDEX IF EXISTS idx_registry_syncable;

-- Drop the syncable column
ALTER TABLE registry DROP COLUMN IF EXISTS syncable;

-- Remove sync_schedule column from registry table
ALTER TABLE registry
DROP COLUMN IF EXISTS sync_schedule;

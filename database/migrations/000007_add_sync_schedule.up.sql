-- Add sync_schedule column to registry table
-- This stores the sync schedule as a PostgreSQL INTERVAL type
-- Nullable because non-synced registries (managed, kubernetes) don't have a sync schedule
ALTER TABLE registry
ADD COLUMN sync_schedule INTERVAL;

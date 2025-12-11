-- Add sync_schedule column to registry table
-- This stores the sync schedule interval from configuration (e.g., '30m', '1h')
-- Nullable because non-synced registries (managed, kubernetes) don't have a sync schedule
ALTER TABLE registry
ADD COLUMN sync_schedule TEXT;

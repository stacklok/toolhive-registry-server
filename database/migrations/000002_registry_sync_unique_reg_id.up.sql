-- Add unique constraint and additional status fields to registry_sync table
-- This ensures that each registry can only have one sync status record
-- and matches the fields in the SyncStatus struct

-- Add unique constraint on reg_id
ALTER TABLE registry_sync ADD CONSTRAINT registry_sync_reg_id_key UNIQUE (reg_id);

-- Add missing fields from SyncStatus struct
-- Use BIGINT for count fields to support 64-bit integers
ALTER TABLE registry_sync ADD COLUMN attempt_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE registry_sync ADD COLUMN last_sync_hash TEXT;
ALTER TABLE registry_sync ADD COLUMN last_applied_filter_hash TEXT;
ALTER TABLE registry_sync ADD COLUMN server_count BIGINT NOT NULL DEFAULT 0;

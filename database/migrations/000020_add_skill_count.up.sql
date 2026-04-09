-- Add skill_count column to registry_sync table
ALTER TABLE registry_sync ADD COLUMN skill_count BIGINT NOT NULL DEFAULT 0;

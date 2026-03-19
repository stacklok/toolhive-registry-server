-- Add claims column to registry_entry for entry-level claim storage.
-- Claims are set on first publish and must be consistent across all versions of a name.
ALTER TABLE registry_entry ADD COLUMN claims JSONB;

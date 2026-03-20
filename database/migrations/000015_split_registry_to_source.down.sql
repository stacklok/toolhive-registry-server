-- Reverse of 000015_split_registry_to_source.up.sql
-- Restores: reg_type column, drops new tables, renames source back to registry,
-- and restores all FK/constraint names to their pre-migration state.

-- =============================================================================
-- Step 1: Recreate the registry_type enum and reg_type column
-- =============================================================================
CREATE TYPE registry_type AS ENUM (
    'MANAGED',
    'FILE',
    'REMOTE',
    'KUBERNETES'
);

ALTER TABLE source ADD COLUMN reg_type registry_type;

-- Backfill reg_type from source_type (reverse of up Step 6).
-- Both 'git' and 'api' map back to REMOTE.
UPDATE source SET reg_type = CASE source_type
    WHEN 'managed'    THEN 'MANAGED'::registry_type
    WHEN 'file'       THEN 'FILE'::registry_type
    WHEN 'git'        THEN 'REMOTE'::registry_type
    WHEN 'api'        THEN 'REMOTE'::registry_type
    WHEN 'kubernetes' THEN 'KUBERNETES'::registry_type
END;

ALTER TABLE source ALTER COLUMN reg_type SET NOT NULL;

-- =============================================================================
-- Step 2: Make source_type nullable again, restore original CHECK constraint
--         (reverse of up Steps 6-7)
-- =============================================================================
ALTER TABLE source ALTER COLUMN source_type DROP NOT NULL;

-- The up migration replaced the CHECK constraint with a new name and definition.
-- Restore the original constraint name (registry_source_type_check from 000008).
ALTER TABLE source DROP CONSTRAINT source_source_type_check;
ALTER TABLE source ADD CONSTRAINT registry_source_type_check
    CHECK (source_type IN ('git', 'api', 'file', 'managed', 'kubernetes'));

-- =============================================================================
-- Step 3: Drop junction and new registry tables (reverse of up Steps 3-5)
-- =============================================================================
DROP TABLE IF EXISTS registry_source;
DROP TABLE IF EXISTS registry;

-- =============================================================================
-- Step 4: Reverse latest_entry_version FK changes (reverse of up Step 2c)
-- =============================================================================
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_entry_version_source_id_fkey;
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_entry_version_pkey;

ALTER TABLE latest_entry_version RENAME COLUMN source_id TO reg_id;

-- Restore the legacy "server_version" constraint names from before this migration.
ALTER TABLE latest_entry_version ADD CONSTRAINT latest_server_version_pkey
    PRIMARY KEY (reg_id, name);
ALTER TABLE latest_entry_version ADD CONSTRAINT latest_server_version_reg_id_fkey
    FOREIGN KEY (reg_id) REFERENCES source(id) ON DELETE CASCADE;

-- =============================================================================
-- Step 5: Reverse registry_sync FK changes (reverse of up Step 2b)
-- =============================================================================
-- Rename indexes back.
ALTER INDEX source_sync_started_at_idx RENAME TO registry_sync_started_at_idx;
ALTER INDEX source_sync_end_at_idx RENAME TO registry_sync_end_at_idx;

-- Drop the new-name constraints.
ALTER TABLE registry_sync DROP CONSTRAINT source_sync_source_id_key;
ALTER TABLE registry_sync DROP CONSTRAINT source_sync_source_id_fkey;

-- Rename column back.
ALTER TABLE registry_sync RENAME COLUMN source_id TO reg_id;

-- Restore the original constraint names.
ALTER TABLE registry_sync ADD CONSTRAINT registry_sync_reg_id_key UNIQUE (reg_id);
ALTER TABLE registry_sync ADD CONSTRAINT registry_sync_reg_id_fkey
    FOREIGN KEY (reg_id) REFERENCES source(id) ON DELETE CASCADE;

-- =============================================================================
-- Step 6: Reverse registry_entry FK changes (reverse of up Step 2a)
-- =============================================================================
-- Rename index back.
ALTER INDEX registry_entry_source_id_idx RENAME TO registry_entry_reg_id_idx;

-- Drop the new-name constraints.
ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_source_id_entry_type_name_key;
ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_source_id_fkey;

-- Rename column back.
ALTER TABLE registry_entry RENAME COLUMN source_id TO reg_id;

-- Restore the original constraint names.
ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_reg_id_entry_type_name_key
    UNIQUE (reg_id, entry_type, name);
ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_reg_id_fkey
    FOREIGN KEY (reg_id) REFERENCES source(id) ON DELETE CASCADE;

-- =============================================================================
-- Step 7: Rename source back to registry (reverse of up Step 1)
-- =============================================================================
-- First drop all FKs that currently reference source(id) so we can rename the table.
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_server_version_reg_id_fkey;
ALTER TABLE registry_sync DROP CONSTRAINT registry_sync_reg_id_fkey;
ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_reg_id_fkey;

-- Rename constraints and table.
ALTER TABLE source RENAME CONSTRAINT source_pkey TO registry_pkey;
ALTER TABLE source RENAME CONSTRAINT source_name_key TO registry_name_key;
ALTER TABLE source RENAME TO registry;

-- Re-add FKs pointing to the now-renamed registry table.
ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_reg_id_fkey
    FOREIGN KEY (reg_id) REFERENCES registry(id) ON DELETE CASCADE;
ALTER TABLE registry_sync ADD CONSTRAINT registry_sync_reg_id_fkey
    FOREIGN KEY (reg_id) REFERENCES registry(id) ON DELETE CASCADE;
ALTER TABLE latest_entry_version ADD CONSTRAINT latest_server_version_reg_id_fkey
    FOREIGN KEY (reg_id) REFERENCES registry(id) ON DELETE CASCADE;

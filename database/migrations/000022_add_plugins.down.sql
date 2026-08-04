-- Rollback migration: Remove plugin support.

-- =============================================================================
-- Drop plugin-related tables, column, and type in reverse order of creation.
-- =============================================================================

-- Remove plugin_count from registry_sync
ALTER TABLE registry_sync DROP COLUMN plugin_count;

-- Drop package tables (CASCADE not needed; standalone tables)
DROP TABLE IF EXISTS plugin_git_package;
DROP TABLE IF EXISTS plugin_oci_package;

-- Drop plugin table
DROP TABLE IF EXISTS plugin;

-- Drop plugin_status enum
DROP TYPE IF EXISTS plugin_status;

-- NOTE: The 'PLUGIN' value cannot be removed from the entry_type enum once
-- added — PostgreSQL does not support dropping individual enum values. This
-- down migration therefore leaves 'PLUGIN' in entry_type. Re-applying the up
-- migration is safe because of the IF NOT EXISTS guard on ALTER TYPE.

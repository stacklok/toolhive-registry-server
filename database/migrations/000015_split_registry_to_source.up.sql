-- Migration: Split the registry table into source (data providers) and
-- registry (logical grouping).
--
-- Before: registry holds both identity *and* sync/source configuration.
-- After:
--   source          = renamed registry; carries sync/source configuration.
--   registry        = new lightweight table (name + claims + creation_type).
--   registry_source = many-to-many junction between registry and source.
--
-- Existing foreign keys from registry_entry, registry_sync, and
-- latest_entry_version are re-pointed from registry → source.
-- A 1:1 backfill creates a matching registry row for every existing source.

-- =============================================================================
-- Step 1: Rename registry -> source
-- =============================================================================
ALTER TABLE registry RENAME TO source;
ALTER TABLE source RENAME CONSTRAINT registry_pkey TO source_pkey;
ALTER TABLE source RENAME CONSTRAINT registry_name_key TO source_name_key;

-- =============================================================================
-- Step 2: Rename FK references in dependent tables
--         reg_id -> source_id in registry_entry, registry_sync,
--         and latest_entry_version
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 2a: registry_entry
-- -----------------------------------------------------------------------------
-- Rename the column.
ALTER TABLE registry_entry RENAME COLUMN reg_id TO source_id;

-- Drop the old FK and unique constraint, then recreate with new column name.
ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_reg_id_fkey;
ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_source_id_fkey
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE;

ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_reg_id_entry_type_name_key;
ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_source_id_entry_type_name_key
    UNIQUE (source_id, entry_type, name);

-- Rename the index.
ALTER INDEX registry_entry_reg_id_idx RENAME TO registry_entry_source_id_idx;

-- -----------------------------------------------------------------------------
-- 2b: registry_sync
-- -----------------------------------------------------------------------------
-- Rename the column.
ALTER TABLE registry_sync RENAME COLUMN reg_id TO source_id;

-- Rename indexes.
ALTER INDEX registry_sync_started_at_idx RENAME TO source_sync_started_at_idx;
ALTER INDEX registry_sync_end_at_idx RENAME TO source_sync_end_at_idx;

-- Drop old unique and FK constraints, recreate with new column name.
ALTER TABLE registry_sync DROP CONSTRAINT registry_sync_reg_id_key;
ALTER TABLE registry_sync ADD CONSTRAINT source_sync_source_id_key
    UNIQUE (source_id);

ALTER TABLE registry_sync DROP CONSTRAINT registry_sync_reg_id_fkey;
ALTER TABLE registry_sync ADD CONSTRAINT source_sync_source_id_fkey
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE;

-- -----------------------------------------------------------------------------
-- 2c: latest_entry_version
-- -----------------------------------------------------------------------------
-- Drop old PK and FK that still carry the legacy "server_version" names.
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_server_version_reg_id_fkey;
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_server_version_pkey;

-- Rename the column.
ALTER TABLE latest_entry_version RENAME COLUMN reg_id TO source_id;

-- Recreate PK and FK with the new column name.
ALTER TABLE latest_entry_version ADD CONSTRAINT latest_entry_version_pkey
    PRIMARY KEY (source_id, name);

ALTER TABLE latest_entry_version ADD CONSTRAINT latest_entry_version_source_id_fkey
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE;

-- =============================================================================
-- Step 3: Create the new registry table
-- =============================================================================
CREATE TABLE registry (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL UNIQUE,
    claims        JSONB,
    creation_type creation_type NOT NULL DEFAULT 'CONFIG',
    created_at    TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- =============================================================================
-- Step 4: Create registry_source junction table
-- =============================================================================
CREATE TABLE registry_source (
    registry_id UUID NOT NULL REFERENCES registry(id) ON DELETE CASCADE,
    source_id   UUID NOT NULL REFERENCES source(id) ON DELETE RESTRICT,
    position    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (registry_id, source_id)
);

CREATE INDEX registry_source_source_id_idx ON registry_source(source_id);

-- =============================================================================
-- Step 5: Backfill - create a registry row for every existing source
-- =============================================================================
INSERT INTO registry (name, creation_type, created_at, updated_at)
SELECT name, creation_type, created_at, updated_at FROM source;

INSERT INTO registry_source (registry_id, source_id)
SELECT r.id, s.id FROM registry r JOIN source s ON r.name = s.name;

-- =============================================================================
-- Step 6: Backfill source_type from reg_type where it was not already set
-- =============================================================================
UPDATE source SET source_type = CASE reg_type
    WHEN 'MANAGED'    THEN 'managed'
    WHEN 'FILE'       THEN 'file'
    WHEN 'REMOTE'     THEN 'git'
    WHEN 'KUBERNETES' THEN 'kubernetes'
END WHERE source_type IS NULL;

ALTER TABLE source ALTER COLUMN source_type SET NOT NULL;

-- =============================================================================
-- Step 7: Rename CHECK constraint on source_type
--         (originally registry_source_type_check from migration 000008)
-- =============================================================================
ALTER TABLE source DROP CONSTRAINT registry_source_type_check;
ALTER TABLE source ADD CONSTRAINT source_source_type_check
    CHECK (source_type IN ('git', 'api', 'file', 'managed', 'kubernetes'));

-- =============================================================================
-- Step 8: Drop the now-redundant reg_type column and its enum
-- =============================================================================
ALTER TABLE source DROP COLUMN reg_type;
DROP TYPE registry_type;

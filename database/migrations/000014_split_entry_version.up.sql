-- Migration: Split registry_entry into registry_entry (name + non-version metadata)
-- and entry_version (version-specific details).
--
-- Currently each registry_entry row represents a (name, version) pair.
-- After this migration:
--   registry_entry  = one row per unique (reg_id, entry_type, name)
--   entry_version   = one row per (entry, version), carrying title/description
--
-- entry_version.id reuses the old registry_entry.id values so that existing
-- FK references from mcp_server, skill, and latest_entry_version remain valid
-- — only the constraint targets change.

-- =============================================================================
-- Step 1: Create entry_version table
-- =============================================================================
CREATE TABLE entry_version (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id    UUID NOT NULL,
    version     TEXT NOT NULL,
    title       TEXT,
    description TEXT,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- =============================================================================
-- Step 2: Pick one canonical registry_entry row per (reg_id, entry_type, name)
-- =============================================================================
CREATE TEMP TABLE _canonical AS
SELECT DISTINCT ON (reg_id, entry_type, name)
       id AS canonical_id,
       reg_id,
       entry_type,
       name
  FROM registry_entry
 ORDER BY reg_id, entry_type, name, id;

-- =============================================================================
-- Step 3: Populate entry_version from existing registry_entry rows
--   entry_version.id       = old registry_entry.id  (preserves child FK values)
--   entry_version.entry_id = canonical id for this (reg_id, entry_type, name)
-- =============================================================================
INSERT INTO entry_version (id, entry_id, version, title, description, created_at, updated_at)
SELECT re.id,
       c.canonical_id,
       re.version,
       re.title,
       re.description,
       re.created_at,
       re.updated_at
  FROM registry_entry re
  JOIN _canonical c ON re.reg_id = c.reg_id
                   AND re.entry_type = c.entry_type
                   AND re.name = c.name;

-- =============================================================================
-- Step 4: Drop FK constraints that reference registry_entry(id) and
--         child table FKs that reference mcp_server(entry_id)
-- =============================================================================
ALTER TABLE mcp_server DROP CONSTRAINT mcp_server_entry_id_fkey;
ALTER TABLE skill DROP CONSTRAINT skill_entry_id_fkey;
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_entry_version_latest_entry_id_fkey;
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_entry_id_fkey;
ALTER TABLE mcp_server_remote DROP CONSTRAINT mcp_server_remote_entry_id_fkey;
ALTER TABLE mcp_server_icon DROP CONSTRAINT mcp_server_icon_entry_id_fkey;
ALTER TABLE skill_oci_package DROP CONSTRAINT skill_oci_package_skill_entry_id_fkey;
ALTER TABLE skill_git_package DROP CONSTRAINT skill_git_package_skill_entry_id_fkey;

-- =============================================================================
-- Step 5: Rename columns
--   mcp_server.entry_id          → version_id  (now references entry_version)
--   skill.entry_id               → version_id  (now references entry_version)
--   mcp_server_{package,remote,icon}.entry_id → server_id
--   skill_{oci,git}_package.skill_entry_id    → skill_id
--   latest_entry_version.latest_entry_id      → latest_version_id
-- =============================================================================
ALTER TABLE mcp_server RENAME COLUMN entry_id TO version_id;
ALTER TABLE skill RENAME COLUMN entry_id TO version_id;
ALTER TABLE mcp_server_package RENAME COLUMN entry_id TO server_id;
ALTER TABLE mcp_server_remote RENAME COLUMN entry_id TO server_id;
ALTER TABLE mcp_server_icon RENAME COLUMN entry_id TO server_id;
ALTER TABLE skill_oci_package RENAME COLUMN skill_entry_id TO skill_id;
ALTER TABLE skill_git_package RENAME COLUMN skill_entry_id TO skill_id;
ALTER TABLE latest_entry_version RENAME COLUMN latest_entry_id TO latest_version_id;

-- =============================================================================
-- Step 6: Recreate FKs with new column names, pointing to entry_version(id)
-- =============================================================================
ALTER TABLE mcp_server ADD CONSTRAINT mcp_server_version_id_fkey
    FOREIGN KEY (version_id) REFERENCES entry_version(id) ON DELETE CASCADE;

ALTER TABLE skill ADD CONSTRAINT skill_version_id_fkey
    FOREIGN KEY (version_id) REFERENCES entry_version(id) ON DELETE CASCADE;

ALTER TABLE latest_entry_version ADD CONSTRAINT latest_entry_version_latest_version_id_fkey
    FOREIGN KEY (latest_version_id) REFERENCES entry_version(id) ON DELETE CASCADE;

ALTER TABLE mcp_server_package ADD CONSTRAINT mcp_server_package_server_id_fkey
    FOREIGN KEY (server_id) REFERENCES mcp_server(version_id) ON DELETE CASCADE;

ALTER TABLE mcp_server_remote ADD CONSTRAINT mcp_server_remote_server_id_fkey
    FOREIGN KEY (server_id) REFERENCES mcp_server(version_id) ON DELETE CASCADE;

ALTER TABLE mcp_server_icon ADD CONSTRAINT mcp_server_icon_server_id_fkey
    FOREIGN KEY (server_id) REFERENCES mcp_server(version_id) ON DELETE CASCADE;

ALTER TABLE skill_oci_package ADD CONSTRAINT skill_oci_package_skill_id_fkey
    FOREIGN KEY (skill_id) REFERENCES skill(version_id) ON DELETE CASCADE;

ALTER TABLE skill_git_package ADD CONSTRAINT skill_git_package_skill_id_fkey
    FOREIGN KEY (skill_id) REFERENCES skill(version_id) ON DELETE CASCADE;

-- =============================================================================
-- Step 7: Delete non-canonical registry_entry rows and update timestamps
-- =============================================================================
DELETE FROM registry_entry
 WHERE id NOT IN (SELECT canonical_id FROM _canonical);

UPDATE registry_entry re
   SET created_at = agg.min_created,
       updated_at = agg.max_updated
  FROM (
    SELECT entry_id,
           MIN(created_at) AS min_created,
           MAX(updated_at) AS max_updated
      FROM entry_version
     GROUP BY entry_id
  ) agg
 WHERE re.id = agg.entry_id;

DROP TABLE _canonical;

-- =============================================================================
-- Step 8: Remove version-specific columns from registry_entry
-- =============================================================================
ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_reg_id_entry_type_name_version_key;
ALTER TABLE registry_entry DROP COLUMN version;
ALTER TABLE registry_entry DROP COLUMN title;
ALTER TABLE registry_entry DROP COLUMN description;

ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_reg_id_entry_type_name_key
    UNIQUE (reg_id, entry_type, name);

-- =============================================================================
-- Step 9: Add FK and indexes on entry_version
-- =============================================================================
ALTER TABLE entry_version ADD CONSTRAINT entry_version_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES registry_entry(id) ON DELETE CASCADE;

ALTER TABLE entry_version ADD CONSTRAINT entry_version_entry_id_version_key
    UNIQUE (entry_id, version);

CREATE INDEX entry_version_entry_id_idx ON entry_version(entry_id);
CREATE INDEX entry_version_version_idx ON entry_version(version);

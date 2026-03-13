-- Rollback: Merge entry_version back into registry_entry

-- =============================================================================
-- Step 1: Drop entry_version → registry_entry FK
-- =============================================================================
ALTER TABLE entry_version DROP CONSTRAINT entry_version_entry_id_fkey;

-- =============================================================================
-- Step 2: Drop child table FKs that reference entry_version(id) and
--         child table FKs that reference mcp_server(version_id)
-- =============================================================================
ALTER TABLE mcp_server DROP CONSTRAINT mcp_server_version_id_fkey;
ALTER TABLE skill DROP CONSTRAINT skill_version_id_fkey;
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_entry_version_latest_version_id_fkey;
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_server_id_fkey;
ALTER TABLE mcp_server_remote DROP CONSTRAINT mcp_server_remote_server_id_fkey;
ALTER TABLE mcp_server_icon DROP CONSTRAINT mcp_server_icon_server_id_fkey;
ALTER TABLE skill_oci_package DROP CONSTRAINT skill_oci_package_skill_id_fkey;
ALTER TABLE skill_git_package DROP CONSTRAINT skill_git_package_skill_id_fkey;

-- =============================================================================
-- Step 3: Rename columns back to original names
-- =============================================================================
ALTER TABLE mcp_server RENAME COLUMN version_id TO entry_id;
ALTER TABLE skill RENAME COLUMN version_id TO entry_id;
ALTER TABLE mcp_server_package RENAME COLUMN server_id TO entry_id;
ALTER TABLE mcp_server_remote RENAME COLUMN server_id TO entry_id;
ALTER TABLE mcp_server_icon RENAME COLUMN server_id TO entry_id;
ALTER TABLE skill_oci_package RENAME COLUMN skill_id TO skill_entry_id;
ALTER TABLE skill_git_package RENAME COLUMN skill_id TO skill_entry_id;

-- =============================================================================
-- Step 4: Restore version-specific columns on registry_entry
-- =============================================================================
ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_reg_id_entry_type_name_key;
ALTER TABLE registry_entry ADD COLUMN version TEXT;
ALTER TABLE registry_entry ADD COLUMN title TEXT;
ALTER TABLE registry_entry ADD COLUMN description TEXT;

-- =============================================================================
-- Step 5: Re-create registry_entry rows for non-canonical versions
-- (these were deleted during the up migration)
-- =============================================================================
INSERT INTO registry_entry (id, reg_id, entry_type, name, version, title, description, created_at, updated_at)
SELECT ev.id,
       re.reg_id,
       re.entry_type,
       re.name,
       ev.version,
       ev.title,
       ev.description,
       ev.created_at,
       ev.updated_at
  FROM entry_version ev
  JOIN registry_entry re ON ev.entry_id = re.id
 WHERE ev.id != re.id;

-- =============================================================================
-- Step 6: Restore version data on the canonical registry_entry rows
-- =============================================================================
UPDATE registry_entry re
   SET version     = ev.version,
       title       = ev.title,
       description = ev.description,
       created_at  = ev.created_at,
       updated_at  = ev.updated_at
  FROM entry_version ev
 WHERE re.id = ev.id;

-- =============================================================================
-- Step 7: Restore NOT NULL and unique constraint
-- =============================================================================
ALTER TABLE registry_entry ALTER COLUMN version SET NOT NULL;
ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_reg_id_entry_type_name_version_key
    UNIQUE (reg_id, entry_type, name, version);

-- =============================================================================
-- Step 8: Point child table FKs back to registry_entry(id) and
--         recreate child table FKs to mcp_server(entry_id)
-- =============================================================================
ALTER TABLE mcp_server ADD CONSTRAINT mcp_server_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES registry_entry(id) ON DELETE CASCADE;

ALTER TABLE skill ADD CONSTRAINT skill_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES registry_entry(id) ON DELETE CASCADE;

ALTER TABLE latest_entry_version RENAME COLUMN latest_version_id TO latest_entry_id;

ALTER TABLE latest_entry_version ADD CONSTRAINT latest_entry_version_latest_entry_id_fkey
    FOREIGN KEY (latest_entry_id) REFERENCES registry_entry(id) ON DELETE CASCADE;

ALTER TABLE mcp_server_package ADD CONSTRAINT mcp_server_package_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES mcp_server(entry_id) ON DELETE CASCADE;

ALTER TABLE mcp_server_remote ADD CONSTRAINT mcp_server_remote_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES mcp_server(entry_id) ON DELETE CASCADE;

ALTER TABLE mcp_server_icon ADD CONSTRAINT mcp_server_icon_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES mcp_server(entry_id) ON DELETE CASCADE;

ALTER TABLE skill_oci_package ADD CONSTRAINT skill_oci_package_skill_entry_id_fkey
    FOREIGN KEY (skill_entry_id) REFERENCES skill(entry_id) ON DELETE CASCADE;

ALTER TABLE skill_git_package ADD CONSTRAINT skill_git_package_skill_entry_id_fkey
    FOREIGN KEY (skill_entry_id) REFERENCES skill(entry_id) ON DELETE CASCADE;

-- =============================================================================
-- Step 9: Drop entry_version table and its indexes
-- =============================================================================
DROP INDEX IF EXISTS entry_version_version_idx;
DROP INDEX IF EXISTS entry_version_entry_id_idx;
DROP TABLE entry_version;

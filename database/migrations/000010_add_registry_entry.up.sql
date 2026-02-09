-- Migration to add registry_entry as a root table for mcp_server and skill
-- This migration restructures mcp_server to reference registry_entry

-- =============================================================================
-- Step 1: Create entry_type enum
-- =============================================================================
CREATE TYPE entry_type AS ENUM ('MCP', 'SKILL');

-- =============================================================================
-- Step 2: Create registry_entry table
-- This is the root table that contains common properties for both
-- MCP servers and skills
-- =============================================================================
CREATE TABLE registry_entry (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reg_id      UUID NOT NULL REFERENCES registry(id) ON DELETE CASCADE,
    entry_type  entry_type NOT NULL,
    name        TEXT NOT NULL,
    title       TEXT,
    description TEXT,
    version     TEXT NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (reg_id, name, version)
);

CREATE INDEX registry_entry_reg_id_idx ON registry_entry(reg_id);
CREATE INDEX registry_entry_name_idx ON registry_entry(name);
CREATE INDEX registry_entry_entry_type_idx ON registry_entry(entry_type);

-- =============================================================================
-- Step 3: Migrate existing mcp_server data to registry_entry
-- Preserve the same UUIDs to maintain referential integrity
-- =============================================================================
INSERT INTO registry_entry (id, reg_id, entry_type, name, title, description, version, created_at, updated_at)
SELECT id, reg_id, 'MCP', name, title, description, version, created_at, updated_at
FROM mcp_server;

-- =============================================================================
-- Step 4: Modify mcp_server to reference registry_entry
-- =============================================================================

-- 4a: Add entry_id column to mcp_server
ALTER TABLE mcp_server ADD COLUMN entry_id UUID;

-- 4b: Populate entry_id with current id values (same UUIDs)
UPDATE mcp_server SET entry_id = id;

-- 4c: Make entry_id NOT NULL
ALTER TABLE mcp_server ALTER COLUMN entry_id SET NOT NULL;

-- =============================================================================
-- Step 5: Drop FK constraints from child tables before modifying mcp_server
-- =============================================================================
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_server_id_fkey;
ALTER TABLE mcp_server_remote DROP CONSTRAINT mcp_server_remote_server_id_fkey;
ALTER TABLE mcp_server_icon DROP CONSTRAINT mcp_server_icon_server_id_fkey;
ALTER TABLE latest_server_version DROP CONSTRAINT latest_server_version_latest_server_id_fkey;

-- Rename table to latest_entry_version
ALTER TABLE latest_server_version RENAME TO latest_entry_version;

-- =============================================================================
-- Step 6: Modify mcp_server primary key and structure
-- =============================================================================

-- 6a: Drop the unique constraint (now handled by registry_entry)
ALTER TABLE mcp_server DROP CONSTRAINT mcp_server_reg_id_name_version_key;

-- 6b: Drop the old primary key constraint
ALTER TABLE mcp_server DROP CONSTRAINT mcp_server_pkey;

-- 6c: Drop the old id column
ALTER TABLE mcp_server DROP COLUMN id;

-- 6d: Make entry_id the new primary key
ALTER TABLE mcp_server ADD PRIMARY KEY (entry_id);

-- 6e: Add FK constraint from entry_id to registry_entry
ALTER TABLE mcp_server ADD CONSTRAINT mcp_server_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES registry_entry(id) ON DELETE CASCADE;

-- 6f: Drop redundant columns that are now in registry_entry
ALTER TABLE mcp_server DROP COLUMN name;
ALTER TABLE mcp_server DROP COLUMN title;
ALTER TABLE mcp_server DROP COLUMN description;
ALTER TABLE mcp_server DROP COLUMN version;
ALTER TABLE mcp_server DROP COLUMN reg_id;
ALTER TABLE mcp_server DROP COLUMN created_at;
ALTER TABLE mcp_server DROP COLUMN updated_at;

-- =============================================================================
-- Step 7: Update child tables to use entry_id
-- =============================================================================

-- 7a: mcp_server_package - rename server_id to entry_id and recreate FK
ALTER TABLE mcp_server_package RENAME COLUMN server_id TO entry_id;
ALTER TABLE mcp_server_package ADD CONSTRAINT mcp_server_package_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES mcp_server(entry_id) ON DELETE CASCADE;

-- 7b: mcp_server_remote - rename column and update PK
ALTER TABLE mcp_server_remote DROP CONSTRAINT mcp_server_remote_pkey;
ALTER TABLE mcp_server_remote RENAME COLUMN server_id TO entry_id;
ALTER TABLE mcp_server_remote ADD PRIMARY KEY (entry_id, transport, transport_url);
ALTER TABLE mcp_server_remote ADD CONSTRAINT mcp_server_remote_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES mcp_server(entry_id) ON DELETE CASCADE;

-- 7c: mcp_server_icon - rename column and update PK
ALTER TABLE mcp_server_icon DROP CONSTRAINT mcp_server_icon_pkey;
ALTER TABLE mcp_server_icon RENAME COLUMN server_id TO entry_id;
ALTER TABLE mcp_server_icon ADD PRIMARY KEY (entry_id, source_uri, mime_type, theme);
ALTER TABLE mcp_server_icon ADD CONSTRAINT mcp_server_icon_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES mcp_server(entry_id) ON DELETE CASCADE;

-- 7d: latest_entry_version - rename column and recreate FK to registry_entry
ALTER TABLE latest_entry_version RENAME COLUMN latest_server_id TO latest_entry_id;
ALTER TABLE latest_entry_version ADD CONSTRAINT latest_entry_version_latest_entry_id_fkey
    FOREIGN KEY (latest_entry_id) REFERENCES registry_entry(id) ON DELETE CASCADE;

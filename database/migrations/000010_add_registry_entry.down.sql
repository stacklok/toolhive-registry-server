-- Rollback migration: Undo registry_entry changes and restore mcp_server structure

-- =============================================================================
-- Step 1: Restore mcp_server child tables to use server_id
-- =============================================================================

-- 1a: latest_entry_version - rename table and column back, drop FK
ALTER TABLE latest_entry_version DROP CONSTRAINT latest_entry_version_latest_entry_id_fkey;
ALTER TABLE latest_entry_version RENAME COLUMN latest_entry_id TO latest_server_id;
ALTER TABLE latest_entry_version RENAME TO latest_server_version;

-- 1b: mcp_server_icon - restore original structure
ALTER TABLE mcp_server_icon DROP CONSTRAINT mcp_server_icon_entry_id_fkey;
ALTER TABLE mcp_server_icon DROP CONSTRAINT mcp_server_icon_pkey;
ALTER TABLE mcp_server_icon RENAME COLUMN entry_id TO server_id;
ALTER TABLE mcp_server_icon ADD PRIMARY KEY (server_id, source_uri, mime_type, theme);

-- 1c: mcp_server_remote - restore original structure
ALTER TABLE mcp_server_remote DROP CONSTRAINT mcp_server_remote_entry_id_fkey;
ALTER TABLE mcp_server_remote DROP CONSTRAINT mcp_server_remote_pkey;
ALTER TABLE mcp_server_remote RENAME COLUMN entry_id TO server_id;
ALTER TABLE mcp_server_remote ADD PRIMARY KEY (server_id, transport, transport_url);

-- 1d: mcp_server_package - restore original structure
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_entry_id_fkey;
ALTER TABLE mcp_server_package RENAME COLUMN entry_id TO server_id;

-- =============================================================================
-- Step 2: Restore mcp_server structure
-- =============================================================================

-- 2a: Drop FK to registry_entry and PK constraint
ALTER TABLE mcp_server DROP CONSTRAINT mcp_server_entry_id_fkey;
ALTER TABLE mcp_server DROP CONSTRAINT mcp_server_pkey;

-- 2b: Rename entry_id to id
ALTER TABLE mcp_server RENAME COLUMN entry_id TO id;

-- 2c: Add back the columns that were moved to registry_entry
ALTER TABLE mcp_server ADD COLUMN name TEXT;
ALTER TABLE mcp_server ADD COLUMN title TEXT;
ALTER TABLE mcp_server ADD COLUMN description TEXT;
ALTER TABLE mcp_server ADD COLUMN version TEXT;
ALTER TABLE mcp_server ADD COLUMN reg_id UUID;
ALTER TABLE mcp_server ADD COLUMN created_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE mcp_server ADD COLUMN updated_at TIMESTAMP WITH TIME ZONE;

-- 2d: Restore data from registry_entry
UPDATE mcp_server ms
SET name = re.name,
    title = re.title,
    description = re.description,
    version = re.version,
    reg_id = re.reg_id,
    created_at = re.created_at,
    updated_at = re.updated_at
FROM registry_entry re
WHERE ms.id = re.id;

-- 2e: Make required columns NOT NULL
ALTER TABLE mcp_server ALTER COLUMN name SET NOT NULL;
ALTER TABLE mcp_server ALTER COLUMN version SET NOT NULL;

-- 2f: Add back the original constraints
ALTER TABLE mcp_server ADD PRIMARY KEY (id);
ALTER TABLE mcp_server ADD CONSTRAINT mcp_server_reg_id_fkey
    FOREIGN KEY (reg_id) REFERENCES registry(id) ON DELETE CASCADE;
ALTER TABLE mcp_server ADD CONSTRAINT mcp_server_reg_id_name_version_key
    UNIQUE (reg_id, name, version);

-- =============================================================================
-- Step 3: Restore FK constraints on child tables
-- =============================================================================
ALTER TABLE mcp_server_package ADD CONSTRAINT mcp_server_package_server_id_fkey
    FOREIGN KEY (server_id) REFERENCES mcp_server(id) ON DELETE CASCADE;

ALTER TABLE mcp_server_remote ADD CONSTRAINT mcp_server_remote_server_id_fkey
    FOREIGN KEY (server_id) REFERENCES mcp_server(id) ON DELETE CASCADE;

ALTER TABLE mcp_server_icon ADD CONSTRAINT mcp_server_icon_server_id_fkey
    FOREIGN KEY (server_id) REFERENCES mcp_server(id) ON DELETE CASCADE;

ALTER TABLE latest_server_version ADD CONSTRAINT latest_server_version_latest_server_id_fkey
    FOREIGN KEY (latest_server_id) REFERENCES mcp_server(id) ON DELETE CASCADE;

-- =============================================================================
-- Step 4: Drop registry_entry table and entry_type enum
-- =============================================================================
DROP TABLE IF EXISTS registry_entry;
DROP TYPE IF EXISTS entry_type;

-- Migration to add plugin support to the registry server.
-- Plugins mirror skills but omit `compatibility` and `allowed_tools`
-- (plugin analogues of those skill-only fields do not exist).

-- =============================================================================
-- Step 1: Extend entry_type enum with PLUGIN
-- =============================================================================
ALTER TYPE entry_type ADD VALUE IF NOT EXISTS 'PLUGIN';

-- =============================================================================
-- Step 2: Create plugin_status enum (distinct from skill_status)
-- =============================================================================
CREATE TYPE plugin_status AS ENUM ('ACTIVE', 'DEPRECATED', 'ARCHIVED');

-- =============================================================================
-- Step 3: Create plugin table
--   Mirrors the CURRENT skill table shape (post-migration-000014):
--   version_id UUID PRIMARY KEY REFERENCES entry_version(id) ON DELETE CASCADE
-- =============================================================================
CREATE TABLE plugin (
    version_id     UUID PRIMARY KEY REFERENCES entry_version(id) ON DELETE CASCADE,
    namespace      TEXT NOT NULL,  -- Ownership scope (e.g., io.github.stacklok)
    status         plugin_status NOT NULL DEFAULT 'ACTIVE',
    license        TEXT,           -- SPDX license identifier
    repository     JSONB,          -- Source repository metadata
    icons          JSONB,          -- Display icons for UI
    metadata       JSONB,          -- Official metadata from plugin manifest
    extension_meta JSONB           -- Opaque extended meta (_meta)
);

CREATE INDEX plugin_namespace_idx ON plugin(namespace);
CREATE INDEX plugin_status_idx ON plugin(status);

-- =============================================================================
-- Step 4: Create plugin_oci_package table
--   Mirrors skill_oci_package (FK column named plugin_id, mirrors skill_id).
-- =============================================================================
CREATE TABLE plugin_oci_package (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plugin_id    UUID NOT NULL REFERENCES plugin(version_id) ON DELETE CASCADE,
    identifier   TEXT NOT NULL,  -- e.g., ghcr.io/stacklok/plugins/my-plugin:1.0.0
    digest       TEXT,           -- sha256:abc123...
    media_type   TEXT            -- application/vnd.stacklok.plugin.v1
);

CREATE INDEX plugin_oci_package_plugin_id_idx ON plugin_oci_package(plugin_id);

-- =============================================================================
-- Step 5: Create plugin_git_package table
--   Mirrors skill_git_package (FK column named plugin_id, mirrors skill_id).
-- =============================================================================
CREATE TABLE plugin_git_package (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plugin_id   UUID NOT NULL REFERENCES plugin(version_id) ON DELETE CASCADE,
    url         TEXT NOT NULL,  -- e.g., https://github.com/stacklok/plugins.git
    ref         TEXT,           -- e.g., v1.0.0
    commit_sha  TEXT,           -- Full commit SHA
    subfolder   TEXT            -- Path within repository
);

CREATE INDEX plugin_git_package_plugin_id_idx ON plugin_git_package(plugin_id);

-- =============================================================================
-- Step 6: Add plugin_count to registry_sync (mirrors 000020's skill_count)
-- =============================================================================
ALTER TABLE registry_sync ADD COLUMN plugin_count BIGINT NOT NULL DEFAULT 0;

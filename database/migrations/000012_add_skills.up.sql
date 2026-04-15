-- Migration to add skill support to the registry server
-- Based on Agent Skills specification: https://agentskills.io/specification

-- =============================================================================
-- Step 1: Create skill_status enum
-- =============================================================================
CREATE TYPE skill_status AS ENUM ('ACTIVE', 'DEPRECATED', 'ARCHIVED');

-- =============================================================================
-- Step 2: Create skill table
-- =============================================================================
CREATE TABLE skill (
    entry_id       UUID PRIMARY KEY REFERENCES registry_entry(id) ON DELETE CASCADE,
    namespace      TEXT NOT NULL,  -- Ownership scope (e.g., io.github.stacklok)
    status         skill_status NOT NULL DEFAULT 'ACTIVE',
    license        TEXT,           -- SPDX license identifier
    compatibility  TEXT,           -- Environment requirements
    allowed_tools  TEXT[],         -- Pre-approved tools (experimental)
    repository     JSONB,          -- Source repository metadata
    icons          JSONB,          -- Display icons for UI
    metadata       JSONB,          -- Custom key-value pairs from author
    extension_meta JSONB,          -- _meta in the spec (reverse-DNS namespacing)
    UNIQUE (entry_id, namespace)
);

CREATE INDEX skill_namespace_idx ON skill(namespace);
CREATE INDEX skill_status_idx ON skill(status);

-- =============================================================================
-- Step 3: Create skill_oci_package table
-- For OCI-based skill distribution
-- =============================================================================
CREATE TABLE skill_oci_package (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_entry_id  UUID NOT NULL REFERENCES skill(entry_id) ON DELETE CASCADE,
    identifier      TEXT NOT NULL,  -- e.g., ghcr.io/stacklok/skills/pdf-processor:1.0.0
    digest          TEXT,           -- sha256:abc123...
    media_type      TEXT            -- application/vnd.stacklok.skillet.skill.v1
);

CREATE INDEX skill_oci_package_skill_entry_id_idx ON skill_oci_package(skill_entry_id);

-- =============================================================================
-- Step 4: Create skill_git_package table
-- For Git-based skill distribution
-- =============================================================================
CREATE TABLE skill_git_package (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_entry_id  UUID NOT NULL REFERENCES skill(entry_id) ON DELETE CASCADE,
    url             TEXT NOT NULL,  -- e.g., https://github.com/stacklok/skills.git
    ref             TEXT,           -- e.g., v1.0.0
    commit_sha      TEXT,           -- Full commit SHA
    subfolder       TEXT            -- Path within repository
);

CREATE INDEX skill_git_package_skill_entry_id_idx ON skill_git_package(skill_entry_id);

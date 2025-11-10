-- Create first set of DB tables

-- Set of supported registry sources.
CREATE TYPE registry_type AS ENUM (
    'LOCAL',
    'FILE',
    'REMOTE'
);

-- Table of registries which we sync against.
CREATE TABLE registry (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    reg_type   registry_type NOT NULL DEFAULT 'LOCAL',
    created_at TIMESTAMPZ DEFAULT NOW(),
    updated_at TIMESTAMPZ DEFAULT NOW()
);

-- Set of states for sync operations.
-- Assumes that these are created during the sync, so we do not need a
-- 'PENDING' state or similar.
CREATE TYPE sync_status AS ENUM (
    'IN_PROGRESS',
    'COMPLETED',
    'FAILED'
);

-- Table of sync operations against remote registries.
-- It is intended that this will be cleaned up on a recurring schedule.
CREATE TABLE registry_sync (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reg_id      UUID REFERENCES registry(id) ON DELETE CASCADE,
    sync_status sync_status NOT NULL DEFAULT 'IN_PROGRESS',
    error_msg   TEXT, -- Populated if sync_status = 'FAILED'
    started_at  TIMESTAMPZ DEFAULT NOW(),
    ended_at    TIMESTAMPZ
);

-- Table of MCP servers known to our registry across all sources.
-- Based on: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/openapi.yaml
CREATE TABLE mcp_server (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    version       TEXT NOT NULL,
    reg_id        UUID REFERENCES registry(id) ON DELETE CASCADE,
    created_at    TIMESTAMPZ DEFAULT NOW(),
    updated_at    TIMESTAMPZ DEFAULT NOW(),
    description   TEXT,
    title         TEXT,
    website       TEXT,
    upstream_meta JSONB, -- Metadata in the ServerResponse object from the upstream registry.
    server_meta   JSONB, -- Metadata in the ServerDetail object.
    repository_url       TEXT, -- Following fields mirror the structure in the API schema.
    repository_id        TEXT,
    repository_subfolder TEXT,
    repository_type      TEXT,
    UNIQUE (name, version, reg_id)
);

-- Used to point to the latest version of a server in a registry.
CREATE TABLE latest_server_version (
    reg_id           UUID NOT NULL REFERENCES registry(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    version          TEXT NOT NULL,
    latest_server_id UUID NOT NULL REFERENCES mcp_server(id) ON DELETE CASCADE,
    PRIMARY KEY (reg_id, name, version)
);

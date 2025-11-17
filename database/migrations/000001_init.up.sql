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
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(name)
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
    started_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    ended_at    TIMESTAMP WITH TIME ZONE
);

CREATE INDEX registry_sync_started_at_idx ON registry_sync(reg_id, started_at);
CREATE INDEX registry_sync_end_at_idx ON registry_sync(reg_id, ended_at);

-- Table of MCP servers known to our registry across all sources.
-- Based on: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/openapi.yaml
CREATE TABLE mcp_server (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    version       TEXT NOT NULL,
    reg_id        UUID REFERENCES registry(id) ON DELETE CASCADE,
    created_at    TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    description   TEXT,
    title         TEXT,
    website       TEXT,
    upstream_meta JSONB, -- Metadata in the ServerResponse object from the upstream registry.
    server_meta   JSONB, -- Metadata in the ServerDetail object.
    repository_url       TEXT, -- Following fields mirror the structure in the API schema.
    repository_id        TEXT,
    repository_subfolder TEXT,
    repository_type      TEXT,
    UNIQUE (reg_id, name, version)
);

-- Set of downloadable artifacts to allow an MCP server to be run locally.
CREATE TABLE mcp_server_package (
    server_id         UUID PRIMARY KEY REFERENCES mcp_server(id) ON DELETE CASCADE,
    registry_type     TEXT NOT NULL, -- Type of upstream registry [npm, docker, nuget, etc].
    pkg_registry_url  TEXT NOT NULL, -- Registry to download this package from. May or may not be the same as the MCP registry.
    pkg_identifier    TEXT NOT NULL, -- URL, or a package manager identifier used to download the package.
    pkg_version       TEXT NOT NULL, -- Version of the MCP server package.
    runtime_hint      TEXT, -- e.g. [npx, uvx, docker, dnx].
    runtime_arguments TEXT[], -- Command line arguments to pass to the runtime.
    package_arguments TEXT[], -- Command line arguments to pass to the package.
    env_vars          TEXT[], -- Name of environment variables needed by MCP server.
    sha256_hash       TEXT, -- SHA256 hash of package file.
    transport         TEXT NOT NULL, -- expected to be one of [stdio, sse, streamable-http], validated in business logic
    transport_url     TEXT,
    transport_headers TEXT[]
);

-- Used to point to a remote MCP server.
CREATE TABLE mcp_server_remote (
    server_id         UUID NOT NULL REFERENCES mcp_server(id) ON DELETE CASCADE,
    transport         TEXT NOT NULL, -- expected to be one of [sse, streamable-http], validated in business logic
    transport_url     TEXT NOT NULL,
    transport_headers TEXT[],
    PRIMARY KEY (server_id, transport, transport_url)
);

-- Used to point to the latest version of a server in a registry.
CREATE TABLE latest_server_version (
    reg_id           UUID NOT NULL REFERENCES registry(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    version          TEXT NOT NULL,
    latest_server_id UUID NOT NULL REFERENCES mcp_server(id) ON DELETE CASCADE,
    PRIMARY KEY (reg_id, name)
);

CREATE TYPE icon_theme AS ENUM (
    'LIGHT',
    'DARK'
);

-- The set of icons associated with an MCP server.
CREATE TABLE mcp_server_icon (
    server_id  UUID NOT NULL REFERENCES mcp_server (id) ON DELETE CASCADE,
    source_uri TEXT NOT NULL,
    mime_type  TEXT,
    theme      icon_theme, -- NULL means 'any' theme.
    PRIMARY KEY (server_id, source_uri, mime_type, theme) -- Unclear if mime_type or theme should be part of the PK
);

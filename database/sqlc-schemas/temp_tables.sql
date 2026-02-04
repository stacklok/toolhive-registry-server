-- Temporary table schemas for sqlc validation
--
-- IMPORTANT: These are NOT migrations and will NOT be executed.
-- These schemas exist only so sqlc can validate queries that reference temp tables.
-- The actual temp tables are created at runtime with CREATE TEMP TABLE ... ON COMMIT DROP.

CREATE TABLE temp_registry_entry (
    id UUID PRIMARY KEY,
    reg_id UUID NOT NULL,
    entry_type entry_type NOT NULL,
    name TEXT NOT NULL,
    title TEXT,
    description TEXT,
    version TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE temp_mcp_server (
    entry_id UUID NOT NULL,
    website TEXT,
    upstream_meta JSONB,
    server_meta JSONB,
    repository_url TEXT,
    repository_id TEXT,
    repository_subfolder TEXT,
    repository_type TEXT
);

CREATE TABLE temp_mcp_server_package (
    server_id UUID NOT NULL,
    registry_type TEXT NOT NULL,
    pkg_registry_url TEXT NOT NULL,
    pkg_identifier TEXT NOT NULL,
    pkg_version TEXT NOT NULL,
    runtime_hint TEXT,
    runtime_arguments TEXT[],
    package_arguments TEXT[],
    env_vars TEXT[],
    sha256_hash TEXT,
    transport TEXT NOT NULL,
    transport_url TEXT,
    transport_headers TEXT[]
);

CREATE TABLE temp_mcp_server_remote (
    server_id UUID NOT NULL,
    transport TEXT NOT NULL,
    transport_url TEXT NOT NULL,
    transport_headers TEXT[]
);

CREATE TABLE temp_mcp_server_icon (
    server_id UUID NOT NULL,
    source_uri TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    theme TEXT NOT NULL
);

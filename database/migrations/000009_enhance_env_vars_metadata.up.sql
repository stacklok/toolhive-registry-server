-- Migration to preserve full metadata for environment variables and transport headers
-- Changes TEXT[] columns to JSONB to store complete KeyValueInput objects

-- Step 1: Add new JSONB columns to mcp_server_package
ALTER TABLE mcp_server_package
    ADD COLUMN env_vars_json JSONB,
    ADD COLUMN transport_headers_json JSONB;

-- Step 2: Migrate existing data from TEXT[] to JSONB format
-- Convert ["VAR1", "VAR2"] to [{"name": "VAR1"}, {"name": "VAR2"}]
UPDATE mcp_server_package
SET env_vars_json = CASE
    WHEN env_vars IS NULL OR array_length(env_vars, 1) IS NULL THEN '[]'::jsonb
    ELSE (
        SELECT jsonb_agg(jsonb_build_object('name', elem))
        FROM unnest(env_vars) AS elem
    )
END,
transport_headers_json = CASE
    WHEN transport_headers IS NULL OR array_length(transport_headers, 1) IS NULL THEN '[]'::jsonb
    ELSE (
        SELECT jsonb_agg(jsonb_build_object('name', elem))
        FROM unnest(transport_headers) AS elem
    )
END;

-- Step 3: Drop old columns and rename new ones
ALTER TABLE mcp_server_package
    DROP COLUMN env_vars,
    DROP COLUMN transport_headers;

ALTER TABLE mcp_server_package
    RENAME COLUMN env_vars_json TO env_vars;

ALTER TABLE mcp_server_package
    RENAME COLUMN transport_headers_json TO transport_headers;

-- Step 4: Add new JSONB column to mcp_server_remote
ALTER TABLE mcp_server_remote
    ADD COLUMN transport_headers_json JSONB;

-- Step 5: Migrate existing data
UPDATE mcp_server_remote
SET transport_headers_json = CASE
    WHEN transport_headers IS NULL OR array_length(transport_headers, 1) IS NULL THEN '[]'::jsonb
    ELSE (
        SELECT jsonb_agg(jsonb_build_object('name', elem))
        FROM unnest(transport_headers) AS elem
    )
END;

-- Step 6: Drop old column and rename new one
ALTER TABLE mcp_server_remote
    DROP COLUMN transport_headers;

ALTER TABLE mcp_server_remote
    RENAME COLUMN transport_headers_json TO transport_headers;

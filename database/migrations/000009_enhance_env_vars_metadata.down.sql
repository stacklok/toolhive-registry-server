-- Rollback migration: Convert JSONB back to TEXT[] (only preserves names)

-- Step 1: Add TEXT[] columns to mcp_server_package
ALTER TABLE mcp_server_package
    ADD COLUMN env_vars_text TEXT[],
    ADD COLUMN transport_headers_text TEXT[];

-- Step 2: Extract names from JSONB and store in TEXT[]
UPDATE mcp_server_package
SET env_vars_text = CASE
    WHEN env_vars IS NULL OR env_vars = '[]'::jsonb THEN '{}'::text[]
    ELSE (
        SELECT array_agg(elem->>'name')
        FROM jsonb_array_elements(env_vars) AS elem
    )
END,
transport_headers_text = CASE
    WHEN transport_headers IS NULL OR transport_headers = '[]'::jsonb THEN '{}'::text[]
    ELSE (
        SELECT array_agg(elem->>'name')
        FROM jsonb_array_elements(transport_headers) AS elem
    )
END;

-- Step 3: Drop JSONB columns and rename TEXT[] columns
ALTER TABLE mcp_server_package
    DROP COLUMN env_vars,
    DROP COLUMN transport_headers;

ALTER TABLE mcp_server_package
    RENAME COLUMN env_vars_text TO env_vars;

ALTER TABLE mcp_server_package
    RENAME COLUMN transport_headers_text TO transport_headers;

-- Step 4: Add TEXT[] column to mcp_server_remote
ALTER TABLE mcp_server_remote
    ADD COLUMN transport_headers_text TEXT[];

-- Step 5: Extract names from JSONB
UPDATE mcp_server_remote
SET transport_headers_text = CASE
    WHEN transport_headers IS NULL OR transport_headers = '[]'::jsonb THEN '{}'::text[]
    ELSE (
        SELECT array_agg(elem->>'name')
        FROM jsonb_array_elements(transport_headers) AS elem
    )
END;

-- Step 6: Drop JSONB column and rename TEXT[] column
ALTER TABLE mcp_server_remote
    DROP COLUMN transport_headers;

ALTER TABLE mcp_server_remote
    RENAME COLUMN transport_headers_text TO transport_headers;

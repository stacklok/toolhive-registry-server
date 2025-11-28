-- Revert mcp_server_package primary key back to server_id only

-- Drop the composite primary key
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_pkey;

-- Add back the original primary key
-- Note: This will fail if there are multiple packages per server
ALTER TABLE mcp_server_package ADD PRIMARY KEY (server_id);

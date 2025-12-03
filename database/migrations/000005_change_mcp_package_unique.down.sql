-- Revert to previous PK definition from migration #3

-- Drop the existing primary key constraint
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_pkey;

-- Add the new composite primary key
ALTER TABLE mcp_server_package ADD PRIMARY KEY (server_id, registry_type, pkg_identifier);
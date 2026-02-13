-- Revert: remove registry_type from the mcp_server_package primary key.

-- Drop the existing primary key constraint.
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_pkey;

-- Restore the previous primary key without registry_type.
ALTER TABLE mcp_server_package ADD PRIMARY KEY (entry_id, pkg_identifier, transport);

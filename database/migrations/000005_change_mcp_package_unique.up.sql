-- Change primary key of mcp_server table
-- We need to consider the transport column since a server which supports
-- multiple serves will be stored in the database as multiple entries with
-- different transports.
-- Also drop registry type - it is unlikely that we will ever have two
-- valid packages with the same identifier, but different registries.

-- Drop the existing primary key constraint
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_pkey;

-- Add the new composite primary key
ALTER TABLE mcp_server_package ADD PRIMARY KEY (server_id, pkg_identifier, transport);
-- Add registry_type back to the mcp_server_package primary key.
-- The previous PK (entry_id, pkg_identifier, transport) allowed only one
-- registry_type per (entry_id, pkg_identifier, transport) triple.  In
-- practice the same package identifier can legitimately appear in different
-- registries (e.g. npm vs pypi), causing duplicate-key failures during
-- sync.  Re-introducing registry_type into the PK fixes this.
--
-- The new PK is strictly more granular than the old one, so existing data
-- that satisfied the old constraint will always satisfy the new one.

-- Step 1: Drop the existing primary key constraint.
ALTER TABLE mcp_server_package DROP CONSTRAINT mcp_server_package_pkey;

-- Step 2: Add the new composite primary key with registry_type.
ALTER TABLE mcp_server_package ADD PRIMARY KEY (entry_id, registry_type, pkg_identifier, transport);

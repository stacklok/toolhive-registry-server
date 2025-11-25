-- Create first set of DB tables

DROP TABLE IF EXISTS mcp_server_icon;
DROP TABLE IF EXISTS mcp_server_remote;
DROP TABLE IF EXISTS mcp_server_package;
DROP TABLE IF EXISTS latest_server_version;
DROP TABLE IF EXISTS registry_sync;
DROP TABLE IF EXISTS mcp_server;
DROP TABLE IF EXISTS registry;

DROP TYPE IF EXISTS sync_status;
DROP TYPE IF EXISTS registry_type;
DROP TYPE IF EXISTS transport;
DROP TYPE IF EXISTS remote_transport;
DROP TYPE IF EXISTS icon_theme;

-- Revoke permissions on existing tables from application role if it exists
-- This removes the privileges granted in the up migration
REVOKE SELECT ON ALL TABLES    IN SCHEMA public FROM toolhive_registry_server;
REVOKE INSERT ON ALL TABLES    IN SCHEMA public FROM toolhive_registry_server;
REVOKE UPDATE ON ALL TABLES    IN SCHEMA public FROM toolhive_registry_server;
REVOKE DELETE ON ALL TABLES    IN SCHEMA public FROM toolhive_registry_server;

-- Revoke permissions on existing sequences from application role
REVOKE USAGE  ON ALL SEQUENCES IN SCHEMA public FROM toolhive_registry_server;
REVOKE SELECT ON ALL SEQUENCES IN SCHEMA public FROM toolhive_registry_server;

-- Revoke default permissions on future tables from application role
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE SELECT ON TABLES FROM toolhive_registry_server;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE INSERT ON TABLES FROM toolhive_registry_server;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE UPDATE ON TABLES FROM toolhive_registry_server;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE DELETE ON TABLES FROM toolhive_registry_server;

-- Revoke default permissions on future sequences from application role
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE USAGE  ON SEQUENCES FROM toolhive_registry_server;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE SELECT ON SEQUENCES FROM toolhive_registry_server;

DROP ROLE IF EXISTS toolhive_registry_server;

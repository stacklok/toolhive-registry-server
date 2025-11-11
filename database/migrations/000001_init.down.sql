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

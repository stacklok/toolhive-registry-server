-- Drop the format column from source table.
--
-- The ToolHive registry format is no longer supported; only the upstream
-- MCP registry format remains. With a single format the column carries no
-- information, so remove it entirely.

ALTER TABLE source DROP CONSTRAINT registry_format_check;
ALTER TABLE source DROP COLUMN format;

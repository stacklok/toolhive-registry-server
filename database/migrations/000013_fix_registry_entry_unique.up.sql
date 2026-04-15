-- Fix the unique constraint on registry_entry to include entry_type.
-- Without entry_type, an MCP server and a skill with the same name and version
-- under the same registry would conflict.

ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_reg_id_name_version_key;
ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_reg_id_entry_type_name_version_key
    UNIQUE (reg_id, entry_type, name, version);

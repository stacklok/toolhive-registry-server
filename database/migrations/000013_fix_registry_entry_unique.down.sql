-- Revert to the original unique constraint without entry_type.

ALTER TABLE registry_entry DROP CONSTRAINT registry_entry_reg_id_entry_type_name_version_key;
ALTER TABLE registry_entry ADD CONSTRAINT registry_entry_reg_id_name_version_key
    UNIQUE (reg_id, name, version);

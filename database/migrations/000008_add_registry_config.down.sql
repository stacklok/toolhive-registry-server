-- Remove registry configuration columns

ALTER TABLE registry DROP CONSTRAINT IF EXISTS registry_format_check;
ALTER TABLE registry DROP CONSTRAINT IF EXISTS registry_source_type_check;
ALTER TABLE registry DROP COLUMN IF EXISTS filter_config;
ALTER TABLE registry DROP COLUMN IF EXISTS source_config;
ALTER TABLE registry DROP COLUMN IF EXISTS format;
ALTER TABLE registry DROP COLUMN IF EXISTS source_type;
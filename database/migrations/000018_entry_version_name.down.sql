CREATE INDEX entry_version_version_idx ON entry_version(version);
DROP INDEX entry_version_name_version_idx;
ALTER TABLE entry_version DROP COLUMN name;

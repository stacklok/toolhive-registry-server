-- Existing databases with more than one managed source must be repaired before upgrading.
CREATE UNIQUE INDEX source_single_managed_idx ON source ((source_type)) WHERE source_type = 'managed';

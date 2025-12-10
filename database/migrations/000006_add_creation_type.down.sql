-- Remove creation_type column from registry table
ALTER TABLE registry
DROP COLUMN IF EXISTS creation_type;

-- Drop creation_type enum
DROP TYPE IF EXISTS creation_type;

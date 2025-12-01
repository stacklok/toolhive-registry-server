-- Remove KUBERNETES value from registry_type enum
-- Note: PostgreSQL does not support directly removing enum values.
-- This migration assumes no registries of type KUBERNETES exist.
-- If Kubernetes registries exist, they must be deleted before running this migration.

-- First, update any existing KUBERNETES registries to a different type (this will fail if any exist)
-- The application should ensure no Kubernetes registries exist before downgrade.

-- PostgreSQL requires recreating the enum type to remove a value.
-- This is a destructive operation and should only be done if no data uses the KUBERNETES type.

-- Create a new enum type without KUBERNETES
CREATE TYPE registry_type_new AS ENUM ('MANAGED', 'FILE', 'REMOTE');

-- Drop the default constraint first
ALTER TABLE registry ALTER COLUMN reg_type DROP DEFAULT;

-- Alter the column to use the new type (will fail if any KUBERNETES values exist)
ALTER TABLE registry
    ALTER COLUMN reg_type TYPE registry_type_new
    USING reg_type::text::registry_type_new;

-- Drop the old type
DROP TYPE registry_type;

-- Rename the new type to the original name
ALTER TYPE registry_type_new RENAME TO registry_type;

-- Restore the default value
ALTER TABLE registry ALTER COLUMN reg_type SET DEFAULT 'MANAGED'::registry_type;

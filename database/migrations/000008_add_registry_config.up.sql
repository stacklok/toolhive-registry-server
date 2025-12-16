-- Add columns to store full registry configuration for API-created registries

-- Add source type column (git, api, file, managed, kubernetes)
ALTER TABLE registry
ADD COLUMN source_type TEXT;

-- Add format column for registry data format (toolhive, upstream)
ALTER TABLE registry
ADD COLUMN format TEXT;

-- Add JSONB column for source-specific configuration (URLs, paths, no credentials)
ALTER TABLE registry
ADD COLUMN source_config JSONB;

-- Add JSONB column for filter configuration (name/tag includes/excludes)
ALTER TABLE registry
ADD COLUMN filter_config JSONB;

-- Add CHECK constraint to ensure valid source types
ALTER TABLE registry
ADD CONSTRAINT registry_source_type_check
CHECK (source_type IS NULL OR source_type IN ('git', 'api', 'file', 'managed', 'kubernetes'));

-- Add CHECK constraint to ensure valid format
ALTER TABLE registry
ADD CONSTRAINT registry_format_check
CHECK (format IS NULL OR format IN ('toolhive', 'upstream'));
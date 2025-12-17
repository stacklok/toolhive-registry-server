-- Add registry configuration columns for API-created registries
--
-- These columns store the full registry configuration, enabling registries
-- to be created via API with all necessary metadata for syncing.

-- Source configuration columns
ALTER TABLE registry
ADD COLUMN source_type TEXT,                    -- git, api, file, managed, kubernetes
ADD COLUMN format TEXT,                         -- toolhive or upstream
ADD COLUMN source_config JSONB,                 -- source-specific config (URLs, paths)
ADD COLUMN filter_config JSONB,                 -- name/tag filtering rules
ADD COLUMN syncable BOOLEAN NOT NULL DEFAULT true;  -- whether registry needs syncing

-- Validation constraints
ALTER TABLE registry
ADD CONSTRAINT registry_source_type_check
    CHECK (source_type IS NULL OR source_type IN ('git', 'api', 'file', 'managed', 'kubernetes')),
ADD CONSTRAINT registry_format_check
    CHECK (format IS NULL OR format IN ('toolhive', 'upstream'));

-- Set syncable=false for non-synced registry types
UPDATE registry SET syncable = false
WHERE reg_type IN ('MANAGED', 'KUBERNETES')
   OR (source_type = 'file' AND source_config->>'data' IS NOT NULL AND source_config->>'data' != '');

-- Index for efficient sync job queries
CREATE INDEX idx_registry_syncable ON registry(syncable) WHERE syncable = true;
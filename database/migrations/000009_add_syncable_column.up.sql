-- Add syncable column with default true (most registries sync)
ALTER TABLE registry ADD COLUMN syncable BOOLEAN NOT NULL DEFAULT true;

-- Set syncable=false for managed and kubernetes registries
UPDATE registry SET syncable = false WHERE reg_type IN ('MANAGED', 'KUBERNETES');

-- Set syncable=false for file registries with inline data
UPDATE registry SET syncable = false
WHERE source_type = 'file'
  AND source_config IS NOT NULL
  AND source_config->>'data' IS NOT NULL
  AND source_config->>'data' != '';

-- Add partial index for efficient filtering (only index syncable registries)
CREATE INDEX idx_registry_syncable ON registry(syncable) WHERE syncable = true;

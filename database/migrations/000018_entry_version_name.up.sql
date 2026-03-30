-- Denormalize name into entry_version to enable an index on (name, version)
-- for efficient cursor-based pagination in ListServers.

-- Step 1: Add name column (nullable initially for backfill)
ALTER TABLE entry_version ADD COLUMN name TEXT;

-- Step 2: Backfill from registry_entry
UPDATE entry_version v
   SET name = e.name
  FROM registry_entry e
 WHERE e.id = v.entry_id;

-- Step 3: Enforce NOT NULL now that all rows are populated
ALTER TABLE entry_version ALTER COLUMN name SET NOT NULL;

-- Step 4: Add composite index for cursor pagination and ordering
CREATE INDEX entry_version_name_version_idx ON entry_version(name, version);

-- Step 5: Drop the standalone version index — subsumed by the composite above
DROP INDEX entry_version_version_idx;

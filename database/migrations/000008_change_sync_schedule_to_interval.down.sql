-- Revert sync_schedule column from INTERVAL back to TEXT
-- This converts PostgreSQL intervals back to duration strings
ALTER TABLE registry
ALTER COLUMN sync_schedule TYPE TEXT
USING CASE
    WHEN sync_schedule IS NULL THEN NULL
    ELSE sync_schedule::TEXT
END;

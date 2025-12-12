-- Change sync_schedule column from TEXT to INTERVAL type
-- This provides native PostgreSQL support for time intervals
-- The conversion preserves existing duration strings like '30m', '1h'
ALTER TABLE registry
ALTER COLUMN sync_schedule TYPE INTERVAL
USING CASE
    WHEN sync_schedule IS NULL THEN NULL
    WHEN sync_schedule = '' THEN NULL
    ELSE sync_schedule::INTERVAL
END;

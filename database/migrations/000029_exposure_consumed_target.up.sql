-- Record target continuity only; never infer or backfill consumed consent.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM exposure_acknowledgement WHERE consumed_at IS NOT NULL) THEN
        RAISE EXCEPTION 'consumed exposure state lacks target evidence; restore supported offline evidence before migrating';
    END IF;
END $$;

ALTER TABLE exposure_acknowledgement
    ADD COLUMN consumed_target_digest TEXT,
    ADD CONSTRAINT exposure_consumed_target_state CHECK (
        (consumed_at IS NULL AND consumed_target_digest IS NULL)
        OR (consumed_at IS NOT NULL AND consumed_target_digest IS NOT NULL
            AND length(consumed_target_digest) = 71
            AND consumed_target_digest ~ '^sha256:[0-9a-f]{64}$'));

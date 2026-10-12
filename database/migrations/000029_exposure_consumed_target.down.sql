DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM exposure_acknowledgement
        WHERE consumed_at IS NOT NULL OR consumed_target_digest IS NOT NULL) THEN
        RAISE EXCEPTION 'refusing to discard exposure consumed target evidence';
    END IF;
END $$;

ALTER TABLE exposure_acknowledgement
    DROP CONSTRAINT exposure_consumed_target_state,
    DROP COLUMN consumed_target_digest;

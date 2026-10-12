DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM exposure_acknowledgement
        WHERE retirement_id IS NOT NULL OR consumed_at IS NOT NULL)
        OR EXISTS (SELECT 1 FROM exposure_claims_archive) THEN
        RAISE EXCEPTION 'refusing to discard exposure retirement, consumption or archive evidence';
    END IF;
END $$;
DROP TABLE exposure_claims_archive;
ALTER TABLE exposure_acknowledgement
    DROP CONSTRAINT exposure_consumption_state,
    DROP CONSTRAINT exposure_retirement_state,
    DROP COLUMN consumed_at,
    DROP COLUMN retired_at,
    DROP COLUMN post_retirement_digest,
    DROP COLUMN retirement_id;

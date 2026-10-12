DO $$
BEGIN
    LOCK TABLE exposure_acknowledgement IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM exposure_acknowledgement WHERE acknowledgement_id IS NOT NULL) THEN
        RAISE EXCEPTION 'refusing to discard exposure acknowledgement; retain migration state';
    END IF;
END $$;
DROP TABLE exposure_acknowledgement;

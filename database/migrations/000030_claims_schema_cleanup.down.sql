-- Claims cannot be reconstructed from SQL NULL or from a partial archive.
DO $$ BEGIN
    RAISE EXCEPTION 'schema 30 downgrade unsupported; restore a verified offline pre-retirement backup';
END $$;

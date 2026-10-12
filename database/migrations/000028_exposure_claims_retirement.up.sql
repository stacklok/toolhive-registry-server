-- Dormant offline retirement state. Schema application never retires metadata.
ALTER TABLE exposure_acknowledgement
    ADD COLUMN retirement_id UUID,
    ADD COLUMN post_retirement_digest TEXT,
    ADD COLUMN retired_at TIMESTAMPTZ,
    ADD COLUMN consumed_at TIMESTAMPTZ,
    ADD CONSTRAINT exposure_retirement_state CHECK (
        (retirement_id IS NULL AND post_retirement_digest IS NULL AND retired_at IS NULL)
        OR (retirement_id IS NOT NULL AND post_retirement_digest IS NOT NULL AND retired_at IS NOT NULL
            AND acknowledgement_id IS NOT NULL AND acknowledgement_mode = 'whole-catalog')),
    ADD CONSTRAINT exposure_consumption_state CHECK (
        consumed_at IS NULL OR (acknowledgement_id IS NOT NULL
            AND (retirement_id IS NOT NULL OR acknowledgement_mode = 'empty')));

-- Sensitive migration evidence, not catalog data. No FK to live rows: deletion or
-- replacement of a catalog incarnation must never cascade into recovery evidence.
CREATE TABLE exposure_claims_archive (
    retirement_id UUID NOT NULL,
    row_table TEXT NOT NULL CHECK (row_table IN ('source', 'registry', 'registry_entry')),
    row_id UUID NOT NULL,
    name TEXT NOT NULL,
    source_id UUID,
    source_name TEXT,
    kind TEXT,
    ownership TEXT CHECK (ownership IN ('CONFIG', 'API')),
    claims_sql_null BOOLEAN NOT NULL,
    claims JSONB,
    PRIMARY KEY (retirement_id, row_table, row_id),
    CHECK (claims_sql_null = (claims IS NULL)),
    CHECK (
        (row_table = 'source' AND source_id IS NOT NULL AND source_name IS NOT NULL
            AND source_id = row_id AND source_name = name
            AND kind IS NOT NULL AND ownership IS NOT NULL)
        OR (row_table = 'registry' AND source_id IS NULL AND source_name IS NULL
            AND kind IS NULL AND ownership IS NOT NULL)
        OR (row_table = 'registry_entry' AND source_id IS NOT NULL AND source_name IS NOT NULL
            AND kind IS NOT NULL AND kind IN ('MCP', 'SKILL', 'PLUGIN') AND ownership IS NOT NULL))
);

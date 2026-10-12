-- Dormant standalone migration state; no runtime grants or catalog changes.
CREATE TABLE exposure_acknowledgement (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    database_id UUID NOT NULL DEFAULT gen_random_uuid(),
    inventory_version INTEGER NOT NULL DEFAULT 1 CHECK (inventory_version = 1),
    acknowledgement_id UUID,
    acknowledgement_mode TEXT CHECK (acknowledgement_mode IN ('whole-catalog', 'empty')),
    reviewed_digest TEXT,
    post_fence_digest TEXT,
    config_revision TEXT,
    acknowledged_at TIMESTAMPTZ,
    CHECK ((acknowledgement_id IS NULL AND acknowledgement_mode IS NULL
        AND reviewed_digest IS NULL AND post_fence_digest IS NULL
        AND config_revision IS NULL AND acknowledged_at IS NULL)
      OR (acknowledgement_id IS NOT NULL AND acknowledgement_mode IS NOT NULL
        AND reviewed_digest IS NOT NULL AND post_fence_digest IS NOT NULL
        AND config_revision IS NOT NULL AND acknowledged_at IS NOT NULL))
);
INSERT INTO exposure_acknowledgement(singleton) VALUES (true);

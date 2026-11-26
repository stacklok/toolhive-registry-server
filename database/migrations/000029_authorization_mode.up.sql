-- The server's authorization mode, fixed for the life of a deployment:
-- 'claims' (roles plus per-resource claims) or 'cedar' (route policy). The
-- server records its configured mode on first start and refuses to start
-- with the other one; switching means a new instance.
CREATE TABLE authorization_mode (
    singleton   BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    mode        TEXT NOT NULL CHECK (mode IN ('claims', 'cedar')),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A database that already holds a catalog predates Cedar mode, so it has
-- been running in claims mode.
INSERT INTO authorization_mode (mode)
SELECT 'claims'
 WHERE EXISTS (SELECT 1 FROM source) OR EXISTS (SELECT 1 FROM registry);

-- Grant explicitly instead of relying on the default privileges set by whoever
-- ran migration 1: a deployment that changed its migration role since then
-- would otherwise leave the application role unable to read this table.
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'toolhive_registry_server') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON authorization_mode TO toolhive_registry_server;
    END IF;
END $$;
